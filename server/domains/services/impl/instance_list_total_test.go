package impl

import (
	"testing"

	"github.com/google/uuid"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The page's total is read off the status counts only where they count the
// same instances the page is drawn from. Anywhere else, reading it off them
// would say "12 of 400" about a list of twelve.
func TestThePageTotalIsReadOffTheCountsOnlyWhenTheyCountTheSameInstances(t *testing.T) {
	definition := uuid.New()
	counts := map[models.ProcessStatus]int64{models.ProcessActive: 3, models.ProcessCompleted: 5}
	countAll := repocontracts.InstanceFilter{DefinitionID: definition}

	for _, c := range []struct {
		name          string
		filter, count repocontracts.InstanceFilter
		total         int64
		read          bool
	}{
		{"every state", countAll, countAll, 8, true},
		{"one state", repocontracts.InstanceFilter{DefinitionID: definition, Status: models.ProcessActive}, countAll, 3, true},
		{"a state nothing is in", repocontracts.InstanceFilter{DefinitionID: definition, Status: models.ProcessFailed}, countAll, 0, true},
		{"needs attention", repocontracts.InstanceFilter{DefinitionID: definition, NeedsAttention: true}, countAll, 0, false},
		{"a business key", repocontracts.InstanceFilter{DefinitionID: definition, BusinessKey: "order-7"}, repocontracts.InstanceFilter{DefinitionID: definition, BusinessKey: "order-7"}, 0, false},
		{"counts of another definition", repocontracts.InstanceFilter{}, countAll, 0, false},
		{"counts narrowed to a state", countAll, repocontracts.InstanceFilter{DefinitionID: definition, Status: models.ProcessActive}, 0, false},
	} {
		total, read := totalFromCounts(c.filter, c.count, counts)
		if read != c.read || total != c.total {
			t.Errorf("%s: total %d, read %v; want %d, %v", c.name, total, read, c.total, c.read)
		}
	}
}
