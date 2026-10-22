package impl

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// maxRunningBatchOperations is how many operations one project may have
// running at once. Each is worked through a few instances at a time beside
// everybody else's, so a project that asks for more than this is asking for
// the same instances twice or for more than it can watch.
const maxRunningBatchOperations = 5

// batchOperationListLimit is how many of a project's operations the list
// shows, newest first.
const batchOperationListLimit = 100

// PrincipalLoader reads an account as a signed-in caller would be: its own
// roles and those its groups grant, as they stand now.
type PrincipalLoader func(ctx context.Context, accountID uuid.UUID) (entities.User, error)

// AccountLoader is the user service's way to read an account as a principal,
// for work done later on somebody's behalf. Nil for a UserService that is not
// this package's, which leaves that work unable to act for anybody.
func AccountLoader(users contracts.UserService) PrincipalLoader {
	s, ok := users.(*userService)
	if !ok {
		return nil
	}
	return func(ctx context.Context, accountID uuid.UUID) (entities.User, error) {
		// Read first, so an account that is gone is told apart from one that
		// could not be read: resolvePrincipal answers both as a bad token.
		if _, _, err := s.repo.User().GetWithPasswordByID(ctx, accountID); err != nil {
			return entities.User{}, err
		}
		principal, err := s.resolvePrincipal(ctx, accountID)
		if err != nil {
			return entities.User{}, err
		}
		return principal.user, nil
	}
}

// batchOperationService records batch operations and works through them.
type batchOperationService struct {
	repo      repositories.Repository
	deviator  contracts.InstanceDeviator
	incidents contracts.IncidentManager
	accounts  PrincipalLoader
	settings  BatchOperationSettings
	now       func() time.Time
}

// NewBatchOperationService returns the service that retries or cancels many
// instances at once. Each instance is cancelled through deviator and retried
// through incidents, exactly as one instance is, acting as the account that
// asked, which accounts reads.
func NewBatchOperationService(
	repo repositories.Repository,
	deviator contracts.InstanceDeviator,
	incidents contracts.IncidentManager,
	accounts PrincipalLoader,
	settings BatchOperationSettings,
) contracts.BatchOperationService {
	return &batchOperationService{
		repo:      repo,
		deviator:  deviator,
		incidents: incidents,
		accounts:  accounts,
		settings:  settings,
		now:       time.Now,
	}
}

// StartBatchOperation checks the caller and the request before reading
// anything, then counts the instances the filter matches. A dry run answers
// the count. Anything else is refused when the filter matches none or more
// than one operation may take, and otherwise records the operation and its
// instances together.
func (s *batchOperationService) StartBatchOperation(ctx context.Context, request entities.BatchOperationRequest) (entities.BatchOperationOutcome, error) {
	var none entities.BatchOperationOutcome
	caller, organization, err := requireBatchRequester(ctx, request.Kind)
	if err != nil {
		return none, err
	}
	request, err = normalizedBatchRequest(request)
	if err != nil {
		return none, err
	}
	matched, err := s.repo.BatchOperation().CountMatching(ctx, request.ProjectID, request.Filter)
	if err != nil {
		return none, err
	}
	preview := entities.BatchOperationPreview{
		Kind:    request.Kind,
		Matched: matched,
		TooMany: matched > entities.MaxBatchOperationInstances,
	}
	if request.DryRun {
		return entities.BatchOperationOutcome{Preview: preview}, nil
	}
	switch {
	case matched == 0:
		return none, apierr.Invalidf("the filter matches no instance of this project, so there is nothing to %s", request.Kind)
	case preview.TooMany:
		return none, apierr.Invalidf("the filter matches %d instances, and one operation takes at most %d: narrow it",
			matched, entities.MaxBatchOperationInstances)
	}
	running, err := s.repo.BatchOperation().CountRunning(ctx, request.ProjectID)
	if err != nil {
		return none, err
	}
	if running >= maxRunningBatchOperations {
		return none, apierr.Invalidf("this project already has %d batch operations running; wait for one to finish, or stop one", running)
	}
	operation := entities.BatchOperation{
		ProjectID:      request.ProjectID,
		OrganizationID: organization,
		Kind:           request.Kind,
		Reason:         request.Reason,
		Filter:         request.Filter,
		RequestedBy:    caller.Username,
		RequestedByID:  caller.ID,
	}
	var started entities.BatchOperation
	err = s.repo.UnitOfWork().Do(ctx, func(ctx context.Context) error {
		started, err = s.repo.BatchOperation().Create(ctx, operation, entities.MaxBatchOperationInstances)
		return err
	})
	if err != nil {
		return none, err
	}
	return entities.BatchOperationOutcome{Preview: preview, Operation: &started}, nil
}

func (s *batchOperationService) BatchOperation(ctx context.Context, id uuid.UUID) (entities.BatchOperation, error) {
	if _, _, err := requireBatchOperator(ctx); err != nil {
		return entities.BatchOperation{}, err
	}
	return s.repo.BatchOperation().Get(ctx, id)
}

func (s *batchOperationService) ListBatchOperations(ctx context.Context, projectID uuid.UUID) ([]entities.BatchOperation, error) {
	if _, _, err := requireBatchOperator(ctx); err != nil {
		return nil, err
	}
	return s.repo.BatchOperation().ListByProject(ctx, projectID, batchOperationListLimit)
}

func (s *batchOperationService) BatchOperationItems(ctx context.Context, id uuid.UUID, status entities.BatchItemStatus, page repocontracts.Pagination) (repocontracts.Page[entities.BatchItem], error) {
	if _, _, err := requireBatchOperator(ctx); err != nil {
		return repocontracts.Page[entities.BatchItem]{}, err
	}
	switch status {
	case "", entities.BatchItemPending, entities.BatchItemSucceeded, entities.BatchItemFailed, entities.BatchItemSkipped:
	default:
		return repocontracts.Page[entities.BatchItem]{}, apierr.Invalidf("status must be pending, succeeded, failed or skipped")
	}
	return s.repo.BatchOperation().Items(ctx, id, status, page)
}

// StopBatchOperation stops an operation of the caller's organization. Who
// stopped it is kept with why. Stopping one that is already over answers it
// as it is: there is nothing left to stop, and nothing to refuse.
func (s *batchOperationService) StopBatchOperation(ctx context.Context, id uuid.UUID, reason string) (entities.BatchOperation, error) {
	caller, _, err := requireBatchOperator(ctx)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	reason, err = batchReason(reason)
	if err != nil {
		return entities.BatchOperation{}, err
	}
	return s.repo.BatchOperation().Stop(ctx, id, caller.Username, reason, s.now())
}

// requireBatchRequester answers who is asking for an operation and the
// organization it is for, and refuses whoever could not make the act on one
// instance: an operator's for a retry, an administrator's for a cancel.
func requireBatchRequester(ctx context.Context, kind entities.BatchOperationKind) (entities.User, uuid.UUID, error) {
	caller, organization, err := requireBatchOperator(ctx)
	if err != nil {
		return entities.User{}, uuid.Nil, err
	}
	switch kind {
	case entities.BatchRetry:
	case entities.BatchCancel:
		if !administers(caller, organization) {
			return entities.User{}, uuid.Nil, apierr.Forbiddenf("only an administrator can cancel instances")
		}
	default:
		return entities.User{}, uuid.Nil, apierr.Invalidf("kind must be retry or cancel")
	}
	if caller.ID == uuid.Nil {
		return entities.User{}, uuid.Nil, apierr.Forbiddenf("a batch operation acts as the account that asked for it, and this request carries none")
	}
	return caller, organization, nil
}

// requireBatchOperator answers the caller and the organization the request is
// for, and refuses anybody who does not run that organization day to day.
func requireBatchOperator(ctx context.Context) (entities.User, uuid.UUID, error) {
	caller := signedIn(ctx)
	organization := entities.ActingOrganization(ctx)
	if caller == nil || caller.Username == "" || !operates(*caller, organization) {
		return entities.User{}, uuid.Nil, apierr.Forbiddenf("only an operator or an administrator of the organization can run batch operations")
	}
	return *caller, organization, nil
}

// operates reports whether an account may retry an instance of the
// organization: an operator or an administrator of it.
func operates(account entities.User, organization uuid.UUID) bool {
	return organization != uuid.Nil &&
		(account.HoldsRoleIn(organization, entities.RoleAdmin) || account.HoldsRoleIn(organization, entities.RoleOperator))
}

// normalizedBatchRequest makes a request ready to count and refuses one that
// is malformed: no project, a status the instance list does not know, and a
// missing or overlong reason.
func normalizedBatchRequest(request entities.BatchOperationRequest) (entities.BatchOperationRequest, error) {
	if request.ProjectID == uuid.Nil {
		return request, apierr.Invalidf("name the project whose instances to %s", request.Kind)
	}
	request.Filter.Status = strings.TrimSpace(request.Filter.Status)
	request.Filter.BusinessKey = strings.TrimSpace(request.Filter.BusinessKey)
	if request.Filter.Status != "" && !knownProcessStatus(request.Filter.Status) {
		return request, apierr.Invalidf("status %q is not one an instance can be in", request.Filter.Status)
	}
	if utf8.RuneCountInString(request.Filter.BusinessKey) > entities.MaxBusinessKeyLength {
		return request, apierr.Invalidf("business_key is longer than %d characters", entities.MaxBusinessKeyLength)
	}
	reason, err := batchReason(request.Reason)
	if err != nil {
		return request, err
	}
	request.Reason = reason
	return request, nil
}

func knownProcessStatus(status string) bool {
	for _, known := range models.ProcessStatuses {
		if string(known) == status {
			return true
		}
	}
	return false
}

// batchReason is the reason as it is kept: trimmed, and refused when there is
// none or it is longer than a ledger row's. A cancel writes it to every
// instance's ledger, so it is held to the same bound.
func batchReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "", apierr.Invalidf("say why: a batch operation is kept with its reason")
	case utf8.RuneCountInString(reason) > entities.MaxDeviationReasonLength:
		return "", apierr.Invalidf("the reason is longer than %d characters", entities.MaxDeviationReasonLength)
	}
	return reason, nil
}
