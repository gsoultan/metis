package impl

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Design §9.3: exactly two things need a second administrator — a skip, and a
// control dropped for instances that had not passed it — and only over
// instances that are still running: the run acts on no other.
func TestSecondApproverReasons(t *testing.T) {
	nodes := map[string]models.FlowNode{"ops": {ID: "ops", Name: "Operations approve"}, "wait": {ID: "wait"}}
	skip := map[string]servicecontracts.NodeAction{"ops": {Kind: servicecontracts.NodeActionSkip, Reason: "moot"}}
	cancel := map[string]servicecontracts.NodeAction{"ops": {Kind: servicecontracts.NodeActionCancel, Reason: "void"}}
	held := map[string]servicecontracts.NodeAction{"ops": {Kind: servicecontracts.NodeActionHold, Reason: "ask"}}
	holds := []entities.ComplianceHold{{NodeID: "wait", Instances: 9}}
	waiting := models.ProcessInstanceModel{Status: models.ProcessActive}
	passed := models.ProcessInstanceModel{Status: models.ProcessActive, CompletedNodes: []string{"wait"}}
	ended := models.ProcessInstanceModel{Status: models.ProcessCompleted}
	withdrawn := models.ProcessInstanceModel{Status: models.ProcessCancelled}
	listed := []models.ProcessInstanceModel{waiting, waiting, passed, ended, withdrawn}
	running := runningOf(listed)
	if len(running) != 3 {
		t.Fatalf("%d of five listed instances count as running, want the three that have not ended", len(running))
	}
	if got := secondApproverReasons(nodes, skip, nil, running); len(got) != 1 || got[0] != "“Operations approve” would be skipped for every listed instance waiting at it when the migration runs — not only those waiting there when this was asked for — and nobody would perform it" {
		t.Fatalf("a skip: %v", got)
	}
	// Two running instances have not passed the control; the hold's own count
	// (nine) includes instances that ended, and is not what is said.
	if got := secondApproverReasons(nodes, nil, holds, running); len(got) != 1 || got[0] != "“wait” carries a control 2 instance(s) have not passed, and they never would" {
		t.Fatalf("a hold: %v", got)
	}
	// Rulings §16: with nothing running, nobody is asked — for a skip or for a
	// control only ended instances had not passed.
	nothingRuns := runningOf([]models.ProcessInstanceModel{ended, withdrawn})
	if got := secondApproverReasons(nodes, skip, holds, nothingRuns); len(got) != 0 {
		t.Fatalf("a skip and a dropped control over a version nothing runs on: %v", got)
	}
	if got := secondApproverReasons(nodes, nil, holds, []models.ProcessInstanceModel{passed}); len(got) != 0 {
		t.Fatalf("a control every running instance has passed: %v", got)
	}
	if got := secondApproverReasons(nodes, cancel, nil, running); len(got) != 0 {
		t.Fatalf("a cancel: %v", got)
	}
	if got := secondApproverReasons(nodes, held, nil, running); len(got) != 0 {
		t.Fatalf("a hold of the instance: %v", got)
	}
	// Both at once are both said, in one order whatever order they were found in.
	both := secondApproverReasons(nodes, skip, holds, running)
	if len(both) != 2 || !slices.IsSorted(both) {
		t.Fatalf("a skip and a dropped control: %v, want the two, sorted", both)
	}
	// A step with no name is called by its id.
	if got := secondApproverReasons(nodes, map[string]servicecontracts.NodeAction{"wait": skip["ops"]}, nil, running); len(got) != 1 || !strings.HasPrefix(got[0], "“wait” would be skipped") {
		t.Fatalf("a skip of a step with no name: %v", got)
	}
}

// Who is counted is every instance that has not ended, not only the ones
// running at the moment of the listing. A suspended instance can be made
// active again between the plan's listing and the run's, and the run would
// then act on it: counted, it was shown to whoever approved. An instance in a
// state nothing here knows is counted for the same reason. One that ran to
// its end, failed or was cancelled never runs again, and is not.
func TestAnInstanceThatHasNotEndedIsCountedWhateverItsState(t *testing.T) {
	nodes := map[string]models.FlowNode{"ops": {ID: "ops", Name: "Operations approve"}}
	skip := map[string]servicecontracts.NodeAction{"ops": {Kind: servicecontracts.NodeActionSkip, Reason: "moot"}}
	holds := []entities.ComplianceHold{{NodeID: "ops"}}
	for status, counted := range map[models.ProcessStatus]bool{
		models.ProcessActive:           true,
		models.ProcessSuspended:        true,
		models.ProcessStatus("paused"): true,
		models.ProcessCompleted:        false,
		models.ProcessFailed:           false,
		models.ProcessCancelled:        false,
	} {
		one := uuid.Must(uuid.NewV7())
		listed := []models.ProcessInstanceModel{{ID: models.UUID(one), Status: status}}
		if got := len(runningOf(listed)) == 1; got != counted {
			t.Errorf("an instance that is %s: counted %v, want %v", status, got, counted)
		}
		if got := len(activeInstanceIDs(listed)) == 1; got != counted {
			t.Errorf("an instance that is %s: among those a request covers %v, want %v", status, got, counted)
		}
		if got := len(secondApproverReasons(nodes, skip, nil, runningOf(listed))) == 1; got != counted {
			t.Errorf("a skip over a version whose only instance is %s: needs a second administrator %v, want %v", status, got, counted)
		}
		if got := len(secondApproverReasons(nodes, nil, holds, runningOf(listed))) == 1; got != counted {
			t.Errorf("a control dropped for a version whose only instance is %s: needs a second administrator %v, want %v", status, got, counted)
		}
	}
}

// A redirect — a mapping that sends a step's work to a different step, not a
// rename — is not one administrator's call while any instance that has not
// ended has a control still to pass. Nothing is known about where that
// control lies, so nothing about it decides: the sentence says which step is
// sent where, how many wait there now, and which controls are at stake.
func TestRedirectsPastControls(t *testing.T) {
	marked := map[string]any{"compliance_relevant": true}
	source := map[string]models.FlowNode{
		"prepare": {ID: "prepare", Name: "Prepare"},
		"control": {ID: "control", Name: "Second signature", Properties: marked},
		"audit":   {ID: "audit", Properties: marked},
		"sign":    {ID: "sign", Name: "Sign"},
	}
	target := map[string]models.FlowNode{"prepare": source["prepare"], "control": source["control"], "audit": source["audit"],
		"sign": source["sign"], "draft": {ID: "draft", Name: "Draft"}}
	at := func(step string, done ...string) models.ProcessInstanceModel {
		return models.ProcessInstanceModel{Status: models.ProcessActive, Tokens: []models.Token{{NodeID: step}}, CompletedNodes: done}
	}
	redirect := map[string]string{"prepare": "sign"}
	passedAll := []string{"prepare", "control", "audit"}

	got := redirectsPastControls(source, target, redirect, nil, []models.ProcessInstanceModel{at("prepare"), at("prepare"), at("sign", passedAll...)})
	want := "“Prepare” would be redirected to “Sign” for every listed instance waiting at it when the migration runs — 2 wait(s) there now — " +
		"and a control such an instance has not passed may no longer be ahead of it: “audit”, “Second signature”"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("a redirect past controls:\n  %v\nwant\n  %s", got, want)
	}
	// The controls named are those an instance waiting at the step has not
	// passed, and not those of an instance somewhere else.
	got = redirectsPastControls(source, target, redirect, nil, []models.ProcessInstanceModel{at("prepare", "audit"), at("draft")})
	if len(got) != 1 || !strings.HasSuffix(got[0], "— 1 wait(s) there now — and a control such an instance has not passed may no longer be ahead of it: “Second signature”") {
		t.Fatalf("a redirect of a step whose one waiting instance has passed the audit: %v", got)
	}
	// It is the redirect that is approved, over the instances listed: one that
	// has not reached the step yet is reason enough, with nobody there now —
	// and the sentence says that nobody is, and whose controls it names.
	const mayYetReach = "and for an instance that reaches it by then, a control it has not passed may no longer be ahead of it: “audit”, “Second signature”"
	got = redirectsPastControls(source, target, redirect, nil, []models.ProcessInstanceModel{at("draft")})
	if len(got) != 1 || !strings.HasSuffix(got[0], "when the migration runs — nobody waits there now — "+mayYetReach) {
		t.Fatalf("a redirect of a step nobody waits at yet: %v", got)
	}
	// Whoever waits there has passed every control, and somebody else has not.
	got = redirectsPastControls(source, target, redirect, nil, []models.ProcessInstanceModel{at("prepare", "control", "audit"), at("draft")})
	if len(got) != 1 || !strings.HasSuffix(got[0], "when the migration runs — 1 wait(s) there now, having passed every control — "+mayYetReach) {
		t.Fatalf("a redirect of a step whose one waiting instance has passed every control: %v", got)
	}
	// Every control passed by every instance that has not ended: nothing to lose.
	if got := redirectsPastControls(source, target, redirect, nil, []models.ProcessInstanceModel{at("prepare", "control", "audit")}); len(got) != 0 {
		t.Fatalf("a redirect once every control has been passed: %v", got)
	}
	// Nothing that has not ended: nobody to move.
	if got := redirectsPastControls(source, target, redirect, nil, nil); len(got) != 0 {
		t.Fatalf("a redirect over a version nothing runs on: %v", got)
	}
	// A step renamed where it stands moves nobody past anything, and neither
	// does a step mapped to itself, nor a mapping from a step the version does
	// not have.
	renamed := map[string]string{"prepare": "draft"}
	for name, c := range map[string]struct{ mapping, inPlace map[string]string }{
		"a step renamed where it stands": {renamed, renamed},
		"a step mapped to itself":        {map[string]string{"prepare": "prepare"}, nil},
		"a step the version has not got": {map[string]string{"review": "sign"}, nil},
		"no mapping":                     {nil, nil},
	} {
		if got := redirectsPastControls(source, target, c.mapping, c.inPlace, []models.ProcessInstanceModel{at("prepare")}); len(got) != 0 {
			t.Errorf("%s: %v, want it to ask nobody", name, got)
		}
	}
	// The same mapping onto an id new to the version, of a step that does not
	// stand where the old one stood, is a redirect: a new id excuses nothing.
	if got := redirectsPastControls(source, target, renamed, nil, []models.ProcessInstanceModel{at("prepare")}); len(got) != 1 ||
		!strings.HasPrefix(got[0], "“Prepare” would be redirected to “Draft”") {
		t.Fatalf("a mapping onto a new id that is not in place: %v, want it to ask", got)
	}
	// A process with no control asks nobody, whatever is redirected.
	plain := map[string]models.FlowNode{"prepare": source["prepare"], "sign": source["sign"]}
	if got := redirectsPastControls(plain, plain, redirect, nil, []models.ProcessInstanceModel{at("prepare")}); len(got) != 0 {
		t.Fatalf("a redirect in a process with no control: %v", got)
	}
	// Many controls are counted, and five of them named.
	many := map[string]models.FlowNode{"prepare": source["prepare"], "sign": source["sign"]}
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"} {
		many[id] = models.FlowNode{ID: id, Properties: marked}
	}
	if got := redirectsPastControls(many, many, redirect, nil, []models.ProcessInstanceModel{at("prepare")}); len(got) != 1 ||
		!strings.HasSuffix(got[0], "ahead of it: “c1”, “c2”, “c3”, “c4”, “c5”, and 2 more") {
		t.Fatalf("a redirect past seven controls: %v", got)
	}
	// A name is as long as a definition's author made it; a reason says as
	// much of it as a ledger row keeps.
	long := strings.Repeat("é", 4000)
	wordy := map[string]models.FlowNode{
		"prepare": {ID: "prepare", Name: long}, "sign": {ID: "sign", Name: long},
		"control": {ID: "control", Name: long, Properties: marked},
	}
	got = redirectsPastControls(wordy, wordy, redirect, nil, []models.ProcessInstanceModel{at("prepare")})
	if len(got) != 1 || len([]rune(got[0])) > 3*deviationNodeNameLength+400 || strings.Contains(got[0], strings.Repeat("é", deviationNodeNameLength+1)) {
		t.Fatalf("a redirect among steps with names of 4000 characters is said in %d characters", len([]rune(strings.Join(got, ""))))
	}
}

// A separation-of-duties rule the new version takes away, in whole or in
// part, from a step some instance has still to pass is not one
// administrator's call. A rule kept — under the same names or the steps' new
// ones — a rule that grows, and a rule taken from a step everybody has passed
// are.
func TestDutiesLoosened(t *testing.T) {
	ruled := func(id, name, rule string) models.FlowNode {
		node := models.FlowNode{ID: id, Name: name}
		if rule != "" {
			node.Properties = map[string]any{SeparationOfDutiesKey: rule}
		}
		return node
	}
	source := map[string]models.FlowNode{
		"submit":  ruled("submit", "Submit", ""),
		"check":   ruled("check", "Check", ""),
		"approve": ruled("approve", "Approve", "submit, check"),
	}
	target := func(approve models.FlowNode, others ...models.FlowNode) map[string]models.FlowNode {
		nodes := map[string]models.FlowNode{approve.ID: approve}
		for _, other := range others {
			nodes[other.ID] = other
		}
		return nodes
	}
	waiting := []models.ProcessInstanceModel{{Status: models.ProcessActive, CompletedNodes: []string{"submit", "check"}}, {Status: models.ProcessActive}}
	const lostBoth = "“Approve” would no longer be refused to whoever performed “Submit”, “Check”: the new version does not keep that separation of duties, and 2 instance(s) have not passed “Approve”"
	const checkGone = "“Approve” may not be done by whoever performed “Check”, which the new version no longer has: whoever performed it before the migration " +
		"is still refused, but nobody can perform it afterwards, so the rule refuses nobody new — and 2 instance(s) have not passed “Approve”"
	const lostCheck = "“Approve” would no longer be refused to whoever performed “Check”: the new version does not keep that separation of duties, and 2 instance(s) have not passed “Approve”"

	for name, c := range map[string]struct {
		target  map[string]models.FlowNode
		mapping map[string]string
		want    string
	}{
		"the rule removed":                                {target(ruled("approve", "Approve", ""), source["submit"], source["check"]), nil, lostBoth},
		"the rule shortened":                              {target(ruled("approve", "Approve", "submit"), source["submit"], source["check"]), nil, lostCheck},
		"the rule kept":                                   {target(source["approve"], source["submit"], source["check"]), nil, ""},
		"the rule lengthened":                             {target(ruled("approve", "Approve", "submit,check,audit"), source["submit"], source["check"]), nil, ""},
		"a named step gone, the rule still naming it":     {target(source["approve"], source["submit"]), nil, checkGone},
		"a named step gone, and its name out of the rule": {target(ruled("approve", "Approve", "submit"), source["submit"]), nil, lostCheck},
		"both named steps gone, the rule still naming them": {target(source["approve"]), nil,
			"“Approve” may not be done by whoever performed “Submit”, “Check”, which the new version no longer has: whoever performed it before the migration " +
				"is still refused, but nobody can perform it afterwards, so the rule refuses nobody new — and 2 instance(s) have not passed “Approve”"},
		// The new version keeps submit and adds request: a mapping between
		// them is no rename, finished work stays under submit, and the rule
		// that still names submit still finds it.
		"a named step mapped onto a new one and still there, the rule unchanged": {
			target(source["approve"], source["submit"], source["check"], ruled("request", "Request", "")),
			map[string]string{"submit": "request"}, ""},
		"the steps renamed and the rule with them": {target(ruled("signOff", "Sign off", "request, check"), ruled("request", "Request", ""), source["check"]),
			map[string]string{"submit": "request", "approve": "signOff"}, ""},
		"the steps renamed and the rule left with the old names": {target(ruled("signOff", "Sign off", "submit, check"), ruled("request", "Request", ""), source["check"]),
			map[string]string{"submit": "request", "approve": "signOff"},
			"“Approve” would no longer be refused to whoever performed “Submit”: the new version does not keep that separation of duties, and 2 instance(s) have not passed “Approve”"},
		"the step redirected onto one with no rule": {target(ruled("approve", "Approve", "submit, check"), source["submit"], source["check"]),
			map[string]string{"approve": "check"}, lostBoth},
		"the step itself gone": {target(source["submit"], source["check"]), nil, ""},
		// The new version keeps a step under the id, with no rule, and the
		// mapping sends the step onto another that has the rule. An instance
		// that has not reached the step is not there to be moved: it comes to
		// the step under the old id, which refuses nobody.
		"the step mapped onto one that keeps the rule, the step under its id having lost it": {
			target(ruled("approve", "Approve", ""), source["submit"], source["check"], ruled("senior", "Senior", "submit, check")),
			map[string]string{"approve": "senior"}, lostBoth},
		"the step mapped onto one that keeps the rule, the step under its id having lost part of it": {
			target(ruled("approve", "Approve", "submit"), source["submit"], source["check"], ruled("senior", "Senior", "submit, check")),
			map[string]string{"approve": "senior"}, lostCheck},
		"the step mapped onto another, both keeping the rule": {
			target(source["approve"], source["submit"], source["check"], ruled("senior", "Senior", "submit, check")),
			map[string]string{"approve": "senior"}, ""},
	} {
		got := dutiesLoosened(source, c.target, c.mapping, nil, waiting)
		if (c.want == "" && len(got) != 0) || (c.want != "" && (len(got) != 1 || got[0] != c.want)) {
			t.Errorf("%s:\n  %v\nwant\n  %q", name, got, c.want)
		}
	}
	// A rule that names a step neither version has is the same rule before
	// and after: it refused nobody on that name, and refuses nobody now.
	ghost := map[string]models.FlowNode{
		"submit": source["submit"], "approve": ruled("approve", "Approve", "submit, opsApprove"),
	}
	if got := dutiesLoosened(ghost, ghost, nil, nil, waiting); len(got) != 0 {
		t.Errorf("a rule naming a step neither version has, unchanged: %v, want it to ask nobody", got)
	}
	// Taken out of the rule, the name is a name the rule no longer has: said
	// as any other, though it never refused anybody.
	if got := dutiesLoosened(ghost, target(ruled("approve", "Approve", "submit"), source["submit"]), nil, nil, waiting); len(got) != 1 ||
		!strings.HasPrefix(got[0], "“Approve” would no longer be refused to whoever performed “opsApprove”") {
		t.Errorf("a rule that stops naming a step neither version has: %v", got)
	}
	// A step the rule names is renamed onto a control. In place, finished
	// work follows it, and a rule renamed with it has loosened nothing. Onto
	// a control that stands elsewhere it does not: the submit done stays
	// under its old id, and a rule that names the new one no longer finds who
	// did it.
	marked := ruled("request", "Request", "")
	marked.Properties = map[string]any{"compliance_relevant": true}
	renamedOntoAControl := target(ruled("approve", "Approve", "request, check"), marked, source["check"])
	onto := map[string]string{"submit": "request"}
	if got := dutiesLoosened(source, renamedOntoAControl, onto, onto, waiting); len(got) != 0 {
		t.Errorf("a named step renamed in place onto a control, the rule renamed with it: %v, want it to ask nobody", got)
	}
	if got := dutiesLoosened(source, renamedOntoAControl, onto, nil, waiting); len(got) != 1 ||
		!strings.HasPrefix(got[0], "“Approve” would no longer be refused to whoever performed “Submit”: ") {
		t.Errorf("a named step renamed onto a control that stands elsewhere, the rule naming the new id: %v", got)
	}
	// And the reason says which of the two it is. Here the new version did
	// keep the rule — it names the step “Submit” is mapped onto — and what is
	// lost is that work already done does not follow that mapping. "The new
	// version does not keep that separation of duties" would be untrue of it.
	const notFollowed = "“Approve” would no longer be refused to whoever performed “Submit”: the new version's rule names the step that is " +
		"mapped onto, but work already done does not follow that mapping, so the rule would not find who did it — " +
		"and 2 instance(s) have not passed “Approve”"
	if got := dutiesLoosened(source, renamedOntoAControl, onto, nil, waiting); len(got) != 1 || got[0] != notFollowed {
		t.Errorf("the names no longer line up only because finished work does not follow the mapping:\n  %v\nwant\n  %q", got, notFollowed)
	}
	// Nobody has still to pass the step, or nobody runs at all: nothing is
	// loosened for anybody.
	gone := target(ruled("approve", "Approve", ""), source["submit"], source["check"])
	passed := []models.ProcessInstanceModel{{Status: models.ProcessActive, CompletedNodes: []string{"submit", "check", "approve"}}}
	if got := dutiesLoosened(source, gone, nil, nil, passed); len(got) != 0 {
		t.Errorf("a rule dropped from a step every instance has passed: %v", got)
	}
	if got := dutiesLoosened(source, gone, nil, nil, nil); len(got) != 0 {
		t.Errorf("a rule dropped over a version nothing runs on: %v", got)
	}
}

// The instances a request covers are the running ones of the plan's listing,
// in one order whatever order they were listed in.
func TestActiveInstanceIDs(t *testing.T) {
	one, two, three := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	covered := []models.ProcessInstanceModel{
		{ID: models.UUID(three), Status: models.ProcessActive},
		{ID: models.UUID(two), Status: models.ProcessCompleted},
		{ID: models.UUID(one), Status: models.ProcessActive},
	}
	got := activeInstanceIDs(covered)
	want := []uuid.UUID{one, three}
	slices.SortFunc(want, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if !slices.Equal(got, want) {
		t.Fatalf("the running instances are %v, want %v", got, want)
	}
	if got := activeInstanceIDs(nil); got == nil || len(got) != 0 {
		t.Fatalf("no instances: %v, want a list with nothing in it", got)
	}
}

// What an approval covers, as design §9.5 words it: the same policy, over no
// instance the request did not show. Fewer instances is still covered.
func TestWhyNoLongerHolds(t *testing.T) {
	one, two, three := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	request := entities.DeviationRequest{Fingerprint: "mf1-asked", ApprovedInstances: []uuid.UUID{one, two}}
	if why := whyNoLongerHolds(request, "mf1-asked", []uuid.UUID{two}); why != "" {
		t.Fatalf("an instance that left made the request stale: %q", why)
	}
	if why := whyNoLongerHolds(request, "mf1-asked", nil); why != "" {
		t.Fatalf("every instance having left made the request stale: %q", why)
	}
	if why := whyNoLongerHolds(request, "mf1-other", []uuid.UUID{one, two}); !strings.Contains(why, "no longer the one that was asked for") {
		t.Fatalf("another policy: %q", why)
	}
	if why := whyNoLongerHolds(request, "mf1-asked", []uuid.UUID{one, three}); !strings.Contains(why, three.String()) || strings.Contains(why, one.String()) {
		t.Fatalf("a newcomer: %q, want it named and nobody else", why)
	}
	// A request with no fingerprint covers nothing, whatever is compared with it.
	if why := whyNoLongerHolds(entities.DeviationRequest{ApprovedInstances: []uuid.UUID{one}}, "", []uuid.UUID{one}); why == "" {
		t.Fatal("a request with no fingerprint covers a migration with none")
	}
	// Many newcomers are counted, and five of them named.
	var many []uuid.UUID
	for range 8 {
		many = append(many, uuid.Must(uuid.NewV7()))
	}
	why := whyNoLongerHolds(request, "mf1-asked", many)
	if !strings.HasPrefix(why, "8 instance(s) reached") || !strings.Contains(why, "and 3 more") {
		t.Fatalf("eight newcomers: %q", why)
	}
	namedIn := 0
	for _, id := range many {
		if strings.Contains(why, id.String()) {
			namedIn++
		}
	}
	if namedIn != 5 {
		t.Fatalf("eight newcomers: %d of them are named, want five: %q", namedIn, why)
	}
}

// A stored migration is read back into the options it was asked with, and one
// that cannot be read whole is refused.
func TestAStoredMigrationCommandReadsBackAsItWasAsked(t *testing.T) {
	src, tgt := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	one := uuid.Must(uuid.NewV7())
	asked := servicecontracts.MigrationOptions{
		Actions:      map[string]servicecontracts.NodeAction{"ops": {Kind: servicecontracts.NodeActionSkip, Reason: "moot"}},
		Acknowledged: []string{"wait"}, Instances: []uuid.UUID{one}, Actor: "somebody else", ActorID: uuid.Must(uuid.NewV7()),
	}
	dita := uuid.Must(uuid.NewV7())
	mapping := map[string]string{"p": "q"}
	doc := migrationCommandDocument(src, tgt, mapping, asked)
	stored, err := migrationCommandFrom(doc)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	source, target, err := stored.versions()
	if err != nil || source != src || target != tgt {
		t.Fatalf("the versions read back as %s → %s (err %v)", source, target, err)
	}
	opts, err := stored.options("dita", dita)
	if err != nil {
		t.Fatalf("its options: %v", err)
	}
	read := servicecontracts.ApplyMigrationOptions(opts)
	if read.Actor != "dita" || read.ActorID != dita || read.Approval != (servicecontracts.MigrationApproval{}) {
		t.Fatalf("the options name %q (account %s) and an approval %+v, want the actor and the account handed in and no approval",
			read.Actor, read.ActorID, read.Approval)
	}
	if migrationFingerprint(source, target, stored.NodeMapping, read, nil) != migrationFingerprint(src, tgt, mapping, asked, nil) {
		t.Fatal("the command read back is another policy than the one stored")
	}
	for name, damage := range map[string]func(map[string]any){
		"a field nothing wrote":     func(d map[string]any) { d["approval"] = map[string]any{"approved_by": "omar"} },
		"no source version":         func(d map[string]any) { d["source_definition_id"] = "" },
		"a target that is no id":    func(d map[string]any) { d["target_definition_id"] = "v2" },
		"an instance that is no id": func(d map[string]any) { d["instances"] = []any{"the first one"} },
		"an action of no known kind": func(d map[string]any) {
			d["node_actions"] = map[string]any{"ops": map[string]any{"kind": "waive", "reason": "moot"}}
		},
	} {
		damaged := migrationCommandDocument(src, tgt, mapping, asked)
		damage(damaged)
		stored, err := migrationCommandFrom(damaged)
		if err == nil {
			if _, _, err = stored.versions(); err == nil {
				_, err = stored.options("dita", dita)
			}
		}
		if err == nil {
			t.Errorf("%s: the stored command was read as though it were whole", name)
		}
	}
}

// What a request says was redirected is what the plan counts as a redirect:
// one question, asked one way. A mapping entry whose key is no step of the
// version being left moves nothing — nothing waits at a step the version
// does not have — and neither is counted nor named; nor is a step mapped to
// itself, nor one renamed where it stands.
func TestARequestNamesOnlyTheRedirectsThePlanCounts(t *testing.T) {
	marked := map[string]any{"compliance_relevant": true}
	source := map[string]models.FlowNode{
		"prepare": {ID: "prepare", Name: "Prepare"}, "sign": {ID: "sign", Name: "Sign"},
		"review": {ID: "review", Name: "Review"}, "control": {ID: "control", Name: "Control", Properties: marked},
	}
	target := map[string]models.FlowNode{
		"sign": source["sign"], "control": source["control"], "check": {ID: "check", Name: "Check"}, "prepare": source["prepare"],
	}
	mapping := map[string]string{
		"prepare":  "sign",    // a redirect
		"review":   "check",   // renamed where it stands
		"sign":     "sign",    // itself
		"nowhere":  "control", // no step of the source
		"archived": "sign",    // no step of the source
	}
	inPlace := map[string]string{"review": "check"}
	waiting := []models.ProcessInstanceModel{{Status: models.ProcessActive, Tokens: []models.Token{{NodeID: "prepare"}}}}

	named := redirectedSteps(source, target, mapping, inPlace)
	if len(named) != 1 || named[0] != "“Prepare” to “Sign”" {
		t.Fatalf("the request names %v as redirected, want the one redirect of a step the version has", named)
	}
	counted := redirectsPastControls(source, target, mapping, inPlace, waiting)
	if len(counted) != len(named) || !strings.Contains(counted[0], "“Prepare” would be redirected to “Sign”") {
		t.Fatalf("the plan counts %v, and the request names %v: they are not the same redirects", counted, named)
	}
	for from, to := range mapping {
		want := from == "prepare"
		if got := redirects(source, inPlace, from, to); got != want {
			t.Errorf("%s → %s is a redirect = %v, want %v", from, to, got, want)
		}
	}
}
