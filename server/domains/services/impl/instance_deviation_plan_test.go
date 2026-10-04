package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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
	// The lookup says which answers the bound cut short, so a plan can say
	// that, and not that the decision could not be read.
	for key, cut := range map[string]bool{"first": false, "second": false, "shared": false, "link0": true, "discount": true} {
		if lookup.leftUnread(key, 0) != cut {
			t.Errorf("%s: left unread for the bound %v, want %v", key, !cut, cut)
		}
	}
}

// rulesConsulting is a process of steps, each a business rule task consulting
// the same decision.
func rulesConsulting(steps int, decision string) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{Nodes: make([]*entities.Node, steps)}
	for i := range def.Nodes {
		def.Nodes[i] = &entities.Node{ID: fmt.Sprintf("rule%05d", i), Name: fmt.Sprintf("Rule %05d", i), Type: entities.BusinessRuleTask,
			Properties: map[string]any{"decision_key": decision}}
	}
	return def
}

// namesNumbered is count names, each the prefix and a number.
func namesNumbered(count int, prefix string) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("%s%05d", prefix, i)
	}
	return names
}

// Two inputs nobody here wrote multiply: a process of many steps, and a
// decision of many names that requires many more. A plan walks the decision
// once however many steps consult it, reads no more tables than its bound,
// and keeps for each step only the names the waived step declares.
//
// Not parallel: it counts allocations.
func TestManyStepsConsultingOneDecisionCostOneWalkOfIt(t *testing.T) {
	shapes := []struct {
		name       string
		steps      int
		stored     func() *storedDecisions
		analysed   bool
		readsInAll int
	}{
		{
			name: "20,000 steps consulting a decision that requires 5,000 others", steps: 20_000,
			stored: func() *storedDecisions {
				required := namesNumbered(5_000, "risk")
				versions := map[string]map[int]entities.DecisionDefinition{"policy": {0: tableReading([]string{"approved"}, required...)}}
				for _, key := range required[:200] {
					versions[key] = map[int]entities.DecisionDefinition{0: tableReading([]string{key + "Score"})}
				}
				return &storedDecisions{versions: versions}
			},
			// The decision itself and the first 63 it requires are read.
			analysed: false, readsInAll: maxDecisionTablesPerPlan,
		},
		{
			name: "2,000 steps consulting a decision of 5,000 columns", steps: 2_000,
			stored: func() *storedDecisions {
				return &storedDecisions{versions: map[string]map[int]entities.DecisionDefinition{
					"policy": {0: tableReading(append(namesNumbered(5_000, "column"), "approved"))},
				}}
			},
			analysed: true, readsInAll: 5_001,
		},
	}
	for _, shape := range shapes {
		def := rulesConsulting(shape.steps, "policy")
		var stored *storedDecisions
		var points []entities.DecisionPoint
		read := func() {
			stored = shape.stored()
			points = decisionPointsReading(def, "review", declares("approved"), nil, newDecisionLookup(stored.load).reads)
		}
		started := time.Now()
		read()
		elapsed := time.Since(started)
		allocations := testing.AllocsPerRun(1, read)
		t.Logf("%s: read in %s with %.0f allocations, %d table(s) loaded", shape.name, elapsed, allocations, len(stored.asked))

		if elapsed > 5*time.Second {
			t.Errorf("%s: took %s", shape.name, elapsed)
		}
		// Building the fixture's tables is counted too: a few allocations for
		// each of 5,000 names. What must not be there is anything for each
		// step times each name.
		if most := float64(8*shape.steps + 40_000); allocations > most {
			t.Errorf("%s: %.0f allocations, want no more than %.0f", shape.name, allocations, most)
		}
		asked := map[string]int{}
		for _, table := range stored.asked {
			asked[table]++
		}
		if len(stored.asked) > maxDecisionTablesPerPlan || len(asked) != len(stored.asked) {
			t.Errorf("%s: %d tables loaded, %d of them different; want each once and no more than %d",
				shape.name, len(stored.asked), len(asked), maxDecisionTablesPerPlan)
		}
		if len(points) != shape.steps {
			t.Fatalf("%s: %d points, want one for each of %d steps", shape.name, len(points), shape.steps)
		}
		for _, point := range []entities.DecisionPoint{points[0], points[len(points)-1]} {
			if point.Analysed != shape.analysed || point.ReadsInAll != shape.readsInAll || !reflect.DeepEqual(point.Reads, []string{"approved"}) {
				t.Errorf("%s: %s is analysed %v, reads %d in all and lists %v; want %v, %d and [approved]",
					shape.name, point.NodeID, point.Analysed, point.ReadsInAll, point.Reads, shape.analysed, shape.readsInAll)
			}
		}
	}
}

// A plan goes back over the wire, and the process it is made from is somebody
// else's to write. Whatever the process — twenty thousand steps that each
// read two thousand fields of a long form — the plan lists a hundred decision
// points, says how many there are, refuses and warns in a dozen sentences
// each, and is made in the time it takes to read the definition once.
func TestAPlanStaysSmallWhateverTheProcess(t *testing.T) {
	t.Parallel()
	const steps, fields, mostBytes = 20_000, 2_000, 256 << 10
	short, long := namesNumbered(fields, "field"), namesNumbered(fields, strings.Repeat("a very long name for a field ", 8))
	nodes := func(build func(i int) *entities.Node) *entities.ProcessDefinition {
		def := &entities.ProcessDefinition{Nodes: make([]*entities.Node, steps)}
		for i := range def.Nodes {
			def.Nodes[i] = build(i)
		}
		return def
	}
	everyField := func(names []string) decisionReads {
		return func(string, int) ([]string, bool, bool) { return names, true, true }
	}
	shapes := []struct {
		name      string
		def       *entities.ProcessDefinition
		fields    []string
		lookup    decisionReads
		refusals  int
		warnings  int
		refusedBy string
	}{
		{
			name: "a call activity on every step, each handed every field",
			def: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: fmt.Sprintf("call%05d", i), Name: fmt.Sprintf("Call %05d", i), Type: entities.CallActivity}
			}),
			fields: short, refusals: 0, warnings: maxPointsNamed + 2,
		},
		{
			name:   "every step consulting one decision that reads every field",
			def:    rulesConsulting(steps, "policy"),
			fields: short, lookup: everyField(short), refusals: maxPointsNamed + 1,
			warnings: 1, refusedBy: "19990 more steps read field00000, field00001,",
		},
		{
			name:   "every step consulting one decision that reads every field, the fields named at length",
			def:    rulesConsulting(steps, "policy"),
			fields: long, lookup: everyField(long), refusals: maxPointsNamed + 1,
			warnings: 1, refusedBy: "19990 more steps read a very long name",
		},
		{
			name: "a gateway on every step, each with a condition of its own",
			def: func() *entities.ProcessDefinition {
				def := nodes(func(i int) *entities.Node {
					return &entities.Node{ID: fmt.Sprintf("gate%05d", i), Name: fmt.Sprintf("Gate %05d", i), Type: entities.ExclusiveGateway}
				})
				for i := range steps {
					def.Flows = append(def.Flows, flow(fmt.Sprintf("f%05d", i), fmt.Sprintf("gate%05d", i), "end", fmt.Sprintf("field%05d and other%05d", i%fields, i)))
				}
				return def
			}(),
			fields: short, refusals: maxPointsNamed + 1,
			warnings: 1, refusedBy: "19990 more steps read field00000, field00001,",
		},
	}
	for _, shape := range shapes {
		declared := declares(shape.fields...)
		p := &planning{plan: entities.DeviationPlan{NodeID: "review", NodeName: "Review the claim"}}
		started := time.Now()
		points := decisionPointsReading(shape.def, "review", declared, nil, shape.lookup)
		p.takePoints(points, declared, declared, nil)
		elapsed := time.Since(started)
		encoded, err := json.Marshal(p.plan)
		if err != nil {
			t.Fatalf("%s: %v", shape.name, err)
		}
		t.Logf("%s: planned in %s, %d bytes as JSON", shape.name, elapsed, len(encoded))

		if elapsed > 5*time.Second {
			t.Errorf("%s: took %s", shape.name, elapsed)
		}
		if len(encoded) > mostBytes {
			t.Errorf("%s: the plan is %d bytes as JSON, want no more than %d", shape.name, len(encoded), mostBytes)
		}
		if p.plan.DecisionPointsInAll != steps || len(p.plan.DecisionPoints) != maxDecisionPointsListed {
			t.Errorf("%s: %d decision points listed of %d, want %d of %d",
				shape.name, len(p.plan.DecisionPoints), p.plan.DecisionPointsInAll, maxDecisionPointsListed, steps)
		}
		if len(p.plan.Refusals) != shape.refusals || len(p.plan.Warnings) != shape.warnings {
			t.Errorf("%s: %d refusals and %d warnings, want %d and %d", shape.name, len(p.plan.Refusals), len(p.plan.Warnings), shape.refusals, shape.warnings)
		}
		if shape.refusedBy != "" && !strings.HasPrefix(p.plan.Refusals[len(p.plan.Refusals)-1], shape.refusedBy) {
			t.Errorf("%s: the last refusal is %q, want it to count the steps not named: %q…", shape.name, p.plan.Refusals[len(p.plan.Refusals)-1], shape.refusedBy)
		}
		if want := fmt.Sprintf("%d more steps read what “Review the claim” would have set and are not listed here.", steps-maxDecisionPointsListed); !slices.Contains(p.plan.Warnings, want) {
			t.Errorf("%s: the warnings do not say how many points are not listed: %q", shape.name, p.plan.Warnings)
		}
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
//
// A step is named in the first few sentences and counted in the last, so a
// process with thousands of gateways refuses in a dozen sentences and still
// names every value it wants.
func TestOutputRefusals(t *testing.T) {
	t.Parallel()
	gateway := entities.DecisionPoint{NodeID: "g", NodeName: "Approved?", Kind: entities.DecisionPointGateway, Analysed: true}
	missing := func(names ...string) []entities.DecisionPoint {
		point := gateway
		point.Missing = names
		return []entities.DecisionPoint{point}
	}
	list := entities.DecisionPoint{NodeID: "ask", NodeName: "Ask each reviewer", Kind: entities.DecisionPointCollection,
		Analysed: true, Missing: []string{"reviewers"}}
	many := make([]string, 25)
	outputs := map[string]any{}
	for i := range many {
		many[i] = fmt.Sprintf("field%02d", i)
		outputs[many[i]] = i
	}
	// Fourteen gateways, each missing approved; the last two miss region too.
	var gateways []entities.DecisionPoint
	var named []string
	for i := range 14 {
		point := entities.DecisionPoint{NodeID: fmt.Sprintf("g%02d", i), NodeName: fmt.Sprintf("Gate %02d", i),
			Kind: entities.DecisionPointGateway, Analysed: true, Missing: []string{"approved"}}
		if i >= 12 {
			point.Missing = []string{"approved", "region"}
		}
		gateways = append(gateways, point)
		if i < maxPointsNamed {
			named = append(named, fmt.Sprintf("“Gate %02d” decides from approved, which “Approve” would have set; "+
				"say what the waiver counts as by supplying approved.", i))
		}
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
			[]string{"“Approve”'s form does not declare amount, zeta, so a waiver cannot set them."}},
		{"one value no form declares", map[string]any{"amount": 2}, declares("approved"), declares("approved"), nil,
			[]string{"“Approve”'s form does not declare amount, so a waiver cannot set it."}},
		{"a value a decision point is missing", nil, declares("approved"), declares("approved"), missing("approved"),
			[]string{"“Approved?” decides from approved, which “Approve” would have set; say what the waiver counts as by supplying approved."}},
		{"a list a later step repeats over", nil, declares("reviewers"), declares("reviewers"), []entities.DecisionPoint{list},
			[]string{"“Ask each reviewer” takes its list of runs from reviewers, which “Approve” would have set; say what the waiver counts as by supplying reviewers."}},
		{"a value only some runs' forms declare, supplied", map[string]any{"amount": 2}, declares("approved", "amount"), declares("approved"), nil,
			[]string{"amount is not declared by every open task of “Approve”, so a waiver cannot supply it."}},
		{"a value only some runs' forms declare, missing at a decision point", nil, declares("approved", "amount"), declares("approved"), missing("amount"),
			[]string{
				"“Approved?” decides from amount, which “Approve” would have set; say what the waiver counts as by supplying amount.",
				"amount is not declared by every open task of “Approve”, so a waiver cannot supply it.",
			}},
		{"two values only some runs' forms declare", map[string]any{"tier": 1}, declares("approved", "amount", "tier"), declares("approved"), missing("amount"),
			[]string{
				"“Approved?” decides from amount, which “Approve” would have set; say what the waiver counts as by supplying amount.",
				"amount, tier are not declared by every open task of “Approve”, so a waiver cannot supply them.",
			}},
		{"more undeclared values than anybody would read", outputs, declares(), declares(), nil,
			[]string{"“Approve”'s form does not declare field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more, so a waiver cannot set them."}},
		{"a decision point missing more than anybody would read", nil, declares(many...), declares(many...), missing(many...),
			[]string{"“Approved?” decides from field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more, " +
				"which “Approve” would have set; say what the waiver counts as by supplying field00, field01, field02, field03, field04, field05, field06, field07, field08, field09 and 15 more."}},
		{"more decision points than anybody would read", nil, declares("approved", "region"), declares("approved", "region"), gateways,
			append(slices.Clone(named), "4 more steps read approved, region, which “Approve” would have set; say what the waiver counts as by supplying approved, region.")},
		{"one decision point more than is named", nil, declares("approved", "region"), declares("approved", "region"), gateways[:maxPointsNamed+1],
			append(slices.Clone(named), "1 more step reads approved, which “Approve” would have set; say what the waiver counts as by supplying approved.")},
		{"a value only some runs declare, missing at a point that is counted and not named", nil, declares("approved", "region"), declares("approved"), gateways,
			append(slices.Clone(named),
				"4 more steps read approved, region, which “Approve” would have set; say what the waiver counts as by supplying approved, region.",
				"region is not declared by every open task of “Approve”, so a waiver cannot supply it.")},
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

// What nobody could vouch for is said per step for the first few and counted
// after: a script, a process the instance calls, and a decision left unread
// because the process consults more tables than one preview reads.
func TestUnreadPointWarnings(t *testing.T) {
	t.Parallel()
	script := entities.DecisionPoint{NodeID: "g", NodeName: "Route by script", Kind: entities.DecisionPointGateway, Reads: []string{"approved"}}
	called := entities.DecisionPoint{NodeID: "c", NodeName: "Check the supplier", Kind: entities.DecisionPointCalledProcess, Reads: []string{"approved", "urgent"}}
	handedOne := entities.DecisionPoint{NodeID: "d", NodeName: "Tell the supplier", Kind: entities.DecisionPointCalledProcess, Reads: []string{"approved"}}
	crowded := entities.DecisionPoint{NodeID: "t", NodeName: "Apply the discount", Kind: entities.DecisionPointDecisionTable}
	read := entities.DecisionPoint{NodeID: "ok", NodeName: "Approved?", Kind: entities.DecisionPointGateway, Analysed: true}
	pastBound := func(point entities.DecisionPoint) bool { return point.NodeID == "t" }

	got := unreadPointWarnings("Approve", []entities.DecisionPoint{read, called, handedOne, script, crowded}, pastBound)
	want := []string{
		"“Check the supplier” starts another process and hands it approved, urgent, which “Approve” would have set; " +
			"that process was not read, so check what it does with them before applying.",
		"“Tell the supplier” starts another process and hands it approved, which “Approve” would have set; " +
			"that process was not read, so check what it does with it before applying.",
		"“Route by script” could not be read to see what it decides from; check it before applying.",
		"“Apply the discount” was not read: this process consults more decision tables than one preview reads (64). Check it before applying.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	var scripts []entities.DecisionPoint
	for i := range maxPointsNamed + 7 {
		point := script
		point.NodeID, point.NodeName = fmt.Sprintf("g%02d", i), fmt.Sprintf("Script %02d", i)
		scripts = append(scripts, point)
	}
	got = unreadPointWarnings("Approve", scripts, nil)
	if len(got) != maxPointsNamed+1 || got[0] != "“Script 00” could not be read to see what it decides from; check it before applying." ||
		got[maxPointsNamed] != "7 more steps could not be read either; check them before applying." {
		t.Errorf("%d warnings for %d unread points:\n  %s", len(got), len(scripts), strings.Join(got, "\n  "))
	}
	if got = unreadPointWarnings("Approve", scripts[:maxPointsNamed+1], nil); got[maxPointsNamed] != "1 more step could not be read either; check it before applying." {
		t.Errorf("one more than is named: %q", got[maxPointsNamed])
	}
}

// A plan lists the decision points somebody has to act on first — a value
// missing, then one nobody could read — and no more than a screenful of them,
// each with no more names than a sentence would spell out. What it leaves out
// is counted.
func TestListedPoints(t *testing.T) {
	t.Parallel()
	shared := make([]string, 40)
	for i := range shared {
		shared[i] = fmt.Sprintf("field%02d", i)
	}
	var points []entities.DecisionPoint
	for i := range 3 * maxDecisionPointsListed {
		point := entities.DecisionPoint{NodeID: fmt.Sprintf("n%04d", i), Kind: entities.DecisionPointGateway, Analysed: true,
			Reads: shared, Supplied: shared, ReadsInAll: 4_000}
		switch i % 50 {
		case 7:
			point.Supplied, point.Missing = nil, shared
		case 3:
			point.Analysed = false
		}
		points = append(points, point)
	}

	listed := listedPoints(points)
	if len(listed) != maxDecisionPointsListed {
		t.Fatalf("%d points listed of %d, want %d", len(listed), len(points), maxDecisionPointsListed)
	}
	var order []string
	for _, point := range listed[:14] {
		what := "supplied"
		switch {
		case len(point.Missing) > 0:
			what = "missing"
		case !point.Analysed:
			what = "unread"
		}
		order = append(order, point.NodeID+" "+what)
	}
	want := []string{"n0007 missing", "n0057 missing", "n0107 missing", "n0157 missing", "n0207 missing", "n0257 missing",
		"n0003 unread", "n0053 unread", "n0103 unread", "n0153 unread", "n0203 unread", "n0253 unread", "n0000 supplied", "n0001 supplied"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("listed in the order %v\nwant %v", order, want)
	}
	for _, point := range listed {
		if len(point.Reads) > maxNamesShown || len(point.Supplied) > maxNamesShown || len(point.Missing) > maxNamesShown || point.ReadsInAll != 4_000 {
			t.Fatalf("%s lists %d read, %d supplied, %d missing and counts %d; want at most %d of each and the count kept",
				point.NodeID, len(point.Reads), len(point.Supplied), len(point.Missing), point.ReadsInAll, maxNamesShown)
		}
	}
	// The points share their lists: what is listed is cut from copies.
	if len(shared) != 40 || shared[39] != "field39" || len(points[7].Missing) != 40 {
		t.Error("cutting the lists of the points listed wrote into the lists the points share")
	}
	if few := listedPoints(points[:5]); len(few) != 5 {
		t.Errorf("%d of 5 points listed", len(few))
	}
	if none := listedPoints(nil); none != nil {
		t.Errorf("no points listed as %v, want nil", none)
	}
}

// Names are worded for a sentence the way a refused completion words them
// (listedNames): the first few, each cut to a length somebody would read, and
// how many more. A name that is empty is shown as one, not as a gap.
func TestNamesShown(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("n", maxNameShown+20)
	for want, names := range map[string][]string{
		"":      nil,
		"a":     {"a"},
		"a, b":  {"a", "b"},
		`"", b`: {"", "b"},
		`""`:    {""},
		strings.Repeat("n", maxNameShown) + "…":    {long},
		"1, 2, 3, 4, 5, 6, 7, 8, 9, 10":            {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
		"1, 2, 3, 4, 5, 6, 7, 8, 9, 10 and 1 more": {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
	} {
		if got := namesShown(names); got != want {
			t.Errorf("namesShown(%q) = %q, want %q", names, got, want)
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
			"outputs approved, tier are null: say what the waiver counts as, or leave them out"},
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
