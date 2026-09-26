// Package pg is the storm-backed repository layer.
//
// It replaces server/repositories/gorms one repository at a time. Both exist
// during the port; which one a service uses is decided at the composition root,
// so a repository can move without the service that calls it changing.
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
	"github.com/gsoultan/metis/server/repositories/store/workflowgroup"
	"github.com/gsoultan/metis/server/repositories/store/workflowgroupmembership"
	"github.com/gsoultan/metis/server/repositories/store/workflowuser"
	"github.com/gsoultan/storm/runtime"
)

type workflowUserRepository struct {
	conn *db.Conn
}

// NewWorkflowUserRepository returns the storm-backed participant directory.
func NewWorkflowUserRepository(conn *db.Conn) contracts.WorkflowUserRepository {
	return &workflowUserRepository{conn: conn}
}

// ListByProject returns a project's participants, excluding those removed.
//
// The deleted_at filter is explicit. GORM applied it automatically and storm
// does not, which is the one behaviour the port cannot carry over silently —
// every read that should hide removed rows has to say so, here and everywhere
// else.
//
// A limit above zero is applied in the query, so asking whether a project has
// anybody reads one row rather than the directory.
//
// No limit means the whole directory, as the contract says — not the store's
// first thousand. A sync deactivating whoever its source stopped naming, and a
// removal checking that somebody is in this project, both act on what this
// returns, and past a thousand people they missed everyone after the window.
func (r *workflowUserRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.WorkflowUser, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// The id breaks ties so the cursor is a position; a username is unique
	// within a project, so it never has to.
	query := workflowuser.New().
		Where(workflowuser.ProjectID.Eq(projectID)).
		Order(workflowuser.Username.Asc(), workflowuser.ID.Asc())
	var rows []workflowuser.Row
	if limit > 0 {
		rows, err = query.Limit(int64(limit)).All(ctx, ex, nil)
	} else {
		rows, err = everyRow[workflowuser.Row](ctx, ex, query)
	}
	if err != nil {
		return nil, fmt.Errorf("could not list participants: %w", err)
	}

	out := make([]entities.WorkflowUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, participantFrom(row))
	}
	return out, nil
}

// GetByUsername finds one participant by the name a process would refer to.
func (r *workflowUserRepository) GetByUsername(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return entities.WorkflowUser{}, err
	}
	row, found, err := workflowuser.New().
		Where(
			workflowuser.ProjectID.Eq(projectID),
			workflowuser.Username.Eq(username),
		).
		One(ctx, ex)
	if err != nil {
		return entities.WorkflowUser{}, fmt.Errorf("could not read participant %q: %w", username, err)
	}
	if !found {
		return entities.WorkflowUser{}, fmt.Errorf("%w: no participant %q in this project", apierr.ErrNotFound, username)
	}
	return participantFrom(row), nil
}

// Upsert writes one participant, replacing the row with the same username in
// the same project.
//
// An upsert on the (project, username) unique index rather than a read followed
// by a write: an import of five hundred rows would otherwise be a thousand
// round trips, and two imports running at once would both read "absent" and one
// would be refused as a duplicate.
func (r *workflowUserRepository) Upsert(ctx context.Context, participant entities.WorkflowUser) (bool, error) {
	if participant.Project == nil || participant.Project.ID == uuid.Nil {
		return false, apierr.Invalidf("a participant belongs to a project")
	}
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return false, err
	}

	// Whether this is new has to be asked before the write: the upsert cannot
	// say which of the two it did, and "created" is the number an import
	// reports.
	//
	// This read does not see removed participants — the model declares soft
	// delete, so the predicate is compiled in — which is the right answer to
	// "is this person in the directory". Somebody being reinstated is reported
	// as created, because from the directory's point of view they were not
	// there and now they are.
	_, found, err := workflowuser.New().
		Where(
			workflowuser.ProjectID.Eq(participant.Project.ID),
			workflowuser.Username.Eq(participant.Username),
		).
		One(ctx, ex)
	if err != nil {
		return false, fmt.Errorf("could not check for participant %q: %w", participant.Username, err)
	}

	ins := workflowuser.Create()
	ins.SetProjectID(participant.Project.ID)
	ins.SetUsername(participant.Username)
	ins.SetActive(participant.Active)
	setOrNullString(ins.SetDisplayName, ins.SetDisplayNameNull, participant.DisplayName)
	setOrNullString(ins.SetEmail, ins.SetEmailNull, participant.Email)
	// Credentials are never written here. An import carries names, not
	// passwords, and overwriting a hash with nothing would lock out everybody
	// the file happened to mention.
	// Always, not only when the row was found: an import naming somebody is
	// somebody saying they are there, whether they were removed or never
	// existed. The read above cannot see a removed participant, so making this
	// conditional on it would upsert onto their row and leave it marked — the
	// person would be reported as created and still be invisible.
	//
	// The conflict lands on their existing row rather than making a second one
	// because the key is declared UniqueAcrossDeleted: their tasks and group
	// memberships hang off that id.
	ins.SetDeletedAtNull()
	ins.OnConflictProjectIDUsername()

	if _, err := ins.Insert(ctx, ex); err != nil {
		return false, fmt.Errorf("could not save participant %q: %w", participant.Username, err)
	}
	return !found, nil
}

// EnsureGroup returns a project's group of that name, creating it if absent.
func (r *workflowUserRepository) EnsureGroup(ctx context.Context, projectID uuid.UUID, name string) (entities.WorkflowGroup, bool, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return entities.WorkflowGroup{}, false, err
	}
	existing, found, err := workflowgroup.New().
		Where(workflowgroup.ProjectID.Eq(projectID), workflowgroup.Name.Eq(name)).
		One(ctx, ex)
	if err != nil {
		return entities.WorkflowGroup{}, false, fmt.Errorf("could not look up group %q: %w", name, err)
	}
	if found {
		return groupFrom(existing), false, nil
	}

	ins := workflowgroup.Create()
	ins.SetProjectID(projectID)
	ins.SetName(name)
	ins.OnConflictProjectIDName()
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.WorkflowGroup{}, false, fmt.Errorf("could not create group %q: %w", name, err)
	}
	return groupFrom(row), true, nil
}

// AddToGroup puts a participant in a group, and says nothing if they are
// already in it — an import that runs twice must not fail the second time.
func (r *workflowUserRepository) AddToGroup(ctx context.Context, participantID, groupID uuid.UUID) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := workflowgroupmembership.Create()
	ins.SetWorkflowUserID(participantID)
	ins.SetWorkflowGroupID(groupID)
	ins.DoNothing()
	// ErrConflict is what a DO NOTHING insert returns when the row was already
	// there: DO NOTHING suppresses RETURNING, so there is no row to hand back.
	// It is the success case here — being in a group twice is being in it once.
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not add a participant to a group: %w", err)
	}
	return nil
}

func participantFrom(row workflowuser.Row) entities.WorkflowUser {
	hash, hasHash := row.PasswordHash.Get()
	return entities.WorkflowUser{
		ID:             row.ID,
		Project:        &entities.Project{ID: row.ProjectID},
		Username:       row.Username,
		DisplayName:    valueOr(row.DisplayName),
		Email:          valueOr(row.Email),
		Active:         row.Active,
		HasCredentials: hasHash && hash != "",
		CreatedAt:      row.CreatedAt,
	}
}

func groupFrom(row workflowgroup.Row) entities.WorkflowGroup {
	return entities.WorkflowGroup{
		ID:          row.ID,
		Project:     &entities.Project{ID: row.ProjectID},
		Name:        row.Name,
		Description: valueOr(row.Description),
	}
}

// valueOr reads a nullable column as the zero value when absent, which is what
// the entity layer means by an optional string.
func valueOr(n runtime.Null[string]) string {
	value, ok := n.Get()
	if !ok {
		return ""
	}
	return value
}

// setOrNullString writes a value, or SQL NULL when it is empty.
//
// The two are different in the database and the same in the entity: an empty
// display name is somebody with no display name, and storing "" would make a
// later "is it set" check answer yes.
func setOrNullString(set func(string), setNull func(), value string) {
	if value == "" {
		setNull()
		return
	}
	set(value)
}

// Delete removes somebody from a project's directory.
//
// The generated Delete marks the row. The key is declared across the deleted
// rows, so the marked row goes on holding this project's spelling of their
// username — which is what makes a later import reinstate them rather than
// create a second person with the same name and none of their history.
func (r *workflowUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := workflowuser.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			// Already gone, or never there. Both are the same answer to the
			// caller and neither is a server fault.
			return fmt.Errorf("%w: no such participant", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not remove the participant: %w", err)
	}
	return nil
}
