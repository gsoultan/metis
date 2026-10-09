package impl

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// What an approver is told of a run that did not finish keeps the class of a
// refusal made before the run began — the plan's, the gate's — and of nothing
// else: a run that stopped part-way is the server's failure whatever class
// the thing that stopped it carries, and so is any other failure before it.
func TestOnlyARefusalBeforeTheRunKeepsItsClassWhenAnApprovedRunDoesNotFinish(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	gate := refusedAtTheGate("a newcomer", "request %s does not cover this migration", id)
	for name, c := range map[string]struct {
		runErr error
		class  error
	}{
		"the plan's refusal":                    {apierr.Invalidf("version 2 has nowhere to put the work parked on control"), apierr.ErrInvalidArgument},
		"the gate's refusal":                    {gate, apierr.ErrForbidden},
		"a version gone before the run":         {fmt.Errorf("source definition: %w", apierr.ErrNotFound), nil},
		"a failure before the run, of no class": {errors.New("the database is away"), nil},
		"part-way, on something not found":      {failedInTheRun{err: fmt.Errorf("instance x: %w", apierr.ErrNotFound)}, nil},
		"part-way, on an invalid argument":      {failedInTheRun{err: apierr.Invalidf("no outgoing flow matches")}, nil},
		"part-way, on something forbidden":      {failedInTheRun{err: apierr.Forbiddenf("the engine may not")}, nil},
		"part-way, of no class":                 {failedInTheRun{err: errors.New("the ledger lost its connection; run the same migration again to carry on from here")}, nil},
	} {
		err := approvedRunFailed(id, entities.DeviationRequestInterrupted, c.runErr)
		for _, class := range answeredClasses {
			kept := c.class != nil && errors.Is(c.class, class)
			if errors.Is(err, class) != kept {
				t.Errorf("%s: answered as %v = %v, want its class to be %v: %v", name, class, errors.Is(err, class), c.class, err)
			}
		}
		said, front := err.Error(), ""
		if c.class != nil {
			front = c.class.Error() + ": "
		}
		if !strings.HasPrefix(said, front+"the approved migration did not finish: ") ||
			!strings.HasSuffix(said, ". Request "+id.String()+" now reads interrupted; what its run had done stands, and what remains has to be asked for again") {
			t.Errorf("%s: reads %q", name, said)
		}
		for _, unsaid := range []string{"run the same migration again", "invalid argument: the approved migration did not finish: invalid argument"} {
			if strings.Contains(said, unsaid) {
				t.Errorf("%s: says %q: %s", name, unsaid, said)
			}
		}
		if c.class == nil && strings.Contains(said, "forbidden: ") {
			t.Errorf("%s: a failure with no class left says one: %s", name, said)
		}
	}
	// The gate's refusal is still found for what it is, with its reason.
	var refused gateRefusal
	if err := approvedRunFailed(id, entities.DeviationRequestInterrupted, gate); !errors.As(err, &refused) || refused.why != "a newcomer" {
		t.Errorf("the gate's refusal is not found under what the approver is told: %v", err)
	}
	// And a failure of the run is still the failure it was, to anybody else:
	// the mark says nothing of its own.
	cause := fmt.Errorf("instance x: %w", apierr.ErrNotFound)
	if marked := error(failedInTheRun{err: cause}); marked.Error() != cause.Error() || !errors.Is(marked, apierr.ErrNotFound) {
		t.Errorf("the mark changed the failure: %v", marked)
	}
}

// A run that was refused before it acted is recorded and logged as that; one
// that began and broke, as stopping part-way; and the two are told apart the
// way the answer to the approver tells them apart. The plan's own words are
// not stored with the request — they name steps — and the gate's are.
func TestARunRefusedBeforeItActedIsRecordedAsRefusedAndNotAsStoppedPartWay(t *testing.T) {
	t.Parallel()
	const refusedByPlan = "the run was refused before it moved anything: the plan made when it came to start refused the migration"
	partWay := func(acted int) string {
		return fmt.Sprintf("the run stopped on a failure after it had acted on %d instance(s); what it had done by then stands", acted)
	}
	gate := refusedAtTheGate("a newcomer", "request %s does not cover this migration", uuid.Must(uuid.NewV7()))
	for name, c := range map[string]struct {
		runErr  error
		acted   int
		want    string
		refused bool
	}{
		"the plan's refusal":               {apierr.Invalidf("version 2 has nowhere to put the work parked on control"), 0, refusedByPlan, true},
		"the gate's refusal":               {gate, 0, "the run was refused before it moved anything: a newcomer", true},
		"part-way, on an invalid argument": {failedInTheRun{err: apierr.Invalidf("no outgoing flow matches")}, 0, partWay(0), false},
		"part-way, after two instances":    {failedInTheRun{err: errors.New("the ledger lost its connection")}, 2, partWay(2), false},
		"a failure before the run":         {errors.New("the database is away"), 0, partWay(0), false},
		"a version gone before the run":    {fmt.Errorf("source definition: %w", apierr.ErrNotFound), 0, partWay(0), false},
	} {
		outcome := runOutcome(entities.MigrationResult{Changed: c.acted}, c.runErr)
		if outcome[outcomeError] != c.want {
			t.Errorf("%s: the request keeps %q, want %q", name, outcome[outcomeError], c.want)
		}
		if got := refusedBeforeItActed(c.runErr); got != c.refused {
			t.Errorf("%s: told as refused before it acted = %v, want %v", name, got, c.refused)
		}
		if strings.Contains(fmt.Sprint(outcome), "nowhere to put the work") {
			t.Errorf("%s: the request keeps the plan's own words: %v", name, outcome)
		}
	}
}
