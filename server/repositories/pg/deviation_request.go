package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/storm/runtime"
)

type deviationRequestRepository struct{ conn }

// NewDeviationRequestRepository returns the store of the requests that wait
// for a second administrator.
func NewDeviationRequestRepository(c *db.Conn) contracts.DeviationRequestRepository {
	return &deviationRequestRepository{conn{conn: c}}
}

// liveRequestIndex is the unique index that allows a project one live request
// per fingerprint. Named here because its refusal is the one a caller is told
// apart from every other failure of the insert.
const liveRequestIndex = "ux_deviation_requests_live_key"

// Create writes a request that waits for approval, inside the transaction of
// the change that asks for it and nowhere else.
//
// Refused with ErrDeviationRequestOutsideTransaction when none is open: a
// request written on its own connection survives an ask that rolled back, and
// then holds its fingerprint against every later one.
//
// Scoped to the caller's organization through the project it names, and what
// it names has to be in that project. instance_id has no foreign key (the
// lock order note in the migration), and the foreign keys on the two versions
// say only that they exist somewhere, so these reads are what stop a request
// pointing an approver at somebody else's case or somebody else's process.
//
// The command and the plan hold business values and are sealed like every
// other copy of them; the outcome and the list of instances are not, and stay
// readable in SQL.
func (r *deviationRequestRepository) Create(ctx context.Context, request entities.DeviationRequest) (entities.DeviationRequest, error) {
	if !db.InTransaction(ctx) {
		return entities.DeviationRequest{}, contracts.ErrDeviationRequestOutsideTransaction
	}
	if err := wellFormedRequest(request); err != nil {
		return entities.DeviationRequest{}, err
	}
	if err := r.requireOwnTarget(ctx, request); err != nil {
		return entities.DeviationRequest{}, err
	}
	if request.ID == uuid.Nil {
		// The database default is a v4, and the queue orders ties in creation
		// time by id.
		request.ID = uuid.Must(uuid.NewV7())
	}
	ins, err := stageRequest(request)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		if heldByALiveRequest(err) {
			return entities.DeviationRequest{}, contracts.ErrDeviationRequestAlreadyWaiting
		}
		return entities.DeviationRequest{}, fmt.Errorf("could not record the request: %w", err)
	}
	return requestFrom(row)
}

// heldByALiveRequest reports whether an insert was refused because the
// project already has a live request with the fingerprint. Any other unique
// violation — the primary key — is not that, and is not told as that.
func heldByALiveRequest(err error) bool {
	var refused *runtime.ConstraintError
	return errors.As(err, &refused) && errors.Is(err, runtime.ErrUniqueViolation) && refused.Constraint == liveRequestIndex
}

// requireOwnTarget refuses a request for a project that is not the caller's,
// or for an instance or a version outside that project.
//
// A request is a record in one project about that project's work. The project
// is the caller's or the request is refused; what it names is then looked for
// in that project and nowhere else, so something of another project — of the
// same organization or of another — is answered as something that is not
// there, which is all this caller may know of it.
func (r *deviationRequestRepository) requireOwnTarget(ctx context.Context, request entities.DeviationRequest) error {
	projectID := request.Project.ID
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if request.Instance != nil && request.Instance.ID != uuid.Nil {
		found, err := processinstance.New().
			Where(processinstance.ID.Eq(request.Instance.ID), processinstance.ProjectID.Eq(projectID)).
			Exists(ctx, ex)
		if err != nil {
			return fmt.Errorf("could not check the process instance: %w", err)
		}
		if !found {
			return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
		}
	}
	for _, version := range []*entities.ProcessDefinition{request.SourceDefinition, request.TargetDefinition} {
		if err := requireVersionInProject(ctx, ex, version, projectID); err != nil {
			return err
		}
	}
	return nil
}

// requireVersionInProject refuses a version that is not one of the project's.
//
// The foreign keys on the two version columns say only that the versions
// exist, in any project of any organization. This is the read the definition
// repository scopes a version by — the id within the project, a deleted
// version left out — asked as a yes or no, since a definition carries its
// whole graph and none of it is wanted here.
func requireVersionInProject(ctx context.Context, ex runtime.Executor, version *entities.ProcessDefinition, projectID uuid.UUID) error {
	if version == nil || version.ID == uuid.Nil {
		return nil
	}
	found, err := processdefinition.New().
		Where(processdefinition.ID.Eq(version.ID), processdefinition.ProjectID.Eq(projectID)).
		Exists(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not check the definition: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such definition", apierr.ErrNotFound)
	}
	return nil
}

// Get answers one request, whole.
func (r *deviationRequestRepository) Get(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error) {
	row, err := r.one(ctx, id, false)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	return requestFrom(row)
}

// GetForUpdate answers one request and holds its row (FOR UPDATE) until the
// transaction ends.
//
// A request's row is locked before the instance's, everywhere — request, then
// instance, then task rows — so whoever decides takes this first. Refused
// outside a transaction: the lock would be let go before the next statement.
func (r *deviationRequestRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error) {
	if !db.InTransaction(ctx) {
		return entities.DeviationRequest{}, contracts.ErrDeviationRequestOutsideTransaction
	}
	row, err := r.one(ctx, id, true)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	return requestFrom(row)
}

// one reads a request by id, within the caller's organization.
//
// The scope is in the statement, so another organization's request is neither
// read nor — when the read locks — held: it is not found, which is also the
// honest answer when it exists and belongs to somebody else.
func (r *deviationRequestRepository) one(ctx context.Context, id uuid.UUID, forUpdate bool) (deviationrequest.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return deviationrequest.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return deviationrequest.Row{}, err
	}
	q := deviationrequest.New().Where(deviationrequest.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return deviationrequest.Row{}, fmt.Errorf("%w: no such request", apierr.ErrNotFound)
		}
		q = q.Where(deviationrequest.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	if forUpdate {
		q = q.ForUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return deviationrequest.Row{}, fmt.Errorf("could not read the request: %w", err)
	}
	if !found {
		return deviationrequest.Row{}, fmt.Errorf("%w: no such request", apierr.ErrNotFound)
	}
	return row, nil
}

// FindLive answers the project's live request with this fingerprint, if there
// is one, as stored.
//
// It reads the live key, which a request holds from the moment it is asked
// until a decision, a report or the sweep writes that it is over. So the
// request found may be approved rather than waiting, and may be past its
// deadline with nothing having closed it yet: the caller reads it through
// EffectiveStatus. Value predicates only, so the bound statement uses the
// unique index whatever plan the server settles on.
func (r *deviationRequestRepository) FindLive(ctx context.Context, projectID uuid.UUID, fingerprint string) (entities.DeviationRequest, bool, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return entities.DeviationRequest{}, false, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.DeviationRequest{}, false, err
	}
	row, found, err := deviationrequest.New().
		Where(deviationrequest.ProjectID.Eq(projectID), deviationrequest.LiveKey.Eq(fingerprint)).
		One(ctx, ex)
	if err != nil {
		return entities.DeviationRequest{}, false, fmt.Errorf("could not read the live request: %w", err)
	}
	if !found {
		return entities.DeviationRequest{}, false, nil
	}
	request, err := requestFrom(row)
	if err != nil {
		return entities.DeviationRequest{}, false, err
	}
	return request, true, nil
}

// Transition moves a request from the status the caller read it at to
// change.Status.
//
// It takes the row (FOR UPDATE — request before instance, as GetForUpdate)
// and only then looks at the status, so a move that waited for another reads
// what the other left: of two made at once, one is written and the other is
// ErrDeviationRequestDecided. Nothing here compares against a copy read
// before the lock.
//
// Which moves exist is checkedMove and, for the request's own kind,
// checkedMoveOfKind; which of them have to say who and when is
// checkedDecision. A change that fails any of them is the caller's mistake,
// and nothing is written.
func (r *deviationRequestRepository) Transition(ctx context.Context, id uuid.UUID, from entities.DeviationRequestStatus, change contracts.DeviationRequestChange) (entities.DeviationRequest, error) {
	if !db.InTransaction(ctx) {
		return entities.DeviationRequest{}, contracts.ErrDeviationRequestOutsideTransaction
	}
	if err := checkedMove(from, change); err != nil {
		return entities.DeviationRequest{}, err
	}
	row, err := r.one(ctx, id, true)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	if err := checkedMoveOfKind(entities.DeviationRequestKind(row.Kind), from, change.Status); err != nil {
		return entities.DeviationRequest{}, err
	}
	if row.Status != string(from) {
		return entities.DeviationRequest{}, contracts.ErrDeviationRequestDecided
	}
	if err := checkedDecision(row, change); err != nil {
		return entities.DeviationRequest{}, err
	}
	mut, err := stageChange(row, change)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	if err := mut.Update(ctx, ex); err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("could not move the request to %s: %w", change.Status, err)
	}
	return movedRequestFrom(mut.Row())
}

// movedRequestFrom decodes the row a move left: whole when its documents
// open, and as the queue reads it — without them — when they do not.
//
// A move writes a status and what goes with it; it writes none of the three
// documents and needs none of them. If answering had to open them, a request
// whose sealed plan no longer opens could never be closed: the answer would
// fail after the write, the transaction would roll back, and the request
// would hold its fingerprint for ever. So the move stands, and the answer says
// what it can — with Command, Plan and ApprovedInstances nil, which a whole
// request never has, so a caller that needs them can tell. Anything else that
// cannot be decoded is still an error.
func movedRequestFrom(row deviationrequest.Row) (entities.DeviationRequest, error) {
	whole, err := requestFrom(row)
	if err == nil {
		return whole, nil
	}
	return queuedRequestFrom(queueRowOf(row))
}
