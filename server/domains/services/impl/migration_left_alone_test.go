package impl

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Each way a run leaves an instance alone has a cause a client can translate,
// names its steps — by id, and by the name the source version gives them —
// and keeps the sentence the reply has always carried: the English a client
// falls back to.
func TestEveryWayOfBeingPassedOverHasACauseAndNamesItsSteps(t *testing.T) {
	version := models.ProcessDefinitionModel{Version: 1, Nodes: []models.FlowNode{
		{ID: "opsApprove", Name: "Operations approve"}, {ID: "wait"},
		{ID: "sub", Name: "Checks", Nodes: []models.FlowNode{{ID: "extraCheck", Name: "Extra check"}}},
	}}
	source := stepsOfSource(version)
	target := models.ProcessDefinitionModel{Version: 2}
	both := []string{"opsApprove", "wait"}
	// A step the version gives no name goes by its id, as it does in the
	// sentence.
	named := []entities.PassedOverStep{{NodeID: "opsApprove", Name: "Operations approve"}, {NodeID: "wait", Name: "wait"}}
	cases := []struct {
		why   leftAlone
		cause entities.PassedOverCause
		steps []entities.PassedOverStep
		says  string
	}{
		{becauseItLeftTheStep(source, "opsApprove"), entities.PassedOverLeftTheStep, named[:1], leftTheStep(version, "opsApprove")},
		{becauseItStopped(source), entities.PassedOverNoLongerRunning, nil, noLongerRunning(version)},
		{becauseNotPlannedFor(source), entities.PassedOverNotPlannedFor, nil, notPlannedFor(version)},
		{becauseAlreadyMoved(source), entities.PassedOverAlreadyMoved, nil, alreadyMoved(version)},
		{becauseNowhereToLand(source, target, both), entities.PassedOverNowhereToLand, named, nowhereToLand(version, target, both)},
		{becauseNothingDecidesThere(source, target, both), entities.PassedOverLeftWhereNothingDecides, named, leftWhereNothingDecides(version, target, both)},
		{becauseUndecided(source, both), entities.PassedOverWaitingToBeDecided, named, waitingToBeDecided(version, both)},
		{becauseCountersWouldMerge(source), entities.PassedOverCountersWouldMerge, nil, countersWouldMerge(version)},
	}
	given := map[entities.PassedOverCause]bool{}
	for _, c := range cases {
		if c.why.cause != c.cause || !c.cause.Valid() || !slices.Equal(c.why.steps, c.steps) || c.why.stepsInAll != len(c.steps) ||
			c.why.reason != c.says || c.why.none() {
			t.Errorf("%s: got cause %q steps %v of %d reason %q", c.cause, c.why.cause, c.why.steps, c.why.stepsInAll, c.why.reason)
		}
		if c.says == "" {
			t.Errorf("%s: the sentence beside the cause is empty", c.cause)
		}
		// Which causes are about steps is said once, by the cause, and is what
		// the catalogues are held to (tests/roledrift): a cause that says it
		// names steps gives some, and one that says it names none gives none.
		if c.cause.NamesSteps() != (len(c.why.steps) > 0) {
			t.Errorf("%s says it names steps: %v, and its run gives %d", c.cause, c.cause.NamesSteps(), len(c.why.steps))
		}
		given[c.cause] = true
	}
	for _, cause := range entities.PassedOverCauses() {
		if !given[cause] {
			t.Errorf("no run ever gives the cause %q, and a client has words for it", cause)
		}
	}
	if len(given) != len(entities.PassedOverCauses()) {
		t.Errorf("a run gives %d causes and the closed set names %d", len(given), len(entities.PassedOverCauses()))
	}
	if !(leftAlone{}).none() || entities.PassedOverCause("moved").Valid() || entities.PassedOverCause("").Valid() {
		t.Error("an instance that may be moved has no cause, and the causes are a closed set")
	}
	told := passedOver(models.ProcessInstanceModel{}, becauseItLeftTheStep(source, "opsApprove"))
	if told.Cause != entities.PassedOverLeftTheStep || !slices.Equal(told.Steps, named[:1]) || told.StepsInAll != 1 ||
		told.Reason != leftTheStep(version, "opsApprove") {
		t.Errorf("the run's account of it: %+v", told)
	}
}

// A step is named as the sentence beside it names it: a step inside a
// sub-process by its own name, and one the version does not have by the id it
// was asked about.
func TestTheStepsOfACauseAreNamedAsTheSentenceNamesThem(t *testing.T) {
	source := stepsOfSource(namedSteps())
	got, inAll := stepsOf(source, []string{"extraCheck", "gate", "gone"})
	want := []entities.PassedOverStep{
		{NodeID: "extraCheck", Name: "Extra check"}, {NodeID: "gate", Name: "gate"}, {NodeID: "gone", Name: "gone"},
	}
	if !slices.Equal(got, want) || inAll != 3 {
		t.Errorf("stepsOf = %+v of %d, want %+v of 3", got, inAll, want)
	}
	if steps, inAll := stepsOf(source, nil); steps != nil || inAll != 0 {
		t.Errorf("a cause about no step names %+v of %d", steps, inAll)
	}
}

// What a cause lists of its steps has a size. A definition's author chooses
// a step's id and its name, and neither has a length: each is cut as the
// ledger and a waive's plan cut a step's name. And an instance can hold work
// on as many steps as its version has: the first ten are listed, in the order
// they had, beside how many there were.
func TestTheStepsOfACauseAreCutAndCounted(t *testing.T) {
	long := strings.Repeat("é", 300)
	version := models.ProcessDefinitionModel{Version: 1, Nodes: []models.FlowNode{{ID: long, Name: "Étape " + long}}}
	for n := range 25 {
		version.Nodes = append(version.Nodes, models.FlowNode{ID: fmt.Sprintf("step%02d", n), Name: fmt.Sprintf("Step %02d", n)})
	}
	source := stepsOfSource(version)

	cut, inAll := stepsOf(source, []string{long})
	if len(cut) != 1 || inAll != 1 {
		t.Fatalf("one step is listed as %d of %d", len(cut), inAll)
	}
	if got := utf8.RuneCountInString(cut[0].NodeID); got != deviationNodeNameLength || !strings.HasPrefix(long, cut[0].NodeID) || !utf8.ValidString(cut[0].NodeID) {
		t.Errorf("an id of 300 characters is listed with %d, want its first %d, cut between characters", got, deviationNodeNameLength)
	}
	if got := utf8.RuneCountInString(cut[0].Name); got != deviationNodeNameLength || !strings.HasPrefix("Étape "+long, cut[0].Name) || !utf8.ValidString(cut[0].Name) {
		t.Errorf("a name of 306 characters is listed with %d, want its first %d, cut between characters", got, deviationNodeNameLength)
	}

	var ids []string
	for n := range 25 {
		ids = append(ids, fmt.Sprintf("step%02d", n))
	}
	listed, inAll := stepsOf(source, ids)
	if len(listed) != entities.MaxPassedOverSteps || entities.MaxPassedOverSteps != 10 || inAll != 25 {
		t.Fatalf("25 steps are listed as %d of %d, want 10 of 25", len(listed), inAll)
	}
	for n, step := range listed {
		if step.NodeID != ids[n] || step.Name != fmt.Sprintf("Step %02d", n) {
			t.Errorf("step %d listed is %+v, want the %dth of those given, in their order", n, step, n)
		}
	}
	why := becauseNowhereToLand(source, models.ProcessDefinitionModel{Version: 2}, ids)
	if len(why.steps) != 10 || why.stepsInAll != 25 {
		t.Errorf("the cause lists %d steps of %d, want 10 of 25", len(why.steps), why.stepsInAll)
	}
	// The sentence names the steps the list does, and counts the rest as the
	// list does: it was every step in full until the sentence was bounded
	// too, and this asserted that it was.
	if why.reason != nowhereToLand(version, models.ProcessDefinitionModel{Version: 2}, ids) ||
		!strings.Contains(why.reason, `"Step 08", "Step 09" and 15 more`) || strings.Contains(why.reason, `"Step 10"`) {
		t.Errorf("the sentence beside a cut list is not cut where the list is: %q", why.reason)
	}
}

// The names of a version's steps are read once, for the run, and every name
// asked of them is the name the sentence gave when it looked through the
// definition each time: a nested step by its own name, a step with no name
// and a step the version does not have by the id, and of two steps under one
// id the first.
//
// A definition is somebody's input. Nothing in the reading goes round: a
// sub-process that names itself as its parent, or two that name each other,
// are read as the steps they are; and one nested very deep is read without a
// call inside a call.
func TestTheNamesOfAVersionAreReadOnceAndNeverGoRound(t *testing.T) {
	deep := models.FlowNode{ID: "innermost", Name: "Innermost"}
	for n := range 20000 {
		deep = models.FlowNode{ID: fmt.Sprintf("level%d", n), Nodes: []models.FlowNode{deep}}
	}
	nodes := []models.FlowNode{
		{ID: "opsApprove", Name: "Operations approve"},
		{ID: "gate"},
		{ID: "loop", Name: "Its own parent", ParentID: "loop", Nodes: []models.FlowNode{
			{ID: "inside", Name: "Inside the loop", ParentID: "loop"},
		}},
		{ID: "a", Name: "A, whose parent is B", ParentID: "b"},
		{ID: "b", Name: "B, whose parent is A", ParentID: "a"},
		{ID: "twice", Name: "The first of two"},
		{ID: "sub", Name: "Checks", Nodes: []models.FlowNode{{ID: "twice", Name: "The second of two"}, {ID: "extraCheck", Name: "Extra check"}}},
		{ID: "unnamedTwice"},
		{ID: "unnamedTwice", Name: "Named the second time"},
		deep,
	}
	read := make(chan sourceSteps, 1)
	go func() { read <- stepsOfSource(models.ProcessDefinitionModel{Version: 7, Nodes: nodes}) }()
	var source sourceSteps
	select {
	case source = <-read:
	case <-time.After(10 * time.Second):
		t.Fatal("the names of the version had not been read after ten seconds")
	}
	if source.version != 7 {
		t.Errorf("the version is read as %d", source.version)
	}
	for id, want := range map[string]string{
		"opsApprove": "Operations approve", "gate": "gate", "loop": "Its own parent", "inside": "Inside the loop",
		"a": "A, whose parent is B", "b": "B, whose parent is A", "twice": "The first of two", "extraCheck": "Extra check",
		"unnamedTwice": "unnamedTwice", "innermost": "Innermost", "level0": "level0", "gone": "gone", "": "",
	} {
		if got := source.name(id); got != want {
			t.Errorf("the step %q is named %q, want %q", id, got, want)
		}
		if was := nodeNameIn(nodes, id); was != want {
			t.Errorf("the step %q was named %q by the walk the sentence used to make: this test's expectation is wrong", id, was)
		}
	}
	if got := source.quoted([]string{"extraCheck", "gate", "opsApprove"}); got != `"Extra check", "gate" and "Operations approve"` {
		t.Errorf("three steps read as %s", got)
	}
}

// A run's account of an instance it left alone is made in one place, from a
// reason one of the eight constructors made: nothing else in the package
// builds one, so nothing can give an instance a cause outside the closed set,
// or none. A reply writes the cause it is given; this is what keeps it from
// ever being given an empty one.
func TestNothingButPassedOverMakesARunsAccountOfAnInstanceLeftAlone(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list the package: %v", err)
	}
	accounts, reasons := map[string]int{}, map[string]int{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if n := strings.Count(string(source), "entities.PassedOverInstance{"); n > 0 {
			accounts[file] = n
		}
		// A reason with something in it: the zero value, leftAlone{}, is "no
		// reason", and is written wherever an instance may be moved.
		if n := strings.Count(string(source), "leftAlone{") - strings.Count(string(source), "leftAlone{}"); n > 0 {
			reasons[file] = n
		}
	}
	if len(accounts) != 1 || accounts["migration_passed_over.go"] != 1 {
		t.Errorf("a run's account of an instance left alone is built in %v, want once, in passedOver", accounts)
	}
	if len(reasons) != 1 || reasons["migration_left_alone.go"] == 0 {
		t.Errorf("a reason for leaving an instance alone is built in %v, want only in migration_left_alone.go, by the constructors", reasons)
	}
}

// The sentence beside the list of steps is bounded as the list is. An
// instance can hold work on as many steps as its version has, each named at
// whatever length the definition's author chose, and a run can pass over
// hundreds of instances: the sentence names the first ten, each cut where a
// step's name is cut wherever one is shown, and says how many it left out.
// Up to ten steps, with names of an ordinary length, it reads as it always
// did.
func TestTheSentenceOfAPassedOverInstanceNamesTenStepsAndCountsTheRest(t *testing.T) {
	long := strings.Repeat("é", 4000)
	nodes := []models.FlowNode{{ID: "wordy", Name: long}}
	var ids []string
	for i := range 12 {
		id := fmt.Sprintf("step%02d", i)
		nodes = append(nodes, models.FlowNode{ID: id, Name: fmt.Sprintf("Step %02d", i)})
		ids = append(ids, id)
	}
	source := stepsOfSource(models.ProcessDefinitionModel{Version: 3, Nodes: nodes})

	ten := `"Step 00", "Step 01", "Step 02", "Step 03", "Step 04", "Step 05", "Step 06", "Step 07", "Step 08" and "Step 09"`
	if got := source.quoted(ids[:10]); got != ten {
		t.Errorf("ten steps read as\n  %s\nwant them all, as before\n  %s", got, ten)
	}
	twelve := `"Step 00", "Step 01", "Step 02", "Step 03", "Step 04", "Step 05", "Step 06", "Step 07", "Step 08", "Step 09" and 2 more`
	if got := source.quoted(ids); got != twelve {
		t.Errorf("twelve steps read as\n  %s\nwant the first ten and a count\n  %s", got, twelve)
	}
	if got, want := source.quoted([]string{"wordy"}), `"`+strings.Repeat("é", deviationNodeNameLength)+`"`; got != want {
		t.Errorf("a name of 4000 characters is said in %d, want it cut at %d", len([]rune(got))-2, deviationNodeNameLength)
	}
	// Each of the three sentences that name steps, over a version of many.
	target := models.ProcessDefinitionModel{Version: 4}
	for cause, sentence := range map[string]string{
		"nowhere to land":            source.nowhereToLand(target, ids),
		"left where nothing decides": source.leftWhereNothingDecides(target, ids),
		"waiting to be decided":      source.waitingToBeDecided(ids),
	} {
		if !strings.Contains(sentence, twelve) || strings.Contains(sentence, "Step 10") {
			t.Errorf("%s: the sentence names more than ten steps: %s", cause, sentence)
		}
	}
	listed, inAll := stepsOf(source, ids)
	if len(listed) != entities.MaxPassedOverSteps || inAll != 12 {
		t.Errorf("the list beside the sentence has %d of %d, want ten of twelve — the sentence is cut where the list is", len(listed), inAll)
	}
}
