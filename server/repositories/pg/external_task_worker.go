package pg

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
	"github.com/gsoultan/metis/server/repositories/store/externaltaskworker"
	"github.com/gsoultan/metis/server/repositories/store/project"
)

type externalTaskWorkerRepository struct{ conn }

// NewExternalTaskWorkerRepository returns the store of the workers that fetch
// external tasks.
func NewExternalTaskWorkerRepository(c *db.Conn) contracts.ExternalTaskWorkerRepository {
	return &externalTaskWorkerRepository{conn{conn: c}}
}

// workerSeenEvery is how stale a worker's row may get before a fetch writes
// it again. A worker polling every second would otherwise write a row a
// second, for a reading nobody needs finer than WorkerLiveWindow.
const workerSeenEvery = 10 * time.Second

// workersListed bounds the workers and the topics one organization's list
// reads. An organization with more is answered from its most recently seen.
const workersListed = 1_000

// Seen writes a worker's row, or brings it up to date when the one there is
// older than workerSeenEvery or says another version.
func (r *externalTaskWorkerRepository) Seen(ctx context.Context, topic, workerID, version string, at time.Time) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.organization == uuid.Nil {
		return nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	at = at.UTC()
	n := externaltaskworker.Create()
	n.SetOrganizationID(scope.organization)
	n.SetTopic(topic)
	n.SetWorkerID(workerID)
	n.SetLastSeenAt(at)
	n.SetUpdatedAt(at)
	// The row says another version than the worker: a different one, none
	// where the worker gives one, or one where it gives none.
	changed := []externaltaskworker.Pred{externaltaskworker.Version.IsNotNull()}
	if version != "" {
		n.SetVersion(version)
		changed = []externaltaskworker.Pred{externaltaskworker.Version.IsNull(), externaltaskworker.Version.NotEq(version)}
	} else {
		n.SetVersionNull()
	}
	n.OnConflictOrganizationIDTopicWorkerID()
	stale := externaltaskworker.New().Any(append(changed, externaltaskworker.LastSeenAt.Lt(at.Add(-workerSeenEvery)))...)
	if _, err := n.UpsertWhere(ctx, ex, stale); err != nil {
		return fmt.Errorf("could not record the worker: %w", err)
	}
	return nil
}

func (r *externalTaskWorkerRepository) Topics(ctx context.Context, now time.Time) ([]entities.ExternalTaskTopic, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if scope.organization == uuid.Nil {
		return nil, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	topics := map[string]*entities.ExternalTaskTopic{}
	topicOf := func(name string) *entities.ExternalTaskTopic {
		topic, ok := topics[name]
		if !ok {
			topic = &entities.ExternalTaskTopic{Topic: name, Workers: []entities.ExternalTaskWorker{}}
			topics[name] = topic
		}
		return topic
	}

	workers, err := externaltaskworker.New().
		Where(externaltaskworker.OrganizationID.Eq(scope.organization)).
		Order(externaltaskworker.LastSeenAt.Desc()).
		Limit(workersListed).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the workers: %w", err)
	}
	// By index, not by pointer: a holder appended below can move a topic's
	// workers to a new array.
	heldBy := map[[2]string]int{}
	for _, row := range workers {
		seen := row.LastSeenAt
		topic := topicOf(row.Topic)
		topic.Workers = append(topic.Workers, entities.ExternalTaskWorker{
			WorkerID:   row.WorkerID,
			Version:    row.Version.V,
			LastSeenAt: &seen,
			Live:       now.Sub(seen) <= entities.WorkerLiveWindow,
		})
	}
	for _, topic := range topics {
		for i := range topic.Workers {
			heldBy[[2]string{topic.Topic, topic.Workers[i].WorkerID}] = i
		}
	}

	// The open tasks of the scope's projects: those on offer under no worker,
	// and those locked under the worker that holds them. A task waiting out a
	// retry, or with no tries left, is neither. Only open tasks are in the
	// table, since completing one deletes it, so both reads are small.
	if len(scope.projects) > 0 {
		projects := externaltask.ProjectID.In(uuidsToRaw(scope.projects)...)
		waiting, err := externaltask.New().
			Where(projects, externaltask.Retries.Gt(0)).
			Any(externaltask.LockExpiration.IsNull(), externaltask.LockExpiration.Lte(now)).
			Limit(workersListed).
			AllWaitingByTopic(ctx, ex)
		if err != nil {
			return nil, fmt.Errorf("could not count the open external tasks: %w", err)
		}
		for _, row := range waiting {
			topicOf(row.Topic).Waiting += row.Total
		}
		held, err := externaltask.New().
			Where(projects, externaltask.LockExpiration.Gt(now), externaltask.WorkerID.IsNotNull()).
			Limit(workersListed).
			AllHeldByWorker(ctx, ex)
		if err != nil {
			return nil, fmt.Errorf("could not count the open external tasks: %w", err)
		}
		for _, row := range held {
			topic := topicOf(row.Topic)
			topic.Locked += row.Total
			holder := row.WorkerID.V
			if i, ok := heldBy[[2]string{topic.Topic, holder}]; ok {
				topic.Workers[i].Locked += row.Total
				continue
			}
			// A lock taken before this worker was first recorded, or by a
			// worker since forgotten: it is serving the topic now all the same.
			heldBy[[2]string{topic.Topic, holder}] = len(topic.Workers)
			topic.Workers = append(topic.Workers, entities.ExternalTaskWorker{WorkerID: holder, Locked: row.Total})
		}
	}

	listed := make([]entities.ExternalTaskTopic, 0, len(topics))
	for _, topic := range topics {
		live := false
		for _, worker := range topic.Workers {
			live = live || worker.Live
		}
		topic.Unserved = topic.Waiting > 0 && !live
		listed = append(listed, *topic)
	}
	sort.Slice(listed, func(i, j int) bool { return listed[i].Topic < listed[j].Topic })
	return listed, nil
}

// CountUnserved counts the topics, by organization, with tasks on offer and
// no worker heard from inside the live window. Three paged reads: the topics
// on offer by project, the organization of each of those projects, and the
// topics a live worker serves. Projects are in every database: an
// environment's has its own project's row, so the lookup holds there too.
func (r *externalTaskWorkerRepository) CountUnserved(ctx context.Context, now time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: the topics nobody serves span every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	now = now.UTC()
	waiting, err := everyGroup(func(offset int64) ([]externaltask.WaitingByProjectRow, error) {
		return externaltask.New().
			Where(externaltask.Retries.Gt(0)).
			Any(externaltask.LockExpiration.IsNull(), externaltask.LockExpiration.Lte(now)).
			Limit(everyRowBatch).Offset(offset).
			AllWaitingByProject(ctx, ex)
	})
	if err != nil {
		return 0, fmt.Errorf("could not count the topics nobody serves: %w", err)
	}
	if len(waiting) == 0 {
		return 0, nil
	}
	projectIDs := make([][16]byte, 0, len(waiting))
	seen := make(map[[16]byte]bool, len(waiting))
	for _, row := range waiting {
		if !seen[row.ProjectID] {
			seen[row.ProjectID] = true
			projectIDs = append(projectIDs, row.ProjectID)
		}
	}
	projects, err := everyRow[project.Row](ctx, ex, project.New().WithDeleted().Where(project.ID.In(projectIDs...)))
	if err != nil {
		return 0, fmt.Errorf("could not count the topics nobody serves: %w", err)
	}
	organizationOf := make(map[[16]byte][16]byte, len(projects))
	for _, p := range projects {
		organizationOf[p.ID] = p.OrganizationID
	}
	served, err := everyGroup(func(offset int64) ([]externaltaskworker.ServedRow, error) {
		return externaltaskworker.New().
			Where(externaltaskworker.LastSeenAt.Gt(now.Add(-entities.WorkerLiveWindow))).
			Limit(everyRowBatch).Offset(offset).
			AllServed(ctx, ex)
	})
	if err != nil {
		return 0, fmt.Errorf("could not count the topics nobody serves: %w", err)
	}
	type orgTopic struct {
		organization [16]byte
		topic        string
	}
	live := make(map[orgTopic]bool, len(served))
	for _, row := range served {
		live[orgTopic{row.OrganizationID, row.Topic}] = true
	}
	unserved := make(map[orgTopic]bool)
	for _, row := range waiting {
		organization, ok := organizationOf[row.ProjectID]
		if !ok {
			continue
		}
		key := orgTopic{organization, row.Topic}
		if !live[key] {
			unserved[key] = true
		}
	}
	return int64(len(unserved)), nil
}

func (r *externalTaskWorkerRepository) ForgetIdle(ctx context.Context, before time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: idle workers span every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	idle := externaltaskworker.New().Where(externaltaskworker.LastSeenAt.Lt(before.UTC()))
	removed, err := db.PruneInBatches(ctx, func(limit int64) (int64, error) {
		return idle.Limit(limit).DeleteBatch(ctx, ex)
	})
	if err != nil {
		return removed, fmt.Errorf("could not forget idle workers: %w", err)
	}
	return removed, nil
}
