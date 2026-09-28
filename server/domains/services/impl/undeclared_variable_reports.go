package impl

import (
	"sync"

	"github.com/gsoultan/metis/internal/pkg/lru"
)

// undeclaredReportCapacity bounds how many steps undeclaredVariableReports
// remembers. A thousand steps completed with variables their forms do not
// declare is past any installation's list of forms to fix, and small enough to
// be free.
const undeclaredReportCapacity = 1000

// undeclaredVariableReports remembers which steps have been named in the log
// for a completion that set variables their form does not declare, so each is
// named once rather than on every completion.
//
// Its keys come from deployed definitions, which are user-authored, so it is
// bounded and forgets the step seen least recently; a forgotten step is named
// again the next time, which costs a log line. The zero value is ready to use
// and holds nothing until the first report: the setting that allows such a
// completion is off almost everywhere.
type undeclaredVariableReports struct {
	mu   sync.Mutex
	seen *lru.Cache[string, struct{}]
}

// first reports whether step has not been named since it was last forgotten,
// and remembers it. Asking and remembering are one step, so two completions of
// the same step at the same moment name it once.
func (r *undeclaredVariableReports) first(step string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = lru.New[string, struct{}](undeclaredReportCapacity)
	}
	if _, seen := r.seen.Get(step); seen {
		return false
	}
	r.seen.Put(step, struct{}{})
	return true
}
