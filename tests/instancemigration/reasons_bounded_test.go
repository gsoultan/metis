package instancemigration

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A mapping is as long as whoever wrote it made it, and each redirect in it
// is a reason. What a plan lists of them, what the refusal says, what the
// request stores and what the queue shows as its reason are one list: ten,
// and a count of the rest, with a step's name as long as a ledger row keeps
// it.
func TestTheReasonsAPlanListsAreBoundedWhereverTheyAreSaid(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("é", 400)
	steps := []step{}
	mapping := map[string]string{}
	for n := 1; n <= 12; n++ {
		id := fmt.Sprintf("p%02d", n)
		steps = append(steps, step{id: id, name: fmt.Sprintf("P%02d %s", n, long)})
		mapping[id] = "sign"
	}
	steps = append(steps, controlStep, signStep)
	v1, v2 := f.startedOn(t, lined(f.project, steps...), lined(f.project, steps...))
	f.assertWaitingAt(t, v1, "p01")
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, actor)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover {
		t.Fatalf("the plan: applicable=%v requires=%v refusals=%v (err %v)", plan.Applicable(), plan.RequiresSecondApprover, plan.Refusals, err)
	}
	const rest = "and 2 more reason(s) of the same kinds, not listed here"
	if len(plan.SecondApproverReasons) != 11 || plan.SecondApproverReasons[10] != rest {
		t.Fatalf("twelve redirects are listed as %d reason(s), the last %q; want ten and %q",
			len(plan.SecondApproverReasons), plan.SecondApproverReasons[len(plan.SecondApproverReasons)-1], rest)
	}
	for n, reason := range plan.SecondApproverReasons[:10] {
		want := fmt.Sprintf("“P%02d ", n+1)
		if !strings.HasPrefix(reason, want) || strings.Contains(reason, long) || utf8.RuneCountInString(reason) > 700 {
			t.Fatalf("reason %d starts %q and is %d characters long; want it to start %s and to cut the step's name",
				n+1, firstRunes(reason, 12), utf8.RuneCountInString(reason), want)
		}
	}
	said := strings.Join(plan.SecondApproverReasons, "; ")

	// The refusal says the same list.
	_, err = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, mapping, actor)
	if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "("+said+")") {
		t.Fatalf("the refusal on one administrator's call does not say the plan's list: %v", err)
	}
	f.assertWaitingAt(t, v1, "p01")

	// So does the request, in what it stores and in what the queue shows.
	if result, err := f.applyWithApproval(t, v1, v2, mapping, actor); err != nil || result.Changed != 1 {
		t.Fatalf("the approved migration: %+v %v", result, err)
	}
	f.assertWaitingAt(t, v2, "sign")
	asked, err := f.svc.GetDeviationRequest(adminAs(f.ctx, "dita"), f.underApproval)
	if err != nil {
		t.Fatalf("read the request: %v", err)
	}
	if stored := strings.Join(asked.Because(), "; "); stored != said {
		t.Fatalf("the request stores\n  %s\nwant the plan's list\n  %s", stored, said)
	}
	if !strings.HasPrefix(asked.Reason, "redirected “P01 ") || !strings.HasSuffix(asked.Reason, "” to “Sign”, and 2 more step(s)") ||
		strings.Count(asked.Reason, " to “Sign”") != 10 || strings.Contains(asked.Reason, long) {
		t.Fatalf("the request's reason names %d redirect(s) in %d characters and ends %q; want ten of twelve, the rest counted, names cut",
			strings.Count(asked.Reason, " to “Sign”"), utf8.RuneCountInString(asked.Reason), lastRunes(asked.Reason, 40))
	}
}

// firstRunes and lastRunes are the ends of a text, for a failure to show.
func firstRunes(text string, n int) string {
	runes := []rune(text)
	return string(runes[:min(n, len(runes))])
}

func lastRunes(text string, n int) string {
	runes := []rune(text)
	return string(runes[max(0, len(runes)-n):])
}
