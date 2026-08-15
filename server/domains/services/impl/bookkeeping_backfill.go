package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/rs/zerolog/log"
)

// legacyMultiInstancePrefix is the prefix the engine used to write its
// multi-instance bookkeeping under, inside the business variable namespace.
const legacyMultiInstancePrefix = "_mi_"

// legacyJoinPrefix is the prefix the engine used for gateway join counters, in
// the same namespace and for the same reason.
const legacyJoinPrefix = "_join_"

// EngineBookkeepingBackfillResult reports what one backfill run did.
type EngineBookkeepingBackfillResult struct {
	Scanned   int
	Migrated  int
	Unchanged int
}

// BackfillEngineBookkeeping moves the engine's own bookkeeping out of the
// variables map and into the instance's own fields.
//
// The engine used to track a node that runs once per item with three variables —
// `_mi_<node>_active`, `_mi_<node>_completed` and `_mi_<node>_total` — and a
// gateway waiting on several branches with `_join_<node>`, all sharing the
// namespace with business data. Those now live in their own columns, so an
// instance already mid-way through either would come back from the database with
// nothing recorded: its iterations would restart from zero, or a gateway would
// forget the branches that had already arrived and wait forever.
//
// This reads the legacy keys once at startup, rewrites them into the new field
// and removes them from the variables.
//
// It is idempotent: a migrated instance has no `_mi_` keys left, so a later run
// does not touch it. Only active instances are considered — a finished process
// has nothing left to count, and the stale keys on it are harmless history.
func BackfillEngineBookkeeping(ctx context.Context, repo repositories.Repository) (EngineBookkeepingBackfillResult, error) {
	ms, err := repo.Process().List(ctx)
	if err != nil {
		return EngineBookkeepingBackfillResult{}, fmt.Errorf("list process instances: %w", err)
	}

	var result EngineBookkeepingBackfillResult
	for i := range ms {
		m := &ms[i]
		if m.Status != "active" {
			continue
		}
		result.Scanned++

		instance := adapters.InstanceEntityAdapter{Model: *m}.ToEntity()
		if !migrateLegacyBookkeepingKeys(&instance) {
			result.Unchanged++
			continue
		}

		if err := repo.Process().Update(ctx, adapters.InstanceModelAdapter{Instance: instance}.ToModel()); err != nil {
			return result, fmt.Errorf("update instance %s: %w", instance.ID, err)
		}
		result.Migrated++
	}

	if result.Migrated > 0 {
		log.Info().
			Int("scanned", result.Scanned).
			Int("migrated", result.Migrated).
			Msg("Moved engine bookkeeping out of the process variables")
	}
	return result, nil
}

// migrateLegacyBookkeepingKeys rewrites one instance in place, reporting
// whether anything changed.
func migrateLegacyBookkeepingKeys(instance *entities.ProcessInstance) bool {
	if len(instance.Variables) == 0 {
		return false
	}

	totals := map[string]int{}
	completed := map[string]int{}
	active := map[string]bool{}
	var legacyKeys []string

	for key, value := range instance.Variables {
		if strings.HasPrefix(key, legacyJoinPrefix) {
			if instance.Joins == nil {
				instance.Joins = map[string]int{}
			}
			instance.Joins[strings.TrimPrefix(key, legacyJoinPrefix)] = toInt(value)
			legacyKeys = append(legacyKeys, key)
			continue
		}
		if !strings.HasPrefix(key, legacyMultiInstancePrefix) {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_total"):
			totals[legacyNodeID(key, "_total")] = toInt(value)
		case strings.HasSuffix(key, "_completed"):
			completed[legacyNodeID(key, "_completed")] = toInt(value)
		case strings.HasSuffix(key, "_active"):
			active[legacyNodeID(key, "_active")] = true
		default:
			// Item bindings from an even older scheme ("_mi_var_<node>_<n>")
			// that nothing reads. Drop them too rather than leave them behind.
		}
		legacyKeys = append(legacyKeys, key)
	}

	if len(legacyKeys) == 0 {
		return false
	}

	for nodeID := range active {
		if instance.MultiInstance == nil {
			instance.MultiInstance = map[string]entities.MultiInstanceState{}
		}
		instance.MultiInstance[nodeID] = entities.MultiInstanceState{
			Total:     totals[nodeID],
			Completed: completed[nodeID],
		}
	}
	for _, key := range legacyKeys {
		delete(instance.Variables, key)
	}
	return true
}

// legacyNodeID recovers the node ID from "_mi_<node><suffix>".
func legacyNodeID(key, suffix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(key, legacyMultiInstancePrefix), suffix)
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
