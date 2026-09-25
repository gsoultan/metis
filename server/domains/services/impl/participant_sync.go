package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/syncdue"
	"github.com/gsoultan/metis/server/domains/logic/userimport"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl/participantsource"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/rs/zerolog/log"
)

// syncLockTTL bounds how long one replica may hold a source.
//
// Long enough for a slow directory — the query itself is capped at thirty
// seconds and the writes are per participant — and short enough that a replica
// which dies mid-sync does not park the source until somebody notices.
const syncLockTTL = 5 * time.Minute

type participantSyncService struct {
	sources repocontracts.ParticipantSourceRepository
	// participants is the concrete service rather than its interface, because
	// the syncer needs the write half — the upserts and the group handling —
	// and that is deliberately not on the public contract: fetching a directory
	// is not something a caller should be able to ask for row by row.
	participants *workflowUserService
	directory    repocontracts.WorkflowUserRepository
	locker       servicecontracts.DistributedLocker
}

// NewParticipantSyncService returns the service that keeps a project's
// participants in step with the directories it declared.
func NewParticipantSyncService(
	sources repocontracts.ParticipantSourceRepository,
	participants *workflowUserService,
	directory repocontracts.WorkflowUserRepository,
	locker servicecontracts.DistributedLocker,
) servicecontracts.ParticipantSyncService {
	if participants == nil {
		// Refused here rather than dereferenced later: a nil write path does
		// not fail at the call that produced it, it fails inside the first
		// sync, which is a long way from the wiring that caused it.
		panic("NewParticipantSyncService: the participant service is required")
	}
	return &participantSyncService{
		sources:      sources,
		participants: participants,
		directory:    directory,
		locker:       locker,
	}
}

func (s *participantSyncService) ListSources(ctx context.Context, projectID uuid.UUID) ([]entities.ParticipantSource, error) {
	if projectID == uuid.Nil {
		return nil, apierr.Invalidf("a project is required")
	}
	return s.sources.ListByProject(ctx, projectID)
}

// SaveSource creates or updates a source.
func (s *participantSyncService) SaveSource(ctx context.Context, source entities.ParticipantSource) (uuid.UUID, error) {
	if err := validateSource(&source); err != nil {
		return uuid.Nil, err
	}
	return s.sources.Save(ctx, source)
}

func (s *participantSyncService) DeleteSource(ctx context.Context, id uuid.UUID) error {
	return s.sources.Delete(ctx, id)
}

// SyncSource reads one source now and writes what it found.
//
// Under a lock keyed on the source, so two replicas — or a scheduled run and
// somebody pressing the button — do not read the same directory twice and race
// each other's upserts. Failing to take the lock is not an error: it means the
// sync is already happening.
func (s *participantSyncService) SyncSource(ctx context.Context, id uuid.UUID) (entities.ImportSummary, error) {
	source, err := s.sources.GetWithSecrets(ctx, id)
	if err != nil {
		return entities.ImportSummary{}, err
	}

	key := "participant-sync:" + id.String()
	acquired, err := s.locker.TryAcquire(ctx, key, syncLockTTL)
	if err != nil {
		return entities.ImportSummary{}, fmt.Errorf("could not claim the sync: %w", err)
	}
	if !acquired {
		return entities.ImportSummary{}, apierr.Invalidf("this source is already being synced")
	}
	defer func() {
		if err := s.locker.Release(ctx, key); err != nil {
			// The lease expires on its own, so this delays the next run rather
			// than blocking it forever — but a lock that will not release is
			// worth knowing about.
			log.Warn().Err(err).Str("source", source.Name).Msg("Could not release a participant sync lock")
		}
	}()

	summary, syncErr := s.run(ctx, source)

	run := entities.SourceRun{
		At:      time.Now().UTC(),
		OK:      syncErr == nil,
		Created: summary.Created,
		Updated: summary.Updated,
	}
	if syncErr != nil {
		run.Detail = syncErr.Error()
	} else if len(summary.Problems) > 0 {
		run.Detail = fmt.Sprintf("%d row(s) were not imported", len(summary.Problems))
	}
	if err := s.sources.RecordRun(ctx, id, run); err != nil {
		// The sync happened; failing to write down that it happened must not
		// discard its result.
		log.Warn().Err(err).Str("source", source.Name).Msg("Could not record a participant sync")
	}
	return summary, syncErr
}

// run fetches and writes, then applies the source's missing policy.
func (s *participantSyncService) run(ctx context.Context, source entities.ParticipantSource) (entities.ImportSummary, error) {
	fetcher, err := fetcherFor(source)
	if err != nil {
		return entities.ImportSummary{}, err
	}
	parsed, err := fetcher.Fetch(ctx)
	if err != nil {
		return entities.ImportSummary{}, err
	}

	projectID := source.Project.ID
	summary := s.participants.write(ctx, projectID, parsed)

	if source.OnMissing == entities.OnMissingDeactivate {
		deactivated, err := s.deactivateMissing(ctx, projectID, parsed)
		if err != nil {
			return summary, err
		}
		summary.Deactivated = deactivated
	}
	return summary, nil
}

// deactivateMissing stops work reaching people the source no longer names.
//
// Only when the source says to. A feed covering one department is not a
// statement about everybody else, and deactivating on absence by default would
// mean syncing finance took work away from engineering.
//
// Deactivated, never deleted: the record of what somebody did has to survive
// them leaving.
func (s *participantSyncService) deactivateMissing(ctx context.Context, projectID uuid.UUID, parsed userimport.Result) (int, error) {
	named := make(map[string]struct{}, len(parsed.Rows))
	for _, row := range parsed.Rows {
		named[row.Username] = struct{}{}
	}

	existing, err := s.directory.ListByProject(ctx, projectID, 0)
	if err != nil {
		return 0, err
	}

	var deactivated int
	for _, person := range existing {
		if _, stillNamed := named[person.Username]; stillNamed || !person.Active {
			continue
		}
		person.Active = false
		if _, err := s.directory.Upsert(ctx, person); err != nil {
			return deactivated, fmt.Errorf("could not deactivate %s: %w", person.Username, err)
		}
		deactivated++
	}
	return deactivated, nil
}

// fetcherFor builds the reader a source describes.
func fetcherFor(source entities.ParticipantSource) (participantsource.Source, error) {
	switch source.Kind {
	case string(participantsource.KindHTTP):
		return participantsource.NewHTTPSource(participantsource.HTTPConfig{
			URL:     stringOf(source.Config, "url"),
			Method:  stringOf(source.Config, "method"),
			Headers: headersOf(source.Config),
		}), nil
	case string(participantsource.KindPostgres):
		return participantsource.NewPostgresSource(participantsource.PostgresConfig{
			DSN:   stringOf(source.Config, "dsn"),
			Query: stringOf(source.Config, "query"),
		}), nil
	default:
		return nil, apierr.Invalidf("%q is not a directory source", source.Kind)
	}
}

// headersOf reads the headers that authenticate a directory call.
//
// Comma-ok throughout: the configuration is stored as JSON and edited by hand
// often enough that a header whose value is a number rather than a string is a
// realistic mistake, and a bare assertion on it would take the worker down.
func headersOf(config map[string]any) map[string]string {
	raw, ok := config["headers"].(map[string]any)
	if !ok {
		return nil
	}
	headers := make(map[string]string, len(raw))
	for name := range raw {
		if value := stringOf(raw, name); value != "" {
			headers[name] = value
		}
	}
	return headers
}

// validateSource refuses a source that could never be read.
func validateSource(source *entities.ParticipantSource) error {
	if source.Name == "" {
		return apierr.Invalidf("a source needs a name")
	}
	switch source.Kind {
	case string(participantsource.KindHTTP), string(participantsource.KindPostgres):
	default:
		return apierr.Invalidf("%q is not a directory source; use http or postgres", source.Kind)
	}
	switch source.OnMissing {
	case "":
		// Defaulted rather than refused: leaving people alone is the safe
		// answer, and making somebody choose before they can save would push
		// them towards whichever is on the left.
		source.OnMissing = entities.OnMissingLeave
	case entities.OnMissingLeave, entities.OnMissingDeactivate:
	default:
		return apierr.Invalidf("%q is not a rule for people the source stops naming", source.OnMissing)
	}
	if source.Schedule != "" {
		if _, err := entities.ParseTimerSchedule(source.Schedule, time.Now()); err != nil {
			return apierr.Invalidf("%q is not a schedule: %v", source.Schedule, err)
		}
	}
	return nil
}

// syncTick is how often the worker looks for sources whose time has come.
//
// A minute, not a second: the shortest schedule anyone sets on a directory is
// minutes, and a tighter tick would be a query per second forever to discover
// nothing has changed.
const syncTick = time.Minute

// StartScheduledSyncs runs due sources until the context is cancelled.
//
// One goroutine for the whole installation, not one per source: sources are
// counted in tens and each run is bounded, so a single sweep is simpler than a
// timer per source that has to be cancelled when one is edited.
//
// Every run takes the same per-source lock a manual sync does, so several
// replicas ticking at once do not read the same directory several times.
func (s *participantSyncService) StartScheduledSyncs(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(syncTick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDue(ctx)
			}
		}
	}()
}

// runDue syncs every source whose time has come.
func (s *participantSyncService) runDue(ctx context.Context) {
	sources, err := s.sources.DueForSync(ctx)
	if err != nil {
		// The ticker cannot return anything, so a failed sweep is logged here.
		// Dropping it would leave a worker that had stopped syncing looking
		// exactly like one with nothing to do.
		log.Error().Err(err).Msg("Could not list participant sources due for sync")
		return
	}
	for _, source := range syncdue.Ready(sources, time.Now().UTC()) {
		summary, err := s.SyncSource(ctx, source.ID)
		if err != nil {
			// Recorded against the source by SyncSource, so this is the
			// operator's copy rather than the only one.
			log.Warn().Err(err).Str("source", source.Name).Msg("A scheduled participant sync failed")
			continue
		}
		log.Info().
			Str("source", source.Name).
			Int("created", summary.Created).
			Int("updated", summary.Updated).
			Int("deactivated", summary.Deactivated).
			Int("problems", len(summary.Problems)).
			Msg("Participant sync")
	}
}
