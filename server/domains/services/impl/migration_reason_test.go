package impl

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A plan lists ten of its reasons and counts the rest; ten or fewer are
// listed as they are.
func TestListedReasons(t *testing.T) {
	t.Parallel()
	reasons := func(n int) []string {
		var all []string
		for i := range n {
			all = append(all, fmt.Sprintf("reason %02d", i+1))
		}
		return all
	}
	for _, n := range []int{0, 1, 10} {
		if got := listedReasons(reasons(n)); !slices.Equal(got, reasons(n)) {
			t.Errorf("%d reason(s) are listed as %v", n, got)
		}
	}
	all := reasons(11)
	got := listedReasons(all)
	if len(got) != 11 || !slices.Equal(got[:10], all[:10]) || got[10] != "and 1 more reason(s) of the same kinds, not listed here" {
		t.Errorf("eleven reasons are listed as %v", got)
	}
	if len(all) != 11 || all[10] != "reason 11" {
		t.Errorf("listing the reasons rewrote the list it was given: %v", all)
	}
	if got := listedReasons(reasons(500)); len(got) != 11 || got[10] != "and 490 more reason(s) of the same kinds, not listed here" {
		t.Errorf("five hundred reasons are listed as %d, the last %q", len(got), got[len(got)-1])
	}
}

// What a request says of why: the typed reasons, the controls whose loss was
// acknowledged, the steps redirected — each part only when there is one, and
// each listing ten and counting the rest.
func TestMigrationReason(t *testing.T) {
	t.Parallel()
	skip := func(reason string) servicecontracts.NodeAction {
		return servicecontracts.NodeAction{Kind: servicecontracts.NodeActionSkip, Reason: reason}
	}
	for name, c := range map[string]struct {
		options    servicecontracts.MigrationOptions
		redirected []string
		want       string
	}{
		"nothing typed, acknowledged or redirected": {want: ""},
		"a typed reason alone": {
			options: servicecontracts.MigrationOptions{Actions: map[string]servicecontracts.NodeAction{"ops": skip("  moot  ")}},
			want:    "moot",
		},
		"typed reasons in the order of their steps, an empty one left out": {
			options: servicecontracts.MigrationOptions{Actions: map[string]servicecontracts.NodeAction{
				"b": skip("second"), "a": skip("first"), "c": skip(" "),
			}},
			want: "first; second",
		},
		"all three parts": {
			options: servicecontracts.MigrationOptions{
				Actions:      map[string]servicecontracts.NodeAction{"ops": skip("moot")},
				Acknowledged: []string{"wait", "audit", "wait"},
			},
			redirected: []string{"“Prepare” to “Sign”"},
			want:       "moot; acknowledged the loss of audit, wait; redirected “Prepare” to “Sign”",
		},
		"a redirect alone": {redirected: []string{"“Prepare” to “Sign”"}, want: "redirected “Prepare” to “Sign”"},
	} {
		if got := migrationReason(c.options, c.redirected); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}

	// Twelve of each: ten said, two counted, and an id as long as its author
	// made it cut as a name is.
	many := servicecontracts.MigrationOptions{Actions: map[string]servicecontracts.NodeAction{}}
	var redirected []string
	for n := 1; n <= 12; n++ {
		many.Actions[fmt.Sprintf("s%02d", n)] = skip(fmt.Sprintf("why %02d", n))
		many.Acknowledged = append(many.Acknowledged, fmt.Sprintf("c%02d", n)+strings.Repeat("x", 1000))
		redirected = append(redirected, fmt.Sprintf("“r%02d” to “sign”", n))
	}
	got := migrationReason(many, redirected)
	for _, want := range []string{
		"why 10; and 2 more decision(s); acknowledged the loss of c01",
		", and 2 more control(s); redirected “r01” to “sign”",
		"“r10” to “sign”, and 2 more step(s)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("twelve of each: the reason does not say %q:\n%s", want, got)
		}
	}
	for _, unsaid := range []string{"why 11", "c11", "“r11”", strings.Repeat("x", deviationNodeNameLength)} {
		if strings.Contains(got, unsaid) {
			t.Errorf("twelve of each: the reason says %.20q, which is beyond what it lists", unsaid)
		}
	}
}

// The steps a mapping redirects, by name, leaving out a step renamed where it
// stands and a step mapped to itself.
func TestRedirectedSteps(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"prepare": {ID: "prepare", Name: "Prepare"}, "sign": {ID: "sign", Name: "Sign"}, "check": {ID: "check"},
	}
	target := map[string]models.FlowNode{
		"draft": {ID: "draft", Name: "Draft"}, "sign": source["sign"], "check": source["check"], "file": {ID: "file", Name: strings.Repeat("é", 4000)},
	}
	mapping := map[string]string{"prepare": "draft", "sign": "sign", "check": "file"}
	got := redirectedSteps(source, target, mapping, map[string]string{"prepare": "draft"})
	if want := []string{"“check” to “" + strings.Repeat("é", deviationNodeNameLength) + "”"}; !slices.Equal(got, want) {
		t.Errorf("with the rename in place: %v", got)
	}
	// The same mapping when the new id does not stand where the old one did.
	got = redirectedSteps(source, target, mapping, nil)
	if len(got) != 2 || got[1] != "“Prepare” to “Draft”" {
		t.Errorf("with nothing renamed in place: %v", got)
	}
}
