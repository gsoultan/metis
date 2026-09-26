package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// environmentWatchEvery is how soon every replica catches up with a change to
// the environments: one created, re-enabled or pointed at another database is
// served, and one deleted or disabled stops being served. A var so a test need
// not wait for it.
var environmentWatchEvery = 15 * time.Second

// environmentCloseAfter is how long a stopped environment's connections stay
// open: the listener's shutdown time, so a request that was in flight when the
// environment went can finish.
var environmentCloseAfter = httpShutdownTimeout

// environmentRuntimes is what this replica runs for each environment: the
// listener on its port and its background workers, under a context of their
// own, so an environment can be started and stopped without a restart.
//
// Deleting an environment used to remove its row and nothing else, and
// creating one did nothing until the next restart: what ran was decided once,
// at boot, and nothing looked again.
type environmentRuntimes struct {
	mu      sync.Mutex
	running map[uuid.UUID]*environmentRuntime
	// settling are the environments being started or closed. The watcher
	// leaves them alone until that has finished, so an environment is never
	// opened while the connections it would replace are still closing.
	settling map[uuid.UUID]bool
	// failures is why each environment last failed to start, so that a
	// failure is logged once for each cause rather than at every check.
	failures map[uuid.UUID]string
	// work is the watcher and the starts it has under way, so a test can wait
	// for them to finish.
	work sync.WaitGroup
}

// environmentRuntime is one environment as this replica serves it.
type environmentRuntime struct {
	name string
	// settings is what it was started with, to tell when its row has changed
	// underneath it.
	settings environmentSettings
	// stop cancels its workers and shuts its listener down.
	stop context.CancelFunc
}

// serving returns the settings an environment was started with, if this
// replica runs it.
func (r *environmentRuntimes) serving(id uuid.UUID) (environmentSettings, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runtime, ok := r.running[id]
	if !ok {
		return environmentSettings{}, false
	}
	return runtime.settings, true
}

// runningIDs returns the environments this replica runs.
func (r *environmentRuntimes) runningIDs() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uuid.UUID, 0, len(r.running))
	for id := range r.running {
		ids = append(ids, id)
	}
	return ids
}

// rename records a running environment's new name, which is only what it is
// called in the log: nothing about serving it changes.
func (r *environmentRuntimes) rename(id uuid.UUID, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if runtime, ok := r.running[id]; ok {
		runtime.name = name
	}
}

// claim marks an environment as starting, and reports false when it is
// running, starting or closing already.
func (r *environmentRuntimes) claim(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settling[id] || r.running[id] != nil {
		return false
	}
	if r.settling == nil {
		r.settling = map[uuid.UUID]bool{}
	}
	r.settling[id] = true
	return true
}

// started records a claimed environment as running.
func (r *environmentRuntimes) started(id uuid.UUID, runtime *environmentRuntime) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = map[uuid.UUID]*environmentRuntime{}
	}
	r.running[id] = runtime
	delete(r.settling, id)
}

// take removes a running environment so it can be stopped, and marks it as
// closing until settled is called.
func (r *environmentRuntimes) take(id uuid.UUID) (*environmentRuntime, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runtime, ok := r.running[id]
	if !ok {
		return nil, false
	}
	delete(r.running, id)
	if r.settling == nil {
		r.settling = map[uuid.UUID]bool{}
	}
	r.settling[id] = true
	return runtime, true
}

// settled marks an environment as neither starting nor closing: a start that
// failed, or a close that has finished.
func (r *environmentRuntimes) settled(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.settling, id)
}

// failed records why an environment could not start, and reports whether the
// cause is new — the only time it is worth an error in the log.
func (r *environmentRuntimes) failed(id uuid.UUID, cause string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures[id] == cause {
		return false
	}
	if r.failures == nil {
		r.failures = map[uuid.UUID]string{}
	}
	r.failures[id] = cause
	return true
}

// clearFailure forgets an environment's failure once it has started, so the
// next one is reported whatever its cause.
func (r *environmentRuntimes) clearFailure(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, id)
}

// forgetFailuresExcept drops the failures of environments no longer enabled,
// so the map holds no more than the registry does, and an environment enabled
// again later has its failure reported afresh.
func (r *environmentRuntimes) forgetFailuresExcept(enabled map[uuid.UUID]models.EnvironmentModel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.failures {
		if _, ok := enabled[id]; !ok {
			delete(r.failures, id)
		}
	}
}
