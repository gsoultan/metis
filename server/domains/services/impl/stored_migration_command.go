package impl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// storedMigrationCommand is a migration as a request keeps it: what was asked
// for, in names of its own.
//
// It is written out field by field and not marshalled from the options, for
// the reason a stored waive is (storedWaiveCommand): a request is a record
// somebody reads a month later, and it must not change shape when the
// options gain a field. An approved migration is run from this and from
// nothing else the request holds. Who asked is not in it — the request names
// them — and neither is any approval.
type storedMigrationCommand struct {
	SourceDefinitionID string                                 `json:"source_definition_id"`
	TargetDefinitionID string                                 `json:"target_definition_id"`
	NodeMapping        map[string]string                      `json:"node_mapping"`
	NodeActions        map[string]servicecontracts.NodeAction `json:"node_actions"`
	Instances          []string                               `json:"instances"`
	Acknowledge        []string                               `json:"acknowledge"`
}

// versions is the two versions the stored migration moves between.
func (c storedMigrationCommand) versions() (source, target uuid.UUID, err error) {
	source, err = uuid.Parse(c.SourceDefinitionID)
	if err != nil || source == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("the stored migration names no version to move from (%q)", c.SourceDefinitionID)
	}
	target, err = uuid.Parse(c.TargetDefinitionID)
	if err != nil || target == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("the stored migration names no version to move to (%q)", c.TargetDefinitionID)
	}
	return source, target, nil
}

// options is the stored migration as the options an apply takes, authorised
// by whoever asked for it: actor is their name, and account their account,
// which is what the apply's gate admits the run on. It names no approval: the
// gate reads that from the request.
//
// A decision of a kind no migration makes, or an instance that is no id, is
// refused: the command was written by this server, so one that cannot be read
// whole is the server's trouble, and nothing runs on a command read as partly
// empty.
func (c storedMigrationCommand) options(actor string, account uuid.UUID) ([]servicecontracts.MigrationOption, error) {
	for nodeID, action := range c.NodeActions {
		switch action.Kind {
		case servicecontracts.NodeActionSkip, servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold:
		default:
			return nil, fmt.Errorf("the stored migration decides %q in a way no migration does (%q)", nodeID, action.Kind)
		}
	}
	instances := make([]uuid.UUID, 0, len(c.Instances))
	for _, raw := range c.Instances {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("the stored migration names an instance that is no id (%q)", raw)
		}
		instances = append(instances, id)
	}
	return []servicecontracts.MigrationOption{
		servicecontracts.WithAcknowledgedHolds(c.Acknowledge...),
		servicecontracts.WithNodeActions(c.NodeActions),
		servicecontracts.WithInstances(instances...),
		servicecontracts.WithActor(actor),
		servicecontracts.WithActorAccount(account),
	}, nil
}

// migrationCommandDocument is a migration as its request stores it
// (storedMigrationCommand). Every map and list is copied, and is there even
// when it holds nothing: stored, a nil one reads back as null.
func migrationCommandDocument(
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
) map[string]any {
	mapping := make(map[string]any, len(nodeMapping))
	for from, to := range nodeMapping {
		mapping[from] = to
	}
	actions := make(map[string]any, len(options.Actions))
	for nodeID, action := range options.Actions {
		actions[nodeID] = map[string]any{"kind": string(action.Kind), "reason": action.Reason}
	}
	instances := make([]any, 0, len(options.Instances))
	for _, id := range options.Instances {
		instances = append(instances, id.String())
	}
	acknowledged := make([]any, 0, len(options.Acknowledged))
	for _, nodeID := range options.Acknowledged {
		acknowledged = append(acknowledged, nodeID)
	}
	return map[string]any{
		"source_definition_id": sourceDefID.String(),
		"target_definition_id": targetDefID.String(),
		"node_mapping":         mapping,
		"node_actions":         actions,
		"instances":            instances,
		"acknowledge":          acknowledged,
	}
}

// migrationCommandFrom reads a stored migration back. A document with a
// field nothing here wrote, or that is not such a document at all, is refused
// with a plain error, as a stored waive is (waiveCommandFrom).
func migrationCommandFrom(doc map[string]any) (storedMigrationCommand, error) {
	var none storedMigrationCommand
	if len(doc) == 0 {
		return none, errors.New("the stored migration is empty")
	}
	written, err := json.Marshal(doc)
	if err != nil {
		return none, fmt.Errorf("the stored migration cannot be written out to be read: %w", err)
	}
	var stored storedMigrationCommand
	decoder := json.NewDecoder(bytes.NewReader(written))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return none, fmt.Errorf("the stored migration cannot be read: %w", err)
	}
	return stored, nil
}

// migrationPlanDocument is the plan a requester was shown, as the request
// keeps it for whoever is asked to approve: the plan as the migrate route
// answers it, under the same names, and because — why it needs somebody else.
//
// It is for reading. Nothing is decided from it: an approval plans again from
// the stored command, and so does the apply. Its lists are bounded by the two
// graphs — one entry for each step, never one for each instance.
func migrationPlanDocument(plan any, because []string) (map[string]any, error) {
	written, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("the plan cannot be written out to be stored: %w", err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(written, &doc); err != nil {
		return nil, fmt.Errorf("the plan cannot be stored as a document: %w", err)
	}
	reasons := make([]any, 0, len(because))
	for _, reason := range slices.Clone(because) {
		reasons = append(reasons, reason)
	}
	doc = maps.Clone(doc)
	doc[entities.PlanBecauseKey] = reasons
	return doc, nil
}
