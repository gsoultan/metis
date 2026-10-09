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

	got := redirectsPastControls(source, target, redirect, []models.ProcessInstanceModel{at("prepare"), at("prepare"), at("sign", "prepare", "control", "audit")})
	want := "“Prepare” would be redirected to “Sign” for every listed instance waiting at it when the migration runs — 2 wait(s) there now — " +
		"and a control such an instance has not passed may no longer be ahead of it: “audit”, “Second signature”"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("a redirect past controls:\n  %v\nwant\n  %s", got, want)
	}
	// It is the redirect that is approved, over the instances listed: one that
	// has not reached the step yet is reason enough, with nobody there now.
	if got := redirectsPastControls(source, target, redirect, []models.ProcessInstanceModel{at("draft")}); len(got) != 1 || !strings.Contains(got[0], "— 0 wait(s) there now —") {
		t.Fatalf("a redirect of a step nobody waits at yet: %v", got)
	}
	// Every control passed by every instance that has not ended: nothing to lose.
	if got := redirectsPastControls(source, target, redirect, []models.ProcessInstanceModel{at("prepare", "control", "audit")}); len(got) != 0 {
		t.Fatalf("a redirect once every control has been passed: %v", got)
	}
	// Nothing that has not ended: nobody to move.
	if got := redirectsPastControls(source, target, redirect, nil); len(got) != 0 {
		t.Fatalf("a redirect over a version nothing runs on: %v", got)
	}
	// A rename moves nobody past anything, and neither does a step mapped to
	// itself, nor a mapping from a step the version does not have.
	for name, mapping := range map[string]map[string]string{
		"a rename":                       {"prepare": "draft"},
		"a step mapped to itself":        {"prepare": "prepare"},
		"a step the version has not got": {"review": "sign"},
		"no mapping":                     nil,
	} {
		if got := redirectsPastControls(source, target, mapping, []models.ProcessInstanceModel{at("prepare")}); len(got) != 0 {
			t.Errorf("%s: %v, want it to ask nobody", name, got)
		}
	}
	// A process with no control asks nobody, whatever is redirected.
	plain := map[string]models.FlowNode{"prepare": source["prepare"], "sign": source["sign"]}
	if got := redirectsPastControls(plain, plain, redirect, []models.ProcessInstanceModel{at("prepare")}); len(got) != 0 {
		t.Fatalf("a redirect in a process with no control: %v", got)
	}
	// Many controls are counted, and five of them named.
	many := map[string]models.FlowNode{"prepare": source["prepare"], "sign": source["sign"]}
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7"} {
		many[id] = models.FlowNode{ID: id, Properties: marked}
	}
	if got := redirectsPastControls(many, many, redirect, []models.ProcessInstanceModel{at("prepare")}); len(got) != 1 ||
		!strings.HasSuffix(got[0], "ahead of it: “c1”, “c2”, “c3”, “c4”, “c5”, and 2 more") {
		t.Fatalf("a redirect past seven controls: %v", got)
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
		Acknowledged: []string{"wait"}, Instances: []uuid.UUID{one}, Actor: "somebody else",
	}
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
	opts, err := stored.options("dita")
	if err != nil {
		t.Fatalf("its options: %v", err)
	}
	read := servicecontracts.ApplyMigrationOptions(opts)
	if read.Actor != "dita" || read.Approval != (servicecontracts.MigrationApproval{}) {
		t.Fatalf("the options name %q and an approval %+v, want the actor handed in and no approval", read.Actor, read.Approval)
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
				_, err = stored.options("dita")
			}
		}
		if err == nil {
			t.Errorf("%s: the stored command was read as though it were whole", name)
		}
	}
}
