package pg

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/storm/runtime"
	"github.com/rs/zerolog/log"
)

// emptyList is what a request stores for a list of instances with none in it.
// The column is NOT NULL and has no default, as the ledger's documents are.
var emptyList = runtime.JSON(`[]`)

// stageRequest sets every column of a request that waits. A column left unset
// would take its default, and none of the four documents has one: an absent
// map is written as an empty object, never as NULL.
func stageRequest(r entities.DeviationRequest) (deviationrequest.Ins, error) {
	command, err := deviationSealed(r.Command)
	if err != nil {
		return deviationrequest.Ins{}, fmt.Errorf("could not encode the request's command: %w", err)
	}
	plan, err := deviationSealed(r.Plan)
	if err != nil {
		return deviationrequest.Ins{}, fmt.Errorf("could not encode the request's plan: %w", err)
	}
	outcome, err := deviationDetails(r.Outcome)
	if err != nil {
		return deviationrequest.Ins{}, fmt.Errorf("could not encode the request's outcome: %w", err)
	}
	instances, err := instancesJSON(r.ApprovedInstances)
	if err != nil {
		return deviationrequest.Ins{}, fmt.Errorf("could not encode the instances the request covers: %w", err)
	}

	ins := deviationrequest.Create()
	ins.SetID(r.ID)
	ins.SetProjectID(r.Project.ID)
	ins.SetKind(string(r.Kind))
	ins.SetStatus(string(r.Status))
	ins.SetRequestedBy(r.RequestedBy)
	ins.SetRequestedByID(r.RequestedByID)
	ins.SetReason(r.Reason)
	ins.SetCommand(command)
	ins.SetPlan(plan)
	ins.SetFingerprint(r.Fingerprint)
	ins.SetApprovedInstances(instances)
	ins.SetExpiresAt(r.ExpiresAt.UTC())
	ins.SetOutcome(outcome)
	stageRequestSubject(&ins, r)
	// A request that waits is live, and has not been decided.
	holdFingerprint(ins.SetLiveKey, ins.SetLiveKeyNull, r.Status, r.Fingerprint)
	ins.SetDecidedByNull()
	ins.SetDecidedByIDNull()
	ins.SetDecisionReasonNull()
	ins.SetDecidedAtNull()
	return ins, nil
}

// stageRequestSubject sets what the request acts on: an instance for a waive,
// two versions for a migration. What its kind does not use is NULL.
func stageRequestSubject(ins *deviationrequest.Ins, r entities.DeviationRequest) {
	var instance, source, target uuid.UUID
	if r.Instance != nil {
		instance = r.Instance.ID
	}
	if r.SourceDefinition != nil {
		source = r.SourceDefinition.ID
	}
	if r.TargetDefinition != nil {
		target = r.TargetDefinition.ID
	}
	setOrNullUUID(ins.SetInstanceID, ins.SetInstanceIDNull, instance)
	setOrNullUUID(ins.SetSourceDefinitionID, ins.SetSourceDefinitionIDNull, source)
	setOrNullUUID(ins.SetTargetDefinitionID, ins.SetTargetDefinitionIDNull, target)
}

// holdFingerprint writes the live key from the status, and from nothing else:
// the fingerprint while the request is live (waiting, or approved and
// running), NULL once it is over. The project is unique on it, so a second
// live request for the same thing is refused and one that is over no longer
// refuses a new one.
//
// Not through a set-or-NULL helper: an empty fingerprint would then store
// NULL for a live request, which holds nothing. Create refuses one.
func holdFingerprint(set func(string), setNull func(), status entities.DeviationRequestStatus, fingerprint string) {
	if status.Live() {
		set(fingerprint)
		return
	}
	setNull()
}

// stageChange stages one move of a request: the status, the live key that
// follows from it, and whatever else the change names. A field the change
// leaves zero is not assigned, so it stays as stored.
func stageChange(row deviationrequest.Row, change contracts.DeviationRequestChange) (deviationrequest.Mut, error) {
	mut := deviationrequest.Mutate(row)
	mut.SetStatus(string(change.Status))
	mut.SetUpdatedAtNow()
	holdFingerprint(mut.SetLiveKey, mut.SetLiveKeyNull, change.Status, row.Fingerprint)
	if change.DecidedBy != "" {
		mut.SetDecidedBy(change.DecidedBy)
	}
	if change.DecidedByID != uuid.Nil {
		mut.SetDecidedByID(change.DecidedByID)
	}
	if change.DecisionReason != "" {
		mut.SetDecisionReason(change.DecisionReason)
	}
	if !change.DecidedAt.IsZero() {
		mut.SetDecidedAt(change.DecidedAt.UTC())
	}
	if change.Outcome != nil {
		outcome, err := deviationDetails(change.Outcome)
		if err != nil {
			return deviationrequest.Mut{}, fmt.Errorf("could not encode the request's outcome: %w", err)
		}
		mut.SetOutcome(outcome)
	}
	return mut, nil
}

// instancesJSON encodes the instances a request covers as a list of ids.
func instancesJSON(ids []uuid.UUID) (runtime.JSON, error) {
	if len(ids) == 0 {
		return emptyList, nil
	}
	return json.Marshal(ids)
}

// instancesOf decodes what instancesJSON wrote. Never nil: the column always
// holds a list, and a reader that ranges over it or encodes it should not have
// to ask which kind of empty it got.
func instancesOf(raw runtime.JSON) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}
	if len(raw) == 0 {
		return ids, nil
	}
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	if ids == nil {
		return []uuid.UUID{}, nil
	}
	return ids, nil
}

// requestFrom is the one decoder of a whole request.
//
// The command, the plan and the outcome read as maps that are never nil, and
// the instances as a list that is never nil: the columns are NOT NULL, so
// there is always something there.
func requestFrom(row deviationrequest.Row) (entities.DeviationRequest, error) {
	request, err := queuedRequestFrom(queueRowOf(row))
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	command, err := sealedMapOf(row.Command)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("could not decode the command of request %s: %w", request.ID, err)
	}
	plan, err := sealedMapOf(row.Plan)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("could not decode the plan of request %s: %w", request.ID, err)
	}
	instances, err := instancesOf(row.ApprovedInstances)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("could not decode the instances request %s covers: %w", request.ID, err)
	}
	request.Command, request.Plan, request.ApprovedInstances = orEmpty(command), orEmpty(plan), instances
	return request, nil
}

// readableRequestFrom decodes a request with each of its sealed documents
// that opens, and leaves one that does not nil — absent, not empty: a plan
// that could not be read is not a plan that said nothing.
//
// It is requestFrom for a reader who is not about to act on what the request
// asked. A request whose plan no longer opens still has a status, a
// requester and a deadline, and those are what reading, rejecting and closing
// it need. Anything else that cannot be decoded is still an error.
func readableRequestFrom(row deviationrequest.Row) (entities.DeviationRequest, error) {
	request, err := queuedRequestFrom(queueRowOf(row))
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	command, err := sealedMapOf(row.Command)
	if err == nil {
		request.Command = orEmpty(command)
	}
	sayUnopened(request.ID, "command", err)
	plan, err := sealedMapOf(row.Plan)
	if err == nil {
		request.Plan = orEmpty(plan)
	}
	sayUnopened(request.ID, "plan", err)
	instances, err := instancesOf(row.ApprovedInstances)
	if err == nil {
		request.ApprovedInstances = instances
	}
	sayUnopened(request.ID, "instances", err)
	return request, nil
}

// sayUnopened says in the server's log why a stored document of a request
// did not open, when it did not.
//
// The reader is answered the request without it, and told only that the
// document is unavailable. Why — a key that was lost or rotated away, a row
// restored from somewhere else, a value damaged in place — is for whoever
// operates the installation, and is said nowhere else: without this line a
// lost key would show as nothing but requests nobody can approve. It is said
// each time such a request is read, and names the request, the document and
// the failure; nothing of what the document held can be in it, since it did
// not open.
func sayUnopened(request uuid.UUID, document string, err error) {
	if err == nil {
		return
	}
	log.Warn().Str("request", request.String()).Str("document", document).Str("error", err.Error()).
		Msg("A stored document of a request for a second administrator could not be opened. The request is answered " +
			"without it and can still be rejected or expire; it cannot be approved.")
}

// queueRowOf is the part of a whole row the queue reads.
func queueRowOf(row deviationrequest.Row) deviationrequest.QueueRow {
	return deviationrequest.QueueRow{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ProjectID: row.ProjectID,
		Kind: row.Kind, Status: row.Status, InstanceID: row.InstanceID,
		SourceDefinitionID: row.SourceDefinitionID, TargetDefinitionID: row.TargetDefinitionID,
		RequestedBy: row.RequestedBy, RequestedByID: row.RequestedByID, Reason: row.Reason,
		Fingerprint: row.Fingerprint, ExpiresAt: row.ExpiresAt,
		DecidedBy: row.DecidedBy, DecidedByID: row.DecidedByID, DecisionReason: row.DecisionReason,
		DecidedAt: row.DecidedAt, Outcome: row.Outcome,
	}
}

// queuedRequestFrom decodes a request as the queue reads it: everything but
// the command, the plan and the instances it covers, which are left nil —
// absent, not empty.
func queuedRequestFrom(row deviationrequest.QueueRow) (entities.DeviationRequest, error) {
	outcome, err := mapOf(row.Outcome)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("could not decode the outcome of request %s: %w", uuid.UUID(row.ID), err)
	}
	request := entities.DeviationRequest{
		ID:             row.ID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		Project:        &entities.Project{ID: row.ProjectID},
		Kind:           entities.DeviationRequestKind(row.Kind),
		Status:         entities.DeviationRequestStatus(row.Status),
		RequestedBy:    row.RequestedBy,
		RequestedByID:  row.RequestedByID,
		Reason:         row.Reason,
		Fingerprint:    row.Fingerprint,
		ExpiresAt:      row.ExpiresAt,
		DecidedBy:      valueOr(row.DecidedBy),
		DecidedByID:    uuidOr(row.DecidedByID),
		DecisionReason: valueOr(row.DecisionReason),
		Outcome:        orEmpty(outcome),
	}
	if id, ok := row.InstanceID.Get(); ok {
		request.Instance = &entities.ProcessInstance{ID: id}
	}
	if id, ok := row.SourceDefinitionID.Get(); ok {
		request.SourceDefinition = &entities.ProcessDefinition{ID: id}
	}
	if id, ok := row.TargetDefinitionID.Get(); ok {
		request.TargetDefinition = &entities.ProcessDefinition{ID: id}
	}
	if at, ok := row.DecidedAt.Get(); ok {
		request.DecidedAt = &at
	}
	return request, nil
}
