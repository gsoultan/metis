package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/validation"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
)

// maxReleaseModels bounds what one release carries, references followed. A
// project's release is tens of models; a bound makes a reference loop or a
// runaway request an error rather than a long walk.
const maxReleaseModels = 200

// maxReleasesListed bounds a list of a project's releases.
const maxReleasesListed = 200

type releaseService struct {
	repo        repositories.Repository
	definitions contracts.DefinitionService
	decisions   contracts.DecisionService
	connectors  contracts.ConnectorService
}

// NewReleaseService returns the service that cuts, imports and makes live
// the releases of a project's models.
func NewReleaseService(
	repo repositories.Repository,
	definitions contracts.DefinitionService,
	decisions contracts.DecisionService,
	connectors contracts.ConnectorService,
) contracts.ReleaseService {
	return &releaseService{repo: repo, definitions: definitions, decisions: decisions, connectors: connectors}
}

// CutRelease records the live versions of the named models as a release.
func (s *releaseService) CutRelease(ctx context.Context, req contracts.CutRelease) (entities.Release, error) {
	name, err := releaseName(req.Name)
	if err != nil {
		return entities.Release{}, err
	}
	if len(req.Processes) == 0 && len(req.Decisions) == 0 {
		return entities.Release{}, apierr.Invalidf("name at least one process or decision to release")
	}
	bundle := entities.ReleaseBundle{
		Format: entities.ReleaseFormat, Name: name, CutAt: time.Now().UTC(), CutBy: releaseActor(ctx),
	}
	var items []entities.ReleaseItem
	connections := map[string]bool{}

	processes, decisions := slices.Clone(req.Processes), slices.Clone(req.Decisions)
	seenProcess, seenDecision := map[string]bool{}, map[string]bool{}
	for len(processes) > 0 || len(decisions) > 0 {
		if len(seenProcess)+len(seenDecision) > maxReleaseModels {
			return entities.Release{}, apierr.Invalidf("a release carries at most %d models, references included", maxReleaseModels)
		}
		if len(processes) > 0 {
			key := strings.TrimSpace(processes[0])
			processes = processes[1:]
			if key == "" || seenProcess[key] {
				continue
			}
			seenProcess[key] = true
			def, err := s.liveProcess(ctx, req.ProjectID, key)
			if err != nil {
				return entities.Release{}, err
			}
			if step := def.PinnedVersionStep(); step != "" {
				return entities.Release{}, apierr.Invalidf(
					"process %s: step %s names a fixed version of another model; version numbers differ between environments, so a release cannot carry it — let the step use the live version",
					key, step)
			}
			if err := s.nameConnections(ctx, req.ProjectID, def, connections); err != nil {
				return entities.Release{}, err
			}
			items = append(items, entities.ReleaseItem{Kind: entities.ReleaseItemProcess, Key: key, SourceVersion: def.Version, Version: def.Version})
			processes = append(processes, def.CalledProcessKeys()...)
			decisions = append(decisions, def.DecisionKeys()...)
			bundle.Processes = append(bundle.Processes, portableProcess(def))
			continue
		}
		key := strings.TrimSpace(decisions[0])
		decisions = decisions[1:]
		if key == "" || seenDecision[key] {
			continue
		}
		seenDecision[key] = true
		m, err := s.repo.Decision().GetLiveByKey(ctx, req.ProjectID, key)
		if err != nil {
			return entities.Release{}, fmt.Errorf("decision %s: %w", key, err)
		}
		d := adapters.DecisionEntityAdapter{Model: m}.ToEntity()
		items = append(items, entities.ReleaseItem{Kind: entities.ReleaseItemDecision, Key: key, SourceVersion: d.Version, Version: d.Version})
		decisions = append(decisions, d.RequiredDecisions...)
		bundle.Decisions = append(bundle.Decisions, portableDecision(d))
	}

	// Sorted, so the same models cut twice hash the same.
	slices.SortFunc(bundle.Processes, func(a, b *entities.ProcessDefinition) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(bundle.Decisions, func(a, b entities.DecisionDefinition) int { return strings.Compare(a.Key, b.Key) })
	bundle.Connections = sortedKeys(connections)
	if bundle.Processes == nil {
		bundle.Processes = []*entities.ProcessDefinition{}
	}
	if bundle.Decisions == nil {
		bundle.Decisions = []entities.DecisionDefinition{}
	}

	hash, err := entities.HashReleaseBundle(bundle)
	if err != nil {
		return entities.Release{}, err
	}
	return s.repo.Release().Create(ctx, entities.Release{
		Project: &entities.Project{ID: req.ProjectID},
		Name:    name, Hash: hash,
		Origin: entities.ReleaseOriginCut, State: entities.ReleaseCut,
		Bundle: bundle, Items: items,
		CutBy: bundle.CutBy, ActedBy: bundle.CutBy,
	})
}

// liveProcess reads the version of key new instances start on here.
func (s *releaseService) liveProcess(ctx context.Context, projectID uuid.UUID, key string) (*entities.ProcessDefinition, error) {
	m, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, key)
	if err != nil {
		return nil, fmt.Errorf("process %s: %w", key, err)
	}
	return adapters.DefinitionEntityAdapter{Model: m}.ToEntity(), nil
}

// nameConnections rewrites every step that names its connection by id to
// name it by name, which is what is the same in the next environment.
func (s *releaseService) nameConnections(ctx context.Context, projectID uuid.UUID, def *entities.ProcessDefinition, names map[string]bool) error {
	var instances []entities.ConnectorInstance
	var failure error
	def.WalkNodes(func(n *entities.Node) {
		if failure != nil || !n.NamesConnector() {
			return
		}
		name := strings.TrimSpace(n.GetStringProperty(entities.ConnectionProperty))
		if name == "" {
			if id := n.GetStringProperty("connector_instance_id"); id != "" {
				name, failure = s.connectionNamed(ctx, id)
			} else {
				if instances == nil {
					instances, failure = s.connectors.ListConnectorInstances(ctx, projectID)
				}
				if failure == nil {
					name, failure = connectionForConnector(instances, n.GetStringProperty("connector_id"))
				}
			}
			if failure != nil {
				failure = fmt.Errorf("process %s, step %s: %w", def.Key, n.ID, failure)
				return
			}
		}
		n.Properties[entities.ConnectionProperty] = name
		delete(n.Properties, "connector_instance_id")
		delete(n.Properties, "connector_id")
		names[name] = true
	})
	return failure
}

func (s *releaseService) connectionNamed(ctx context.Context, id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", apierr.Invalidf("its connection id %q is not an id", id)
	}
	inst, err := s.connectors.GetConnectorInstance(ctx, parsed)
	if err != nil {
		return "", fmt.Errorf("its connection: %w", err)
	}
	return inst.Name, nil
}

// connectionForConnector is the project's one connection to a catalogue
// connector. Two would leave the next environment to guess.
func connectionForConnector(instances []entities.ConnectorInstance, connectorID string) (string, error) {
	var found []string
	for _, inst := range instances {
		if inst.Connector != nil && inst.Connector.ID.String() == connectorID {
			found = append(found, inst.Name)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", apierr.Invalidf("this project has no connection to its connector")
	default:
		return "", apierr.Invalidf("this project has %d connections to its connector (%s); choose one on the step", len(found), strings.Join(found, ", "))
	}
}

// portableProcess strips what only means something in this runtime.
func portableProcess(def *entities.ProcessDefinition) *entities.ProcessDefinition {
	def.ID, def.Project, def.Version, def.CreatedAt, def.Deployment = uuid.Nil, nil, 0, time.Time{}, nil
	return def
}

func portableDecision(d entities.DecisionDefinition) entities.DecisionDefinition {
	d.ID, d.Project, d.Version, d.CreatedAt = uuid.Nil, nil, 0, time.Time{}
	return d
}

// ImportReleaseFrom imports a release cut in another runtime of the project.
func (s *releaseService) ImportReleaseFrom(ctx context.Context, projectID, sourceEnvironment uuid.UUID, name string) (entities.Release, error) {
	here, _ := db.EnvironmentFrom(ctx)
	if here == sourceEnvironment {
		return entities.Release{}, apierr.Invalidf("this is the environment the release is in; import it into the next one")
	}
	if sourceEnvironment != uuid.Nil {
		env, err := s.repo.Environment().Get(ctx, sourceEnvironment)
		if err != nil || uuid.UUID(env.ProjectID) != projectID {
			return entities.Release{}, fmt.Errorf("%w: this project has no such environment", apierr.ErrNotFound)
		}
	}
	source, err := s.repo.Release().GetByName(db.Bind(ctx, sourceEnvironment), projectID, strings.TrimSpace(name))
	if err != nil {
		return entities.Release{}, err
	}
	hash, err := entities.HashReleaseBundle(source.Bundle)
	if err != nil {
		return entities.Release{}, err
	}
	if hash != source.Hash {
		return entities.Release{}, apierr.Invalidf("release %s has changed since it was cut, so it is not imported; cut a new one", source.Name)
	}
	return s.importBundle(ctx, projectID, source.Bundle, hash)
}

// importBundle stores a release's models here, staged, after checking it can
// run here.
func (s *releaseService) importBundle(ctx context.Context, projectID uuid.UUID, bundle entities.ReleaseBundle, hash string) (entities.Release, error) {
	if bundle.Format != entities.ReleaseFormat {
		return entities.Release{}, apierr.Invalidf("this release is %q, which this server does not read", bundle.Format)
	}
	existing, err := s.repo.Release().GetByName(ctx, projectID, bundle.Name)
	switch {
	case err == nil && existing.Hash == hash:
		return existing, nil
	case err == nil:
		return entities.Release{}, apierr.Invalidf("a different release named %s is already here", bundle.Name)
	case !errors.Is(err, apierr.ErrNotFound):
		return entities.Release{}, err
	}
	if err := s.requireConnections(ctx, projectID, bundle.Connections); err != nil {
		return entities.Release{}, err
	}
	work, err := copyBundle(bundle)
	if err != nil {
		return entities.Release{}, err
	}
	actor := releaseActor(ctx)
	var imported entities.Release
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		var items []entities.ReleaseItem
		for _, d := range work.Decisions {
			item, err := s.stageDecision(txCtx, projectID, d)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		for _, def := range work.Processes {
			item, err := s.stageProcess(txCtx, projectID, def)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		created, err := s.repo.Release().Create(txCtx, entities.Release{
			Project: &entities.Project{ID: projectID},
			Name:    bundle.Name, Hash: hash,
			Origin: entities.ReleaseOriginImported, State: entities.ReleaseStaged,
			Bundle: bundle, Items: items,
			CutBy: bundle.CutBy, ActedBy: actor,
		})
		imported = created
		return err
	})
	return imported, err
}

// requireConnections refuses a release that calls a connection this runtime
// does not have, naming every one missing.
func (s *releaseService) requireConnections(ctx context.Context, projectID uuid.UUID, names []string) error {
	var missing []string
	for _, name := range names {
		if _, err := s.connectors.GetConnectorInstanceByName(ctx, projectID, name); err != nil {
			if !errors.Is(err, apierr.ErrNotFound) {
				return fmt.Errorf("connection %s: %w", name, err)
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return apierr.Invalidf("this environment has no connection named %s; add it here before importing the release",
			strings.Join(missing, ", "))
	}
	return nil
}

// stageDecision stores a decision as a new version of its key, not live. A
// key new here is only checked: its first version would be live the moment
// it was stored.
func (s *releaseService) stageDecision(ctx context.Context, projectID uuid.UUID, d entities.DecisionDefinition) (entities.ReleaseItem, error) {
	item := entities.ReleaseItem{Kind: entities.ReleaseItemDecision, Key: d.Key}
	live, err := s.repo.Decision().GetLiveByKey(ctx, projectID, d.Key)
	if errors.Is(err, apierr.ErrNotFound) {
		if len(d.Outputs) == 0 {
			return item, apierr.Invalidf("decision %s has no output column", d.Key)
		}
		return item, nil
	}
	if err != nil {
		return item, fmt.Errorf("decision %s: %w", d.Key, err)
	}
	d.Project = &entities.Project{ID: projectID}
	saved, err := s.decisions.UpdateDecision(ctx, uuid.UUID(live.ID), d, false)
	if err != nil {
		return item, fmt.Errorf("decision %s: %w", d.Key, err)
	}
	item.Version = saved.Version
	return item, nil
}

// stageProcess stores a process as a new version of its key, not live. A key
// new here is only checked, for the reason stageDecision gives.
func (s *releaseService) stageProcess(ctx context.Context, projectID uuid.UUID, def *entities.ProcessDefinition) (entities.ReleaseItem, error) {
	item := entities.ReleaseItem{Kind: entities.ReleaseItemProcess, Key: def.Key}
	def.Project = &entities.Project{ID: projectID}
	if _, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, def.Key); errors.Is(err, apierr.ErrNotFound) {
		validator := validation.NewVisitor()
		def.Accept(validator)
		if !validator.IsValid() {
			return item, apierr.Invalidf("process %s is not valid: %s", def.Key, strings.Join(validator.Errors(), "; "))
		}
		return item, nil
	} else if err != nil {
		return item, fmt.Errorf("process %s: %w", def.Key, err)
	}
	if _, err := s.definitions.DeployDefinition(ctx, def, false); err != nil {
		return item, fmt.Errorf("process %s: %w", def.Key, err)
	}
	item.Version = def.Version
	return item, nil
}

// GoLive makes every model of a staged release live here, together.
func (s *releaseService) GoLive(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error) {
	rel, err := s.repo.Release().GetByName(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return entities.Release{}, err
	}
	switch {
	case rel.State == entities.ReleaseLive:
		return rel, nil
	case rel.Origin == entities.ReleaseOriginCut:
		return entities.Release{}, apierr.Invalidf("release %s was cut here, from what already runs here; import it into the next environment", rel.Name)
	}
	return s.makeLive(ctx, rel, true)
}

// RollBack makes the release that was live before the live one live again.
//
// A model the live release brought that the previous one did not carry stays
// as it is: there is no earlier version of it to return to.
func (s *releaseService) RollBack(ctx context.Context, projectID uuid.UUID) (entities.Release, error) {
	live, err := s.repo.Release().GetLive(ctx, projectID)
	if err != nil {
		return entities.Release{}, err
	}
	if live.Previous == "" {
		return entities.Release{}, apierr.Invalidf("release %s was the first to go live here, so there is nothing to roll back to", live.Name)
	}
	previous, err := s.repo.Release().GetByName(ctx, projectID, live.Previous)
	if err != nil {
		return entities.Release{}, fmt.Errorf("the release before %s: %w", live.Name, err)
	}
	return s.makeLive(ctx, previous, false)
}

// makeLive puts every model of rel into force in one transaction, decisions
// first so no process goes live consulting a policy that is not. With
// remember set the release it replaces becomes rel's previous one; a rollback
// keeps rel's own, so rolling back again walks further back.
func (s *releaseService) makeLive(ctx context.Context, rel entities.Release, remember bool) (entities.Release, error) {
	projectID := rel.Project.ID
	work, err := copyBundle(rel.Bundle)
	if err != nil {
		return entities.Release{}, err
	}
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		for _, kind := range []string{entities.ReleaseItemDecision, entities.ReleaseItemProcess} {
			for i := range rel.Items {
				if rel.Items[i].Kind != kind {
					continue
				}
				if err := s.putIntoForce(txCtx, projectID, &rel.Items[i], work); err != nil {
					return err
				}
			}
		}
		current, err := s.repo.Release().GetLive(txCtx, projectID)
		switch {
		case err == nil && current.ID != rel.ID:
			current.State = entities.ReleaseSuperseded
			current.ActedBy = releaseActor(txCtx)
			if err := s.repo.Release().Update(txCtx, current); err != nil {
				return err
			}
			if remember {
				rel.Previous = current.Name
			}
		case err != nil && !errors.Is(err, apierr.ErrNotFound):
			return err
		}
		now := time.Now().UTC()
		rel.State, rel.LiveAt, rel.ActedBy = entities.ReleaseLive, &now, releaseActor(txCtx)
		return s.repo.Release().Update(txCtx, rel)
	})
	if err != nil {
		return entities.Release{}, err
	}
	return rel, nil
}

// putIntoForce makes one item live: the staged version, or for a key that was
// new here at import, its first version, stored now.
func (s *releaseService) putIntoForce(ctx context.Context, projectID uuid.UUID, item *entities.ReleaseItem, work entities.ReleaseBundle) error {
	if item.Version != 0 {
		if item.Kind == entities.ReleaseItemDecision {
			return s.decisions.PromoteDecisionVersion(ctx, projectID, item.Key, item.Version)
		}
		return s.definitions.PromoteDefinitionVersion(ctx, projectID, item.Key, item.Version)
	}
	if item.Kind == entities.ReleaseItemDecision {
		for _, d := range work.Decisions {
			if d.Key != item.Key {
				continue
			}
			d.Project = &entities.Project{ID: projectID}
			if _, err := s.decisions.CreateDecision(ctx, d); err != nil {
				return fmt.Errorf("decision %s: %w", d.Key, err)
			}
			live, err := s.repo.Decision().GetLiveByKey(ctx, projectID, d.Key)
			if err != nil {
				return fmt.Errorf("decision %s: %w", d.Key, err)
			}
			item.Version = live.Version
			return nil
		}
	} else {
		for _, def := range work.Processes {
			if def.Key != item.Key {
				continue
			}
			def.Project = &entities.Project{ID: projectID}
			if _, err := s.definitions.DeployDefinition(ctx, def, true); err != nil {
				return fmt.Errorf("process %s: %w", def.Key, err)
			}
			item.Version = def.Version
			return nil
		}
	}
	return fmt.Errorf("release item %s %s is not in the release", item.Kind, item.Key)
}

// ListReleases lists a project's releases here, newest first.
func (s *releaseService) ListReleases(ctx context.Context, projectID uuid.UUID) ([]entities.Release, error) {
	return s.repo.Release().List(ctx, projectID, maxReleasesListed)
}

// GetRelease reads one release here.
func (s *releaseService) GetRelease(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error) {
	return s.repo.Release().GetByName(ctx, projectID, strings.TrimSpace(name))
}

// copyBundle is a bundle to work on: storing a model fills in its id, project
// and version, and the release itself must stay as it was cut.
func copyBundle(b entities.ReleaseBundle) (entities.ReleaseBundle, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return entities.ReleaseBundle{}, fmt.Errorf("could not read the release: %w", err)
	}
	var out entities.ReleaseBundle
	if err := json.Unmarshal(raw, &out); err != nil {
		return entities.ReleaseBundle{}, fmt.Errorf("could not read the release: %w", err)
	}
	return out, nil
}

func releaseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", apierr.Invalidf("give the release a name, such as 2026.11.1")
	case len([]rune(name)) > entities.MaxReleaseNameLength:
		return "", apierr.Invalidf("a release name is at most %d characters", entities.MaxReleaseNameLength)
	}
	return name, nil
}

// actorName is who a change is recorded as made by.
func releaseActor(ctx context.Context) string {
	if caller := signedIn(ctx); caller != nil {
		if caller.Username != "" {
			return caller.Username
		}
		return caller.ID.String()
	}
	return "system"
}
