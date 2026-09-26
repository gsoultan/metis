package impl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/validation"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

type definitionService struct {
	repo repositories.Repository
	// onChanged is called after a definition is removed, so anything holding a
	// decoded copy can drop it. Nil until the composition root wires the
	// engine's cache in; a nil check is cheaper than a null object on a path
	// that runs once per deletion.
	onChanged func()
}

// InvalidateWith registers a callback run whenever a definition is removed.
//
// The engine caches decoded definitions, and a cache that outlives its source
// keeps executing something an administrator deleted. Wired by the composition
// root, which is the only place that holds both.
func (s *definitionService) InvalidateWith(onChanged func()) {
	s.onChanged = onChanged
}

// NewDefinitionService creates a new DefinitionService implementation.
// definitionService still satisfies the contract; asserted here because the
// constructor now returns the concrete type and would no longer catch a drift.
var _ servicecontracts.DefinitionService = (*definitionService)(nil)

// NewDefinitionService returns the concrete type, not the interface, so the
// composition root can wire the engine's cache invalidation after both exist —
// the same reason NewExecutionEngine returns *Engine.
func NewDefinitionService(repo repositories.Repository) *definitionService {
	return &definitionService{
		repo: repo,
	}
}

// CreateDefinition deploys a new version and makes it live.
//
// Every caller that predates staging means exactly this, so the behaviour they
// were written against is what they keep.
func (s *definitionService) CreateDefinition(ctx context.Context, def *entities.ProcessDefinition) (uuid.UUID, error) {
	return s.DeployDefinition(ctx, def, true)
}

// DeployDefinition deploys a new version, promoting it only when asked.
func (s *definitionService) DeployDefinition(ctx context.Context, def *entities.ProcessDefinition, promote bool) (uuid.UUID, error) {
	// Use Visitor Pattern to validate definition
	validator := validation.NewVisitor()
	def.Accept(validator)
	if !validator.IsValid() {
		return uuid.Nil, apierr.Invalidf("invalid definition: %s", strings.Join(validator.Errors(), "; "))
	}
	if err := authorizeLookups(ctx, def); err != nil {
		return uuid.Nil, err
	}

	if def.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("could not generate a definition id: %w", err)
		}
		def.ID = id
	}

	// The version series is per project, matching the unique index, so the
	// allocator needs the same project the adapter is about to write.
	var projectID uuid.UUID
	if def.Project != nil {
		projectID = def.Project.ID
	}

	// Staging has to record which version is live *before* the new row exists,
	// because with no release row "live" means "highest" — and the new row is
	// about to be the highest. Skipping this would make "stage" promote, which
	// is the one thing it must not do.
	//
	// Read outside the allocation loop: it does not change between attempts, and
	// a failed attempt rolls back to a savepoint that would undo it.
	if !promote {
		if err := s.pinCurrentLiveVersion(ctx, projectID, def.Key); err != nil {
			return uuid.Nil, err
		}
	}

	err := allocateVersion(ctx, s.repo.UnitOfWork(), "process "+def.Key,
		func(ctx context.Context) (int, error) {
			return s.repo.Definition().NextVersion(ctx, projectID, def.Key)
		},
		func(txCtx context.Context, version int) error {
			def.Version = version
			if err := s.repo.Definition().Create(txCtx, adapters.DefinitionModelAdapter{Definition: def}.ToModel()); err != nil {
				return err
			}
			if !promote {
				return nil
			}
			// In the same transaction as the insert: a deploy that saved the
			// model but not the promotion would report success and leave the
			// previous version live, which is the failure nobody would look for.
			return s.repo.Definition().ScheduleRelease(txCtx, projectID, def.Key, version, time.Now().UTC())
		})
	if err != nil {
		return uuid.Nil, err
	}
	return def.ID, nil
}

// pinCurrentLiveVersion writes down the version that is live by default, so it
// stays live once a higher-numbered one exists.
//
// A key with no versions yet needs no pin: the first deploy is the only
// candidate, and pinning version zero would leave the key permanently resolving
// to a version that does not exist.
func (s *definitionService) pinCurrentLiveVersion(ctx context.Context, projectID uuid.UUID, key string) error {
	if _, err := s.repo.Definition().GetRelease(ctx, projectID, key); err == nil {
		// Somebody has already chosen; staging must not overrule them.
		return nil
	} else if !errors.Is(err, apierr.ErrNotFound) {
		return fmt.Errorf("could not read the live version of %s: %w", key, err)
	}

	current, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, key)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not read the current version of %s: %w", key, err)
	}
	// Effective now: "keep what is live live" is a statement about the present.
	// It deliberately does not touch cutovers already arranged for the future —
	// staging a version must not cancel a scheduled one.
	return s.repo.Definition().ScheduleRelease(ctx, projectID, key, current.Version, time.Now().UTC())
}

// PromoteDefinitionVersion makes one existing version the one new instances
// start on.
//
// The version is checked to exist first. Promoting a number nobody deployed
// would be accepted by the upsert and would then resolve, via GetByKey's
// fallback, to the highest version — so the caller would be told their rollback
// worked while new instances kept starting on the version they were rolling
// back from.
func (s *definitionService) PromoteDefinitionVersion(ctx context.Context, projectID uuid.UUID, key string, version int) error {
	return s.releaseAt(ctx, projectID, key, version, time.Now().UTC())
}

// ScheduleDefinitionVersion arranges for a version to take over at a given time.
//
// Nothing runs at that moment: the live version is resolved from the timeline
// and the clock on every read, so the cutover happens by the time arriving. That
// is why it cannot be missed by a replica that was restarting, and why it needs
// no lock, no leader and no catch-up pass.
func (s *definitionService) ScheduleDefinitionVersion(ctx context.Context, projectID uuid.UUID, key string, version int, activateAt time.Time) error {
	if activateAt.IsZero() {
		return apierr.Invalidf("a scheduled version needs a time to take effect")
	}
	// Refused rather than silently treated as "now". A cutover in the past would
	// take effect the instant it was saved, which is a different act from the one
	// the caller asked for — and, if it landed behind an existing entry, would do
	// nothing at all while reporting success.
	if !activateAt.After(time.Now().UTC()) {
		return apierr.Invalidf("%s is not in the future; use promote to change the live version now",
			activateAt.UTC().Format(time.RFC3339))
	}
	return s.releaseAt(ctx, projectID, key, version, activateAt)
}

// releaseAt is the shared half of promoting and scheduling: both check that the
// version is real and belongs to the project, then write one timeline entry.
func (s *definitionService) releaseAt(ctx context.Context, projectID uuid.UUID, key string, version int, activateAt time.Time) error {
	if version <= 0 {
		return apierr.Invalidf("version must be a positive number, got %d", version)
	}
	target, err := s.repo.Definition().GetByProjectKeyAndVersion(ctx, projectID, key, version)
	if err != nil {
		return err
	}
	if uuid.UUID(target.ProjectID) != projectID {
		return apierr.Invalidf("version %d of %q does not belong to that project", version, key)
	}
	if err := s.repo.Definition().ScheduleRelease(ctx, projectID, key, version, activateAt); err != nil {
		return err
	}
	if s.onChanged != nil {
		s.onChanged()
	}
	return nil
}

// CancelScheduledVersion drops a cutover that has not happened yet.
func (s *definitionService) CancelScheduledVersion(ctx context.Context, projectID uuid.UUID, releaseID uuid.UUID) error {
	if err := s.repo.Definition().DeleteScheduledRelease(ctx, projectID, releaseID); err != nil {
		return err
	}
	if s.onChanged != nil {
		s.onChanged()
	}
	return nil
}

// scheduledVersions returns the cutovers arranged for one key that have not
// happened yet, soonest first.
func (s *definitionService) scheduledVersions(ctx context.Context, projectID uuid.UUID, key string) ([]entities.ScheduledVersion, error) {
	timeline, err := s.repo.Definition().ListReleasesForKey(ctx, projectID, key)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var scheduled []entities.ScheduledVersion
	for _, entry := range timeline {
		if !entry.Scheduled(now) {
			// The timeline is newest first, so everything from here back has
			// already taken effect.
			break
		}
		scheduled = append(scheduled, entities.ScheduledVersion{
			ID:         uuid.UUID(entry.ID),
			Key:        entry.ProcessKey,
			Version:    entry.Version,
			ActivateAt: entry.ActivateAt,
		})
	}
	// Soonest first: the timeline reads newest first, which for future entries is
	// furthest away first, and "what happens next" is the useful order.
	slices.Reverse(scheduled)
	return scheduled, nil
}

// ListDefinitionVersions returns every version of one key with its live flag and
// instance counts.
func (s *definitionService) ListDefinitionVersions(ctx context.Context, projectID uuid.UUID, key string) ([]entities.DefinitionVersionStatus, error) {
	versions, err := s.repo.Definition().ListVersionsByKey(ctx, projectID, key)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, nil
	}

	// Which version is live: the release when there is one, and otherwise the
	// highest — the same rule GetByKey resolves by, restated here rather than
	// inferred, so the page cannot disagree with the engine about what will run.
	live := versions[0].Version
	if release, err := s.repo.Definition().GetRelease(ctx, projectID, key); err == nil {
		live = release.Version
	} else if !errors.Is(err, apierr.ErrNotFound) {
		return nil, fmt.Errorf("could not read the live version of %s: %w", key, err)
	}

	ids := make([]uuid.UUID, 0, len(versions))
	for _, v := range versions {
		ids = append(ids, uuid.UUID(v.ID))
	}
	counts, err := s.repo.Process().CountInstancesByDefinitions(ctx, ids)
	if err != nil {
		return nil, err
	}

	// The soonest pending cutover per version. Read once for the key rather than
	// once per row, and folded in here so the page that renders the timeline is
	// one request rather than two that could disagree with each other.
	scheduled, err := s.scheduledVersions(ctx, projectID, key)
	if err != nil {
		return nil, err
	}
	soonest := make(map[int]entities.ScheduledVersion, len(scheduled))
	for _, entry := range scheduled {
		// scheduledVersions is soonest first, so the first one seen for a version
		// is the one to show.
		if _, seen := soonest[entry.Version]; !seen {
			soonest[entry.Version] = entry
		}
	}

	out := make([]entities.DefinitionVersionStatus, 0, len(versions))
	for _, v := range versions {
		count := counts[uuid.UUID(v.ID)]
		status := entities.DefinitionVersionStatus{
			ID:               uuid.UUID(v.ID),
			Key:              v.Key,
			Name:             v.Name,
			Version:          v.Version,
			CreatedAt:        v.CreatedAt,
			Live:             v.Version == live,
			RunningInstances: count.Running,
			TotalInstances:   count.Total,
		}
		if entry, ok := soonest[v.Version]; ok {
			status.ScheduledFor = entry.ActivateAt
			status.ScheduledReleaseID = entry.ID
		}
		out = append(out, status)
	}
	return out, nil
}

// DeleteDefinition removes a version that has never been used.
//
// A version an instance references cannot be deleted, and the refusal is the
// point. Instances pin their definition by ID: delete the row and a running one
// can never advance again — the engine cannot load the graph it is executing —
// while a finished one loses the record of what it actually ran, which is the
// only account of why it did what it did.
//
// So "safe to delete" means no instance has ever started on it, and no cutover
// is waiting to make it live.
func (s *definitionService) DeleteDefinition(ctx context.Context, id uuid.UUID) error {
	counts, err := s.repo.Process().CountInstancesByDefinitions(ctx, []uuid.UUID{id})
	if err != nil {
		return err
	}
	if count := counts[id]; count.Total > 0 {
		if count.Running > 0 {
			return apierr.Invalidf(
				"this version still has %d running instance(s); they finish on it, so deleting it would strand them",
				count.Running)
		}
		return apierr.Invalidf(
			"this version has run %d instance(s); deleting it would remove the record of what they executed",
			count.Total)
	}
	if err := s.refuseIfScheduled(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Definition().Delete(ctx, id); err != nil {
		return err
	}
	if s.onChanged != nil {
		s.onChanged()
	}
	return nil
}

// refuseIfScheduled refuses to delete a version that a cutover is waiting on.
//
// A version scheduled for next Monday has run nothing yet, so every other check
// in DeleteDefinition passes it. Delete it and the release timeline still names
// it: when the scheduled moment arrives the reader finds no such version and
// falls back to the highest one, so whichever draft happened to be deployed in
// the meantime goes live instead — unattended, at a moment nobody is watching,
// with no error raised anywhere.
//
// A scheduled cutover is a decision somebody has already made. It gets the same
// protection a running instance gets, and for the same reason: the silent
// outcome is the one nobody can undo.
func (s *definitionService) refuseIfScheduled(ctx context.Context, id uuid.UUID) error {
	def, err := s.repo.Definition().Get(ctx, id)
	if err != nil {
		return err
	}
	releases, err := s.repo.Definition().ListReleasesForKey(ctx, uuid.UUID(def.ProjectID), def.Key)
	if err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return nil
		}
		return err
	}
	now := time.Now().UTC()
	for _, release := range releases {
		if release.Version != def.Version || !release.Scheduled(now) {
			continue
		}
		return apierr.Invalidf(
			"version %d of %q is scheduled to go live at %s; cancel that cutover first, "+
				"or the timeline will name a version that is not there and the highest one will go live in its place",
			def.Version, def.Key, release.ActivateAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// ListDefinitionsPaged returns one page of a project's definitions.
//
// The unpaged call returns every version of every process the project has ever
// had. That list only grows, and it exists to pick one process from.
func (s *definitionService) ListDefinitionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[*entities.ProcessDefinition], error) {
	result, err := s.repo.Definition().ListByProjectPaged(ctx, projectID, page)
	if err != nil {
		return repocontracts.Page[*entities.ProcessDefinition]{}, err
	}
	defs := make([]*entities.ProcessDefinition, len(result.Items))
	for i, m := range result.Items {
		def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
		defs[i] = def
	}
	return repocontracts.NewPage(defs, result.Total, page), nil
}

func (s *definitionService) ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error) {
	var ms []models.ProcessDefinitionModel
	var err error
	if projectID != uuid.Nil {
		ms, err = s.repo.Definition().ListByProject(ctx, projectID)
	} else {
		ms, err = s.repo.Definition().List(ctx)
	}
	if err != nil {
		return nil, err
	}
	res := make([]*entities.ProcessDefinition, len(ms))
	for i, m := range ms {
		res[i] = adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *definitionService) GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error) {
	m, err := s.repo.Definition().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return adapters.DefinitionEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *definitionService) ExportDefinition(ctx context.Context, id uuid.UUID) ([]byte, error) {
	def, err := s.GetDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	parser := &BPMNXMLParser{}
	return parser.Export(def)
}

// ImportDefinition deploys BPMN XML into a project.
//
// The project is required, not optional: definitions are looked up under the
// caller's tenant scope, which joins through the project. An import that set no
// project produced a definition the tenant scope could never find — deployed,
// versioned, and permanently invisible to the organization that uploaded it.
func (s *definitionService) ImportDefinition(ctx context.Context, projectID uuid.UUID, xmlContent []byte) (uuid.UUID, error) {
	if projectID == uuid.Nil {
		return uuid.Nil, errors.New("import requires a project_id: a definition without a project is invisible to its own organization")
	}
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(bytes.NewReader(xmlContent))
	if err != nil {
		return uuid.Nil, err
	}
	def.Project = &entities.Project{ID: projectID}
	return s.CreateDefinition(ctx, def)
}

// ListJavaScriptConditions walks every definition the caller can see and
// reports each condition the evaluator chain would hand to JavaScript. This is
// the worklist for the javascript-conditions flag: it ships off, so anything
// reported here is a decision point that will refuse to route until rewritten
// in FEEL. It is deliberately complete rather than paged — a truncated worklist
// reads as "migration done" when it is not — so the scan is batched instead,
// holding one batch of graphs at a time rather than the whole installation.
func (s *definitionService) ListJavaScriptConditions(ctx context.Context) ([]entities.JavaScriptConditionUsage, error) {
	usages := make([]entities.JavaScriptConditionUsage, 0)
	err := s.repo.Definition().ScanWithGraphs(ctx, func(batch []models.ProcessDefinitionModel) error {
		for _, m := range batch {
			def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
			usages = append(usages, collectJavaScriptConditions(def)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return usages, nil
}

// collectJavaScriptConditions finds the `js:` conditions in one definition.
//
// The prefix check mirrors JSExpressionEvaluator exactly — HasPrefix, no
// trimming — because this report is a promise about what that evaluator will
// refuse. A condition it would not treat as JavaScript must not appear here.
func collectJavaScriptConditions(def *entities.ProcessDefinition) []entities.JavaScriptConditionUsage {
	c := &jsConditionCollector{def: def}
	def.Accept(c)
	return c.usages
}

// jsConditionCollector is the DefinitionVisitor behind
// collectJavaScriptConditions. It inspects the two fields the condition chain
// evaluates: sequence-flow conditions and completion conditions. Node.Condition
// is deliberately ignored — it carries timer expressions and script bodies,
// which the javascript-conditions flag does not gate, and reporting them would
// make the worklist lie about what turning the flag on or off changes.
type jsConditionCollector struct {
	def    *entities.ProcessDefinition
	usages []entities.JavaScriptConditionUsage
}

func (c *jsConditionCollector) VisitDefinition(*entities.ProcessDefinition) {}

func (c *jsConditionCollector) VisitFlowNode(n *entities.Node) {
	if n == nil {
		return
	}
	c.record(n.ID, n.Name, "completion condition", n.CompletionCondition)
}

func (c *jsConditionCollector) VisitSequenceFlow(sf *entities.SequenceFlow) {
	if sf == nil {
		return
	}
	c.record(sf.ID, "", "flow condition", sf.Condition)
}

func (c *jsConditionCollector) record(elementID, elementName, where, condition string) {
	if !strings.HasPrefix(condition, "js:") {
		return
	}
	c.usages = append(c.usages, entities.JavaScriptConditionUsage{
		DefinitionID:   c.def.ID,
		DefinitionKey:  c.def.Key,
		DefinitionName: c.def.Name,
		Version:        c.def.Version,
		ElementID:      elementID,
		ElementName:    elementName,
		Where:          where,
		Condition:      condition,
	})
}

// ListScriptTasks walks every definition the caller can see and reports each
// script task in it.
//
// This is an inventory, not a worklist: nothing here is refused and nothing
// needs rewriting today. It exists because the script sandbox bounds wall-clock
// time, recursion and host capability but cannot bound memory — goja offers no
// heap limit — and choosing between a FEEL replacement and out-of-process
// execution is a question about the scripts that exist.
//
// Complete rather than paged, and batched for the same reason
// ListJavaScriptConditions is: a truncated inventory reads as a small problem.
func (s *definitionService) ListScriptTasks(ctx context.Context) ([]entities.ScriptTaskUsage, error) {
	usages := make([]entities.ScriptTaskUsage, 0)
	err := s.repo.Definition().ScanWithGraphs(ctx, func(batch []models.ProcessDefinitionModel) error {
		for _, m := range batch {
			def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
			usages = append(usages, collectScriptTasks(def)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return usages, nil
}

// collectScriptTasks finds the script tasks in one definition.
func collectScriptTasks(def *entities.ProcessDefinition) []entities.ScriptTaskUsage {
	c := &scriptTaskCollector{def: def}
	def.Accept(c)
	return c.usages
}

// scriptTaskCollector is the DefinitionVisitor behind collectScriptTasks.
//
// It keys on the node type rather than on a non-empty Script, because a script
// task with an empty body is still a script task: it is a node somebody will
// fill in, and leaving it out would make the inventory shrink for the wrong
// reason. A non-script node carrying a Script field is ignored for the mirror
// image of that reason — the engine would never hand it to goja.
type scriptTaskCollector struct {
	def    *entities.ProcessDefinition
	usages []entities.ScriptTaskUsage
}

func (c *scriptTaskCollector) VisitDefinition(*entities.ProcessDefinition) {}

func (c *scriptTaskCollector) VisitSequenceFlow(*entities.SequenceFlow) {}

func (c *scriptTaskCollector) VisitFlowNode(n *entities.Node) {
	if n == nil || n.Type != entities.ScriptTask {
		return
	}
	c.usages = append(c.usages, entities.ScriptTaskUsage{
		DefinitionID:   c.def.ID,
		DefinitionKey:  c.def.Key,
		DefinitionName: c.def.Name,
		Version:        c.def.Version,
		NodeID:         n.ID,
		NodeName:       n.Name,
		ScriptFormat:   n.ScriptFormat,
		Script:         n.Script,
		ScriptLength:   len(n.Script),
	})
}

// ListLiveVersions maps each process key in a project to the version new
// instances start on.
//
// Only keys somebody has explicitly promoted appear. An absent key is not an
// error and not a gap: it means nobody has chosen, and both the engine and the
// caller resolve it to the highest version. Returning them all would mean
// reading every definition in the project to compute a number the reader can
// already see.
func (s *definitionService) ListLiveVersions(ctx context.Context, projectID uuid.UUID) (map[string]int, error) {
	releases, err := s.repo.Definition().ListReleases(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// The timeline comes newest first, cutovers still to come included. The
	// live version of a key is the first entry whose time has arrived — the
	// same rule the engine applies when it starts an instance. Every row used to
	// overwrite the one before, which left each key on the oldest version ever
	// promoted.
	now := time.Now()
	live := make(map[string]int, len(releases))
	for _, release := range releases {
		if release.ActivateAt.After(now) {
			continue
		}
		if _, decided := live[release.ProcessKey]; !decided {
			live[release.ProcessKey] = release.Version
		}
	}
	return live, nil
}
