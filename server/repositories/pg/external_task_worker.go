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
	"github.com/gsoultan/metis/server/repositories/store/externaltaskworker"
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

// seenWorker writes a worker's row, or brings it up to date when the one
// there is older than workerSeenEvery or says another version.
const seenWorker = `INSERT INTO external_task_workers (organization_id, topic, worker_id, version, last_seen_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organization_id, topic, worker_id) DO UPDATE
SET last_seen_at = EXCLUDED.last_seen_at, version = EXCLUDED.version, updated_at = EXCLUDED.last_seen_at
WHERE external_task_workers.last_seen_at < $6
   OR external_task_workers.version IS DISTINCT FROM EXCLUDED.version`

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
	var said any
	if version != "" {
		said = version
	}
	at = at.UTC()
	if _, err := ex.Exec(ctx, seenWorker, []any{scope.organization, topic, workerID, said, at, at.Add(-workerSeenEvery)}); err != nil {
		return fmt.Errorf("could not record the worker: %w", err)
	}
	return nil
}

// openTasksByWorker counts the open tasks of a set of projects by topic and by
// the worker holding them: those on offer under no worker, those locked under
// the worker that holds them. A task waiting out a retry, or with no tries
// left, is neither. Only open tasks are in the table, since completing one
// deletes it, so the statement reads little.
const openTasksByWorker = `SELECT topic, CASE WHEN lock_expiration > $1 THEN worker_id END AS holder,
       count(*) FILTER (WHERE (lock_expiration IS NULL OR lock_expiration <= $1) AND retries > 0) AS waiting,
       count(*) FILTER (WHERE lock_expiration > $1 AND worker_id IS NOT NULL) AS locked
FROM external_tasks
WHERE deleted_at IS NULL AND project_id = ANY($2)
GROUP BY 1, 2
ORDER BY 1, 2
LIMIT $3`

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
	held := map[[2]string]int{}
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
			held[[2]string{topic.Topic, topic.Workers[i].WorkerID}] = i
		}
	}

	if len(scope.projects) > 0 {
		rows, err := ex.Query(ctx, openTasksByWorker, []any{now, uuidsToRaw(scope.projects), int64(workersListed)})
		if err != nil {
			return nil, fmt.Errorf("could not count the open external tasks: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			values := rows.RawValues()
			if len(values) < 4 {
				return nil, fmt.Errorf("could not count the open external tasks: %d columns", len(values))
			}
			var waiting, locked int64
			if err := scanInt(values[2:3], &waiting); err != nil {
				return nil, err
			}
			if err := scanInt(values[3:4], &locked); err != nil {
				return nil, err
			}
			topic := topicOf(string(values[0]))
			topic.Waiting += waiting
			topic.Locked += locked
			if values[1] == nil || locked == 0 {
				continue
			}
			holder := string(values[1])
			if i, ok := held[[2]string{topic.Topic, holder}]; ok {
				topic.Workers[i].Locked += locked
				continue
			}
			// A lock taken before this worker was first recorded, or by a
			// worker since forgotten: it is serving the topic now all the same.
			held[[2]string{topic.Topic, holder}] = len(topic.Workers)
			topic.Workers = append(topic.Workers, entities.ExternalTaskWorker{WorkerID: holder, Locked: locked})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("could not count the open external tasks: %w", err)
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

// unservedTopics counts the topics, by organization, with tasks on offer and
// no worker heard from since $2. Projects are in every database: an
// environment's has its own project's row, so the join holds there too.
const unservedTopics = `SELECT count(*) FROM (
  SELECT DISTINCT p.organization_id, t.topic
  FROM external_tasks AS t JOIN projects AS p ON p.id = t.project_id
  WHERE t.deleted_at IS NULL AND t.retries > 0 AND (t.lock_expiration IS NULL OR t.lock_expiration <= $1)
) AS waiting
WHERE NOT EXISTS (
  SELECT 1 FROM external_task_workers AS w
  WHERE w.organization_id = waiting.organization_id AND w.topic = waiting.topic AND w.last_seen_at > $2)`

func (r *externalTaskWorkerRepository) CountUnserved(ctx context.Context, now time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: the topics nobody serves span every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	now = now.UTC()
	rows, err := ex.Query(ctx, unservedTopics, []any{now, now.Add(-entities.WorkerLiveWindow)})
	if err != nil {
		return 0, fmt.Errorf("could not count the topics nobody serves: %w", err)
	}
	defer rows.Close()
	var unserved int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &unserved); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not count the topics nobody serves: %w", err)
	}
	return unserved, nil
}

func (r *externalTaskWorkerRepository) ForgetIdle(ctx context.Context, before time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: idle workers span every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	removed, err := db.DeleteInBatches(ctx, ex,
		`DELETE FROM external_task_workers WHERE id = ANY(ARRAY(
		     SELECT id FROM external_task_workers WHERE last_seen_at < $1 LIMIT $2))`, before.UTC())
	if err != nil {
		return removed, fmt.Errorf("could not forget idle workers: %w", err)
	}
	return removed, nil
}
