package impl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocont "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ocelExportLimit is the most audit entries, and the most instances, one OCEL
// export will hold.
//
// The export is built in memory, whole: every entry of the project's trail and
// every instance it has, variables and all, and then the log on top of them. A
// project that has run for a year holds millions of entries, and one request
// for its log could take the replica's memory with it — the pod is killed, and
// every job it had claimed waits out its lease. A hundred thousand events is a
// log a mining tool opens comfortably; past it the export is refused with the
// number in the message rather than started and abandoned.
const ocelExportLimit = 100_000

// ExportOCEL reads a project's audit trail as an OCEL 2.0 object-centric event
// log.
//
// Scope comes from the repository, not from here: Audit().ListByProject and
// Process().ListByProject both apply the tenant scope, so a caller asking for a
// project in another organization gets an empty log rather than a refusal they
// could use to probe for which project ids exist.
func (e *Engine) ExportOCEL(ctx context.Context, projectID uuid.UUID, opts entities.OCELOptions) (entities.OCELLog, error) {
	return e.exportOCEL(ctx, projectID, opts, ocelExportLimit)
}

// exportOCEL is ExportOCEL with the limit as a parameter, so a test can reach
// it without writing a hundred thousand rows.
func (e *Engine) exportOCEL(ctx context.Context, projectID uuid.UUID, opts entities.OCELOptions, limit int64) (entities.OCELLog, error) {
	// Counted before anything is read: the instances are the heavier rows, and
	// a count is one grouped query.
	counts, err := e.repo.Process().CountByStatuses(ctx, projectID, repocont.InstanceFilter{})
	if err != nil {
		return entities.OCELLog{}, fmt.Errorf("count the instances of project %s: %w", projectID, err)
	}
	var instanceCount int64
	for _, n := range counts {
		instanceCount += n
	}
	if instanceCount > limit {
		return entities.OCELLog{}, ocelTooLarge(projectID, fmt.Sprintf("%d instances", instanceCount), limit)
	}

	// One more than the limit, so that a trail of exactly the limit is told
	// apart from a longer one.
	entries, err := e.repo.Audit().ListByProject(ctx, projectID, limit+1)
	if err != nil {
		return entities.OCELLog{}, fmt.Errorf("read the audit trail for project %s: %w", projectID, err)
	}
	if int64(len(entries)) > limit {
		return entities.OCELLog{}, ocelTooLarge(projectID, "more audit entries than that", limit)
	}

	instances, err := e.repo.Process().ListByProject(ctx, projectID)
	if err != nil {
		return entities.OCELLog{}, fmt.Errorf("read the instances for project %s: %w", projectID, err)
	}

	known, err := e.casesOf(ctx, instances)
	if err != nil {
		return entities.OCELLog{}, fmt.Errorf("read the versions the instances of project %s run: %w", projectID, err)
	}

	audit := make([]entities.AuditEntry, len(entries))
	for i, m := range entries {
		audit[i] = adapters.AuditEntityAdapter{Model: m}.ToEntity()
	}

	return buildOCELLog(audit, known, opts), nil
}

// ocelTooLarge is the refusal of an export past the limit: a 400 that says
// what there was too much of and where the limit is, so the caller is not left
// guessing whether a retry would help.
func ocelTooLarge(projectID uuid.UUID, what string, limit int64) error {
	return apierr.Invalidf("project %s is too large to export as one OCEL log: "+
		"an export holds at most %d audit entries and %d instances, and this project has %s",
		projectID, limit, limit, what)
}

// casesOf is the project's instances as the log's cases, each naming the key
// and version of the definition it runs.
//
// An instance row carries only its definition's id, and the export used it as
// it came: every case said key "" and version 0, so a case of v3 and one of v4
// were the same case to a miner, and none was related to a definition object.
// The definitions are read here once per version rather than once per
// instance, through the engine's cache — keyed by tenant, and already holding
// the versions that are running.
//
// The version is the one an instance runs now. A case a migration moved runs
// its new version; its instance_migrated event records which it came from.
func (e *Engine) casesOf(ctx context.Context, instances []models.ProcessInstanceModel) (map[uuid.UUID]entities.ProcessInstance, error) {
	cases := make(map[uuid.UUID]entities.ProcessInstance, len(instances))
	versions := make(map[uuid.UUID]*entities.ProcessDefinition)
	for _, m := range instances {
		instance := adapters.InstanceEntityAdapter{Model: m}.ToEntity()
		version, err := e.versionOf(ctx, uuid.UUID(m.DefinitionID), versions)
		if err != nil {
			return nil, err
		}
		if version != nil {
			instance.Definition = version
		}
		cases[instance.ID] = instance
	}
	return cases, nil
}

// versionOf names the definition id refers to — its id, key and version, and
// nothing of its graph — remembering the answer in seen for the rest of the
// export.
//
// Nil when there is no such definition to read any more: deleted since the
// instance ran. That case is exported without a version rather than failing
// the whole export over it, or being given one it did not run.
func (e *Engine) versionOf(ctx context.Context, id uuid.UUID, seen map[uuid.UUID]*entities.ProcessDefinition) (*entities.ProcessDefinition, error) {
	if version, ok := seen[id]; ok {
		return version, nil
	}
	definition, err := e.loadDefinition(ctx, id)
	if errors.Is(err, apierr.ErrNotFound) {
		seen[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read definition %s: %w", id, err)
	}
	version := &entities.ProcessDefinition{ID: definition.ID, Key: definition.Key, Version: definition.Version}
	seen[id] = version
	return version, nil
}

// buildOCELLog is the whole transformation, separated from the reads so it can
// be tested against a fixed set of entries rather than a database.
func buildOCELLog(
	entries []entities.AuditEntry,
	instances map[uuid.UUID]entities.ProcessInstance,
	opts entities.OCELOptions,
) entities.OCELLog {
	log := entities.OCELLog{
		Objects: []entities.OCELObject{},
		Events:  []entities.OCELEvent{},
	}

	seenInstance := map[uuid.UUID]bool{}
	seenDefinition := map[string]bool{}
	eventTypes := map[string]bool{}

	for _, entry := range entries {
		if entry.Instance == nil || entry.Instance.ID == uuid.Nil {
			// An entry with no instance describes the project rather than a
			// case. OCEL has no notion of an event outside every object, and
			// inventing one would put a phantom case in every discovered model.
			continue
		}
		instanceID := entry.Instance.ID

		if !seenInstance[instanceID] {
			seenInstance[instanceID] = true
			object := entities.OCELObject{
				ID:         instanceID.String(),
				Type:       entities.OCELObjectProcessInstance,
				Attributes: []entities.OCELObjectAttr{},
			}
			if inst, ok := instances[instanceID]; ok {
				object.Attributes = instanceAttributes(inst, entry.Timestamp)
				if key := definitionKey(inst); key != "" {
					if !seenDefinition[key] {
						seenDefinition[key] = true
						log.Objects = append(log.Objects, entities.OCELObject{
							ID:         key,
							Type:       entities.OCELObjectDefinition,
							Attributes: []entities.OCELObjectAttr{},
						})
					}
					// The instance-to-definition edge is what lets a miner
					// separate one process's cases from another's in a project
					// that runs several.
					object.Relationships = []entities.OCELRelationship{
						{ObjectID: key, Qualifier: entities.OCELQualifierDefines},
					}
				}
			}
			log.Objects = append(log.Objects, object)
		}

		activity := activityName(entry)
		eventTypes[activity] = true

		event := entities.OCELEvent{
			ID:         entry.ID.String(),
			Type:       activity,
			Time:       entry.Timestamp,
			Attributes: eventAttributes(entry, opts),
			Relationships: []entities.OCELRelationship{
				{ObjectID: instanceID.String(), Qualifier: entities.OCELQualifierInstance},
			},
		}
		log.Events = append(log.Events, event)
	}

	log.ObjectTypes = []entities.OCELType{
		{
			Name: entities.OCELObjectProcessInstance,
			Attributes: []entities.OCELAttributeDecl{
				{Name: "status", Type: "string"},
				{Name: "definition_key", Type: "string"},
				{Name: "definition_version", Type: "string"},
			},
		},
		{Name: entities.OCELObjectDefinition, Attributes: []entities.OCELAttributeDecl{}},
	}
	log.EventTypes = declaredEventTypes(eventTypes, opts)
	return log
}

// activityName is what a mining tool will label the box in the discovered model.
//
// It is the node's name, because that is the only thing in an audit entry a
// business reader recognises. The entry's Type — node_reached, task_completed —
// is the lifecycle transition, and using it as the activity would discover a
// model with four boxes in it no matter how large the process is. Entries with
// no node at all are process-level, and there the type *is* the activity.
func activityName(entry entities.AuditEntry) string {
	if entry.Node != nil {
		if entry.Node.Name != "" {
			return entry.Node.Name
		}
		if entry.Node.ID != "" {
			return entry.Node.ID
		}
	}
	return entry.Type
}

func eventAttributes(entry entities.AuditEntry, opts entities.OCELOptions) []entities.OCELEventAttr {
	attrs := []entities.OCELEventAttr{
		// The audit type travels as an attribute rather than as the activity,
		// so a miner that wants lifecycle-aware discovery can still find it.
		{Name: "lifecycle", Value: entry.Type},
	}
	if entry.Node != nil && entry.Node.ID != "" {
		attrs = append(attrs, entities.OCELEventAttr{Name: "node_id", Value: entry.Node.ID})
	}
	if entry.Message != "" {
		attrs = append(attrs, entities.OCELEventAttr{Name: "message", Value: entry.Message})
	}

	if !opts.IncludeVariables {
		return attrs
	}
	// Sorted, because a map's iteration order is random and an export that
	// reorders its own columns between two runs is one nobody can diff.
	keys := make([]string, 0, len(entry.Data))
	for k := range entry.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		attrs = append(attrs, entities.OCELEventAttr{
			Name:  k,
			Value: fmt.Sprintf("%v", entry.Data[k]),
		})
	}
	return attrs
}

// declaredEventTypes lists every activity the log contains. OCEL requires the
// declaration, and a reader validates events against it.
func declaredEventTypes(names map[string]bool, opts entities.OCELOptions) []entities.OCELType {
	declared := []entities.OCELAttributeDecl{
		{Name: "lifecycle", Type: "string"},
		{Name: "node_id", Type: "string"},
		{Name: "message", Type: "string"},
	}
	if opts.IncludeVariables {
		// Variable names are per-process and not knowable up front, so the
		// declaration cannot enumerate them. Saying so is better than emitting
		// a list that is wrong for every process but the one it was built from.
		declared = append(declared, entities.OCELAttributeDecl{Name: "*", Type: "string"})
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	out := make([]entities.OCELType, 0, len(sorted))
	for _, name := range sorted {
		out = append(out, entities.OCELType{Name: name, Attributes: declared})
	}
	return out
}

func instanceAttributes(inst entities.ProcessInstance, at time.Time) []entities.OCELObjectAttr {
	attrs := []entities.OCELObjectAttr{
		{Name: "status", Time: at, Value: string(inst.Status)},
	}
	// Only a definition that was read. One known by its id alone would say key ""
	// and version 0, which files the case under a process that never existed —
	// the same test definitionKey makes before relating the case to one.
	if inst.Definition != nil && inst.Definition.Key != "" {
		attrs = append(attrs,
			entities.OCELObjectAttr{Name: "definition_key", Time: at, Value: inst.Definition.Key},
			entities.OCELObjectAttr{Name: "definition_version", Time: at, Value: fmt.Sprintf("%d", inst.Definition.Version)},
		)
	}
	return attrs
}

// definitionKey identifies the definition an instance belongs to, version
// included: two versions of a process are two different control flows, and
// merging their cases discovers a model that is neither.
func definitionKey(inst entities.ProcessInstance) string {
	if inst.Definition == nil || inst.Definition.Key == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", inst.Definition.Key, inst.Definition.Version)
}
