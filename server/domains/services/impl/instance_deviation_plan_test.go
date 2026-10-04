package impl

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// tableReading is a decision table with one column for each name, requiring
// the decisions given.
func tableReading(names []string, requires ...string) entities.DecisionDefinition {
	table := entities.DecisionDefinition{RequiredDecisions: requires}
	for _, name := range names {
		table.Inputs = append(table.Inputs, entities.DecisionInput{Expression: name})
	}
	return table
}

// storedDecisions is a place decisions are kept, by key and version, with the
// version in force under 0. It counts what it is asked for.
type storedDecisions struct {
	versions map[string]map[int]entities.DecisionDefinition
	failing  map[string]error
	asked    []string
}

func (s *storedDecisions) load(key string, version int) (entities.DecisionDefinition, bool, error) {
	s.asked = append(s.asked, fmt.Sprintf("%s@%d", key, version))
	if err := s.failing[key]; err != nil {
		return entities.DecisionDefinition{}, false, err
	}
	table, found := s.versions[key][version]
	return table, found, nil
}

// A decision reads what its own table reads and what every decision it
// requires reads, at the version the engine would evaluate each: the one the
// step names for the decision it consults, the one in force for the rest
// (decisionService.evaluateRecursive). What cannot be read — a decision nobody
// stored, a ring of requirements the engine refuses to evaluate, a chain
// longer than a plan will follow — is said to be unread, never to read
// nothing.
func TestDecisionLookupFollowsWhatADecisionRequires(t *testing.T) {
	t.Parallel()
	unreadable := tableReading([]string{"score"})
	unreadable.Rules = []entities.DecisionRule{{Inputs: []string{"((("}}}

	stored := map[string]map[int]entities.DecisionDefinition{
		"discount": {0: tableReading([]string{"approved"}), 2: tableReading([]string{"amount"})},
		"tier":     {0: tableReading([]string{"approved"}, "risk"), 3: tableReading([]string{"region"}, "risk")},
		"risk":     {0: tableReading([]string{"amount"}, "score"), 2: tableReading([]string{"neverInForce"})},
		"score":    {0: tableReading([]string{"history"})},
		"top":      {0: tableReading([]string{"a"}, "left", "right")},
		"left":     {0: tableReading([]string{"b"}, "shared")},
		"right":    {0: tableReading([]string{"c"}, "shared")},
		"shared":   {0: tableReading([]string{"d"})},
		"ring":     {0: tableReading([]string{"x"}, "ringBack")},
		"ringBack": {0: tableReading([]string{"y"}, "ring")},
		"itself":   {0: tableReading([]string{"z"}, "itself")},
		"orphaned": {0: tableReading([]string{"p"}, "nobodyStored")},
		"onShaky":  {0: tableReading([]string{"q"}, "shaky")},
		"shaky":    {0: unreadable},
	}
	cases := []struct {
		name       string
		key        string
		version    int
		names      []string
		analysable bool
		found      bool
	}{
		{"the version in force", "discount", 0, []string{"approved"}, true, true},
		{"a pinned version", "discount", 2, []string{"amount"}, true, true},
		{"a decision nobody stored", "nobodyStored", 0, nil, false, false},
		{"a version nobody stored", "discount", 9, nil, false, false},
		{"a chain of requirements", "tier", 0, []string{"amount", "approved", "history"}, true, true},
		{"a pinned version's requirements are read at the version in force", "tier", 3, []string{"amount", "history", "region"}, true, true},
		{"two requirements that share one", "top", 0, []string{"a", "b", "c", "d"}, true, true},
		{"a ring of requirements", "ring", 0, []string{"x", "y"}, false, true},
		{"a decision that requires itself", "itself", 0, []string{"z"}, false, true},
		{"a requirement nobody stored", "orphaned", 0, []string{"p"}, false, true},
		{"a requirement that cannot be read", "onShaky", 0, []string{"q", "score"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			lookup := newDecisionLookup((&storedDecisions{versions: stored}).load)
			names, analysable, found := lookup.reads(c.key, c.version)
			if !reflect.DeepEqual(names, c.names) || analysable != c.analysable || found != c.found {
				t.Errorf("reads %v, analysable %v, found %v; want %v, %v, %v", names, analysable, found, c.names, c.analysable, c.found)
			}
			if lookup.err != nil {
				t.Errorf("an error nobody raised: %v", lookup.err)
			}
		})
	}
}

// A definition is somebody's input, and so is the chain of decisions it leads
// to. A plan reads a bounded number of tables, each once however many steps
// consult it; past the bound a decision is unread, which the plan warns of.
func TestDecisionLookupReadsEachTableOnceAndStopsAtItsBound(t *testing.T) {
	t.Parallel()
	const links = 4 * maxDecisionTablesPerPlan
	stored := &storedDecisions{versions: map[string]map[int]entities.DecisionDefinition{
		"shared": {0: tableReading([]string{"d"})},
		"first":  {0: tableReading([]string{"a"}, "shared")},
		"second": {0: tableReading([]string{"b"}, "shared")},
	}}
	for i := range links {
		stored.versions[fmt.Sprintf("link%d", i)] = map[int]entities.DecisionDefinition{
			0: tableReading([]string{fmt.Sprintf("v%d", i)}, fmt.Sprintf("link%d", i+1)),
		}
	}

	lookup := newDecisionLookup(stored.load)
	for range 3 {
		for _, key := range []string{"first", "second"} {
			if _, analysable, found := lookup.reads(key, 0); !analysable || !found {
				t.Fatalf("%s: analysable %v, found %v", key, analysable, found)
			}
		}
	}
	if want := []string{"first@0", "shared@0", "second@0"}; !reflect.DeepEqual(stored.asked, want) {
		t.Errorf("the store was asked for %v, want each table once: %v", stored.asked, want)
	}

	stored.asked = nil
	names, analysable, found := lookup.reads("link0", 0)
	if analysable || !found {
		t.Errorf("a chain of %d decisions: analysable %v, found %v; want it found and not vouched for", links, analysable, found)
	}
	if len(names) == 0 || len(stored.asked) > maxDecisionTablesPerPlan {
		t.Errorf("it read %d name(s) from %d table(s); want what it could read, from no more than %d tables",
			len(names), len(stored.asked), maxDecisionTablesPerPlan)
	}
	// And once the bound is reached nothing more is read for this plan, not
	// even a decision that is one table.
	stored.asked = nil
	if _, analysable, found := lookup.reads("discount", 0); analysable || !found || len(stored.asked) != 0 {
		t.Errorf("past the bound: analysable %v, found %v, %d table(s) read; want unread and nothing asked", analysable, found, len(stored.asked))
	}
}

// A store that fails is not a decision nobody stored: the plan fails with it
// rather than calling the step unreadable.
func TestDecisionLookupKeepsTheErrorOfAStoreThatFails(t *testing.T) {
	t.Parallel()
	down := errors.New("the database went away")
	stored := &storedDecisions{
		versions: map[string]map[int]entities.DecisionDefinition{"tier": {0: tableReading([]string{"approved"}, "risk")}},
		failing:  map[string]error{"risk": down},
	}
	lookup := newDecisionLookup(stored.load)
	if _, analysable, _ := lookup.reads("tier", 0); analysable {
		t.Error("a decision whose requirement could not be read was vouched for")
	}
	if !errors.Is(lookup.err, down) {
		t.Errorf("the lookup's error is %v, want the store's", lookup.err)
	}
}

// What a waive may set is what every open run's form declares; what the
// process may expect from the step is what any run's form declares (plan
// Ruling 8). A value only some runs could set is refused, not guessed.
func TestOutputRefusals(t *testing.T) {
	t.Parallel()
	gateway := entities.DecisionPoint{NodeID: "g", NodeName: "Approved?", Kind: entities.DecisionPointGateway, Analysed: true}
	missing := func(names ...string) []entities.DecisionPoint {
		point := gateway
		point.Missing = names
		return []entities.DecisionPoint{point}
	}
	many := make([]string, 25)
	outputs := map[string]any{}
	for i := range many {
		many[i] = fmt.Sprintf("field%02d", i)
		outputs[many[i]] = i
	}
	cases := []struct {
		name     string
		outputs  map[string]any
		anyRun   map[string]struct{}
		everyRun map[string]struct{}
		points   []entities.DecisionPoint
		want     []string
	}{
		{"everything declared and supplied", map[string]any{"approved": true}, declares("approved"), declares("approved"), []entities.DecisionPoint{gateway}, nil},
		{"nothing set and nothing asked", nil, declares("approved"), declares("approved"), nil, nil},
		{"values no form declares", map[string]any{"zeta": 1, "amount": 2, "approved": true}, declares("approved"), declares("approved"), nil,
			[]string{"“Approve”'s form does not declare amount and zeta, so a waiver cannot set them."}},
		{"a value a decision point is missing", nil, declares("approved"), declares("approved"), missing("approved"),
			[]string{"“Approved?” decides from approved, which “Approve” would have set; say what the waiver counts as by supplying approved."}},
		{"a value only some runs' forms declare, supplied", map[string]any{"amount": 2}, declares("approved", "amount"), declares("approved"), nil,
			[]string{"amount is not declared by every open task of “Approve”, so a waiver cannot supply it."}},
		{"a value only some runs' forms declare, missing at a decision point", nil, declares("approved", "amount"), declares("approved"), missing("amount"),
			[]string{
				"“Approved?” decides from amount, which “Approve” would have set; say what the waiver counts as by supplying amount.",
				"amount is not declared by every open task of “Approve”, so a waiver cannot supply it.",
			}},
		{"more undeclared values than anybody would read", outputs, declares(), declares(), nil,
			[]string{"“Approve”'s form does not declare field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more, so a waiver cannot set them."}},
		{"a decision point missing more than anybody would read", nil, declares(many...), declares(many...), missing(many...),
			[]string{"“Approved?” decides from field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more, " +
				"which “Approve” would have set; say what the waiver counts as by supplying field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := outputRefusals("Approve", c.outputs, c.anyRun, c.everyRun, c.points); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(c.want, "\n  "))
			}
		})
	}
}

func TestListed(t *testing.T) {
	t.Parallel()
	for want, names := range map[string][]string{
		"":           nil,
		"a":          {"a"},
		"a and b":    {"a", "b"},
		"a, b and c": {"a", "b", "c"},
		"1, 2, 3, 4, 5, 6, 7, 8, 9, 10 and 1 more": {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
		"1, 2, 3, 4, 5, 6, 7, 8, 9 and 10":         {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
		"1, 2, 3, 4, 5, 6, 7, 8, 9, 10 and 2 more": {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"},
	} {
		if got := listed(names); got != want {
			t.Errorf("listed(%v) = %q, want %q", names, got, want)
		}
	}
}

// What is malformed is refused before anything is read, as something the
// caller typed and can fix; everything else is made ready for the planner.
func TestNormalizedDeviationCommand(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	preview := func(kind entities.DeviationKind, nodeID string, outputs map[string]any) entities.DeviationCommand {
		return entities.DeviationCommand{InstanceID: id, Kind: kind, NodeID: nodeID, Reason: "because", Outputs: outputs, DryRun: true}
	}
	tooMany := map[string]any{}
	for i := range entities.MaxDeviationOutputs + 1 {
		tooMany[fmt.Sprintf("field%d", i)] = i
	}
	atTheLimit := map[string]any{}
	for i := range entities.MaxDeviationOutputs {
		atTheLimit[fmt.Sprintf("field%d", i)] = i
	}
	apply := preview(entities.DeviationWaive, "step", nil)
	apply.DryRun = false
	blankKey := apply
	blankKey.VisitKey = "   "

	refused := []struct {
		name    string
		command entities.DeviationCommand
		says    string
	}{
		{"a kind that is not one of the three", preview("skip", "step", nil), "kind must be waive, cancel or hold"},
		{"a kind the ledger records and this command does not make", preview(entities.DeviationReassign, "step", nil), "kind must be waive, cancel or hold"},
		{"no kind", preview("", "step", nil), "kind must be waive, cancel or hold"},
		{"a waive that names no step", preview(entities.DeviationWaive, "  ", nil), "say which step: node_id is required for a waive and a hold"},
		{"a hold that names no step", preview(entities.DeviationHold, "", nil), "say which step: node_id is required for a waive and a hold"},
		{"a cancel carrying outputs", preview(entities.DeviationCancel, "step", map[string]any{"approved": true}), "outputs are what a waived step counts as; a cancel sets none"},
		{"a hold carrying outputs", preview(entities.DeviationHold, "step", map[string]any{"approved": true}), "outputs are what a waived step counts as; a hold sets none"},
		{"more outputs than a form has fields", preview(entities.DeviationWaive, "step", tooMany), "a waive sets at most 50 values, and this one names 51"},
		{"outputs heavier than the record keeps", preview(entities.DeviationWaive, "step", map[string]any{"note": strings.Repeat("n", entities.MaxDeviationOutputBytes)}),
			"the outputs are larger than 64 KiB, which is more than the record of a waive keeps"},
		{"an output given as null", preview(entities.DeviationWaive, "step", map[string]any{"approved": nil, "amount": 1, "tier": nil}),
			"outputs approved and tier are null: say what the waiver counts as, or leave them out"},
		{"an output with no name", preview(entities.DeviationWaive, "step", map[string]any{"": 1}), "an output needs the name of the field it sets"},
		{"an output that cannot be written down", preview(entities.DeviationWaive, "step", map[string]any{"approved": func() {}}), "the outputs cannot be written as JSON"},
		{"an apply that names no visit key", apply, "preview first: an apply names the visit_key of the plan it previewed"},
		{"an apply whose visit key is blank", blankKey, "preview first: an apply names the visit_key of the plan it previewed"},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizedDeviationCommand(c.command)
			if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), c.says) {
				t.Errorf("got %v, want an invalid argument saying %q", err, c.says)
			}
		})
	}

	t.Run("what is well formed is trimmed and passed on", func(t *testing.T) {
		t.Parallel()
		command := entities.DeviationCommand{InstanceID: id, Kind: entities.DeviationWaive, NodeID: " step ", Reason: "\n because \t",
			Outputs: atTheLimit, VisitKey: " dv1-key ", DryRun: false}
		got, err := normalizedDeviationCommand(command)
		want := entities.DeviationCommand{InstanceID: id, Kind: entities.DeviationWaive, NodeID: "step", Reason: "because",
			Outputs: atTheLimit, VisitKey: "dv1-key", DryRun: false}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, %v; want %+v", got, err, want)
		}
	})
	t.Run("a reason is the plan's to refuse, not the request's", func(t *testing.T) {
		t.Parallel()
		for _, reason := range []string{"", "   ", strings.Repeat("r", entities.MaxDeviationReasonLength+1)} {
			command := preview(entities.DeviationHold, "step", nil)
			command.Reason = reason
			if _, err := normalizedDeviationCommand(command); err != nil {
				t.Errorf("a reason of %d characters: %v", len(reason), err)
			}
		}
	})
	t.Run("a cancel may name no step, and no outputs is no outputs", func(t *testing.T) {
		t.Parallel()
		got, err := normalizedDeviationCommand(preview(entities.DeviationCancel, "", map[string]any{}))
		if err != nil || got.NodeID != "" || got.Outputs != nil {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

// An administrator of the organization the request is for, and nobody else:
// absent constraint means deny (AGENTS.md §2.3), so no account, no name, no
// organization and no role each refuse.
func TestRequireDeviationAdministrator(t *testing.T) {
	t.Parallel()
	here, elsewhere := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	in := func(organization uuid.UUID) context.Context {
		return entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: organization.String()})
	}
	as := func(ctx context.Context, account any) context.Context {
		return context.WithValue(ctx, pkgauth.UserContextKey, account)
	}
	everywhere := entities.User{Username: "ana", Roles: []string{"admin"}}
	hereOnly := entities.User{Username: "lena", RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleAdmin}}}

	allowed := map[string]context.Context{
		"an administrator everywhere, whatever the case of the role": as(in(here), everywhere),
		"an administrator of this organization alone":                as(in(here), hereOnly),
		"an account carried as a pointer":                            as(in(here), &hereOnly),
	}
	for who, ctx := range allowed {
		actor, err := requireDeviationAdministrator(ctx)
		if err != nil || actor == "" {
			t.Errorf("%s: actor %q, %v", who, actor, err)
		}
	}
	if actor, _ := requireDeviationAdministrator(as(in(here), hereOnly)); actor != "lena" {
		t.Errorf("the actor is %q, want the account's name", actor)
	}

	refused := map[string]context.Context{
		"nobody signed in":                 in(here),
		"a nil account":                    as(in(here), (*entities.User)(nil)),
		"something that is not an account": as(in(here), "ana"),
		"an operator":                      as(in(here), entities.User{Username: "olga", Roles: []string{entities.RoleOperator}}),
		"a designer":                       as(in(here), entities.User{Username: "dina", Roles: []string{entities.RoleDesigner}}),
		"a member":                         as(in(here), entities.User{Username: "mia", Roles: []string{entities.RoleUser}}),
		"an administrator of this organization, asking in another": as(in(elsewhere), hereOnly),
		"an administrator whose request is for no organization":    as(t.Context(), everywhere),
		"an administrator whose organization cannot be read": as(entities.WithTenantContext(t.Context(),
			entities.TenantContext{TenantID: "not-an-id"}), everywhere),
		"an administrator's account with no name": as(in(here), entities.User{Roles: []string{entities.RoleAdmin}}),
	}
	var answers []string
	for who, ctx := range refused {
		actor, err := requireDeviationAdministrator(ctx)
		if !errors.Is(err, apierr.ErrForbidden) || actor != "" {
			t.Errorf("%s: actor %q, %v; want forbidden", who, actor, err)
			continue
		}
		answers = append(answers, err.Error())
	}
	for _, answer := range answers {
		if answer != answers[0] {
			t.Errorf("two refusals read differently, %q and %q: the answer says why, and so who is asking", answers[0], answer)
		}
	}
}
