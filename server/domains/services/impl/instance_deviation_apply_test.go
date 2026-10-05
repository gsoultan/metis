package impl

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// rowWaitingAt is an instance's row as a lock returns it: running, with a
// token on each of the steps named.
func rowWaitingAt(status models.ProcessStatus, nodeIDs ...string) models.ProcessInstanceModel {
	row := models.ProcessInstanceModel{Status: status}
	row.ID = models.UUID(uuid.Must(uuid.NewV7()))
	for _, nodeID := range nodeIDs {
		row.Tokens = append(row.Tokens, models.Token{ID: models.UUID(uuid.Must(uuid.NewV7())), NodeID: nodeID})
	}
	return row
}

// The questions an apply puts to the row it locked, after its plan is made
// again from that row. They are asked here of rows and plans made by hand,
// because the last of them repeats what a plan refuses: through the service
// it can only be reached by a plan that has stopped refusing it.
func TestAnApplyIsRefusedUnlessItIsThePlanThatWasPreviewed(t *testing.T) {
	const key = "dv1-the-key-the-preview-answered-000"
	waive := entities.DeviationCommand{Kind: entities.DeviationWaive, NodeID: "approve", VisitKey: key}
	cancelNowhere := entities.DeviationCommand{Kind: entities.DeviationCancel, VisitKey: key}
	applicable := entities.DeviationPlan{VisitKey: key}
	refusing := entities.DeviationPlan{VisitKey: key, Refusals: []string{"Say why: a reason is required.", "“Approve” has 2 ways out."}}
	const moved = "this instance has moved since you previewed it; preview again"

	for name, tc := range map[string]struct {
		locked  models.ProcessInstanceModel
		plan    entities.DeviationPlan
		command entities.DeviationCommand
		want    string
	}{
		"the plan that was previewed, on the step it was previewed at": {
			locked: rowWaitingAt(models.ProcessActive, "approve"), plan: applicable, command: waive},
		"a step reached twice at once still waits there": {
			locked: rowWaitingAt(models.ProcessActive, "approve", "approve"), plan: applicable, command: waive},
		"another plan than the one previewed": {
			locked: rowWaitingAt(models.ProcessActive, "approve"), plan: entities.DeviationPlan{VisitKey: "dv1-another"}, command: waive, want: moved},
		"another plan, which refuses as well: the caller hears that the work changed": {
			locked: rowWaitingAt(models.ProcessActive), plan: entities.DeviationPlan{VisitKey: "dv1-another", Refusals: refusing.Refusals},
			command: waive, want: moved},
		"the plan that was previewed, which refuses": {
			locked: rowWaitingAt(models.ProcessActive, "approve"), plan: refusing, command: waive,
			want: "Say why: a reason is required. “Approve” has 2 ways out."},
		"a plan that accepts a step the locked row does not wait at": {
			locked: rowWaitingAt(models.ProcessActive, "record"), plan: applicable, command: waive, want: moved},
		"a plan that accepts a step on a row holding no token": {
			locked: rowWaitingAt(models.ProcessActive), plan: applicable, command: waive, want: moved},
		"a cancel of no step, on a row that waits nowhere": {
			locked: rowWaitingAt(models.ProcessActive), plan: applicable, command: cancelNowhere},
		"a cancel of no step, on a row that waits somewhere": {
			locked: rowWaitingAt(models.ProcessActive, "approve"), plan: applicable, command: cancelNowhere, want: moved},
	} {
		err := refuseUnlessAsPreviewed(tc.locked, tc.plan, tc.command)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: refused with %v", name, err)
		case tc.want != "" && (!errors.Is(err, apierr.ErrInvalidArgument) || !strings.HasSuffix(err.Error(), tc.want)):
			t.Errorf("%s: got %v, want it refused as the caller's to fix, saying %q", name, err, tc.want)
		}
	}
}

func TestAnInstanceThatHasEndedIsRefusedFromTheRowTheApplyLocked(t *testing.T) {
	if err := refuseEnded(rowWaitingAt(models.ProcessActive, "approve"), entities.DeviationWaive); err != nil {
		t.Fatalf("a running instance: %v", err)
	}
	for status, kind := range map[models.ProcessStatus]entities.DeviationKind{
		models.ProcessCompleted: entities.DeviationWaive,
		models.ProcessCancelled: entities.DeviationCancel,
		models.ProcessFailed:    entities.DeviationHold,
	} {
		// The token is still on the step: it is the status that refuses.
		err := refuseEnded(rowWaitingAt(status, "approve"), kind)
		want := "this instance is " + string(status) + ", so it can no longer be " + pastTense(kind) + "; preview again"
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("an instance that is %s: got %v, want %q", status, err, want)
		}
	}
	// A suspended instance has not ended, so it is not said that it can no
	// longer be acted on. Nor is it said to resume it first: nothing in the
	// product suspends an instance or resumes one. It is said what it is, and
	// that it is refused, in one sentence for all three kinds.
	const want = "this instance is suspended, and a suspended instance is not waived, cancelled or held in place"
	for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationHold, entities.DeviationWaive} {
		err := refuseEnded(rowWaitingAt(models.ProcessSuspended, "approve"), kind)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != "invalid argument: "+want {
			t.Errorf("a %s of an instance that is suspended: got %v, want %q", kind, err, want)
		}
	}
}

// The act is handed the locked row as the engine reads it, and the engine
// refuses to end a step that row holds no token on. So "still waits at the
// step" has to mean, of the row as the store holds it, exactly what a
// migration's skip asks of the row as the engine reads it (parkedOn).
func TestWaitingWhereACommandActsIsWhatASkipAsksOfTheRowItLocked(t *testing.T) {
	for name, locked := range map[string]models.ProcessInstanceModel{
		"on the step":                  rowWaitingAt(models.ProcessActive, "approve"),
		"on the step twice":            rowWaitingAt(models.ProcessActive, "approve", "approve"),
		"on the step and on another":   rowWaitingAt(models.ProcessActive, "record", "approve"),
		"on another step":              rowWaitingAt(models.ProcessActive, "record"),
		"nowhere":                      rowWaitingAt(models.ProcessActive),
		"on a step whose id is a part": rowWaitingAt(models.ProcessActive, "approve-again"),
	} {
		command := entities.DeviationCommand{Kind: entities.DeviationWaive, NodeID: "approve"}
		live := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
		if got, want := waitsWhereItActs(locked, command), parkedOn(live, "approve"); got != want {
			t.Errorf("%s: the apply says the instance waits at the step: %v; a skip says %v", name, got, want)
		}
	}
}

func deviationRow(kind entities.DeviationKind, scope entities.DeviationScope, nodeID, reason string, set map[string]any) entities.Deviation {
	row := entities.Deviation{ID: uuid.Must(uuid.NewV7()), Kind: kind, Scope: scope, Reason: reason, Actor: "ana",
		Before: map[string]any{}, After: map[string]any{}, Details: map[string]any{}}
	if nodeID != "" {
		row.Node = &entities.Node{ID: nodeID, Name: "Approve"}
	}
	if set != nil {
		row.After["variables"] = set
	}
	return row
}

// A retry is the same request: the same act on the same step, for the same
// reason, counting as the same values — compared as the values the record
// keeps, not as the types that carried them.
func TestARequestIsARetryWhenItAsksForWhatTheRecordSays(t *testing.T) {
	// As a row is read back: numbers as float64.
	recorded := deviationRow(entities.DeviationWaive, entities.DeviationScopeTask, "approve", "the CFO agreed",
		map[string]any{"approved": true, "amount": float64(900), "lines": []any{float64(1), "two"}, "who": map[string]any{"b": float64(2), "a": "x"}})
	same := entities.DeviationCommand{Kind: entities.DeviationWaive, NodeID: "approve", Reason: "the CFO agreed",
		Outputs: map[string]any{"approved": true, "amount": 900, "lines": []any{1, "two"}, "who": map[string]any{"a": "x", "b": 2}}}

	with := func(change func(*entities.DeviationCommand)) entities.DeviationCommand {
		command := same
		command.Outputs = map[string]any{}
		for name, value := range same.Outputs {
			command.Outputs[name] = value
		}
		change(&command)
		return command
	}
	for name, tc := range map[string]struct {
		command entities.DeviationCommand
		same    bool
	}{
		"the same request":           {same, true},
		"the reason with spaces":     {with(func(c *entities.DeviationCommand) { c.Reason = "  the CFO agreed\n" }), true},
		"a number as a float":        {with(func(c *entities.DeviationCommand) { c.Outputs["amount"] = 900.0 }), true},
		"a number as a decoder's":    {with(func(c *entities.DeviationCommand) { c.Outputs["amount"] = json.Number("900") }), true},
		"a number written 900.0":     {with(func(c *entities.DeviationCommand) { c.Outputs["amount"] = json.Number("900.0") }), true},
		"another reason":             {with(func(c *entities.DeviationCommand) { c.Reason = "the CEO agreed" }), false},
		"another kind":               {with(func(c *entities.DeviationCommand) { c.Kind = entities.DeviationHold }), false},
		"another step":               {with(func(c *entities.DeviationCommand) { c.NodeID = "record" }), false},
		"no step":                    {with(func(c *entities.DeviationCommand) { c.NodeID = "" }), false},
		"another value":              {with(func(c *entities.DeviationCommand) { c.Outputs["amount"] = 901 }), false},
		"a number as text":           {with(func(c *entities.DeviationCommand) { c.Outputs["amount"] = "900" }), false},
		"a value fewer":              {with(func(c *entities.DeviationCommand) { delete(c.Outputs, "approved") }), false},
		"a value more":               {with(func(c *entities.DeviationCommand) { c.Outputs["extra"] = 1 }), false},
		"a list in another order":    {with(func(c *entities.DeviationCommand) { c.Outputs["lines"] = []any{"two", 1} }), false},
		"a value inside changed":     {with(func(c *entities.DeviationCommand) { c.Outputs["who"] = map[string]any{"a": "y", "b": 2} }), false},
		"no values where some were":  {with(func(c *entities.DeviationCommand) { c.Outputs = nil }), false},
		"the reason in another case": {with(func(c *entities.DeviationCommand) { c.Reason = "The CFO agreed" }), false},
	} {
		got, err := sameRequest(recorded, tc.command)
		if err != nil || got != tc.same {
			t.Errorf("%s: the same request %v (%v), want %v", name, got, err, tc.same)
		}
	}

	// A row that set nothing and names no step, as a cancel of an instance
	// waiting nowhere writes: no values is no values however it is spelled,
	// and no step matches no step.
	nothingSet := deviationRow(entities.DeviationCancel, entities.DeviationScopeInstance, "", "nothing left", nil)
	for name, outputs := range map[string]map[string]any{"nil": nil, "empty": {}} {
		command := entities.DeviationCommand{Kind: entities.DeviationCancel, Reason: "nothing left", Outputs: outputs}
		if got, err := sameRequest(nothingSet, command); err != nil || !got {
			t.Errorf("no values given as %s against a row that set none: %v (%v), want the same request", name, got, err)
		}
	}
	named := entities.DeviationCommand{Kind: entities.DeviationCancel, NodeID: "approve", Reason: "nothing left"}
	if got, _ := sameRequest(nothingSet, named); got {
		t.Error("a cancel that names a step is taken for the retry of one that named none")
	}

	// A record whose values cannot be read is the server's trouble: it is
	// neither taken for the same request nor blamed on the caller.
	unreadable := deviationRow(entities.DeviationWaive, entities.DeviationScopeTask, "approve", "the CFO agreed", nil)
	unreadable.After["variables"] = "approved"
	if got, err := sameRequest(unreadable, same); got || err == nil || errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("a record whose values are not values by name: the same request %v, %v; want a plain error", got, err)
	}
}

func TestAVisitAlreadyActedOnSaysWhatWasDoneAndByWhom(t *testing.T) {
	for want, row := range map[string]entities.Deviation{
		"this step was already waived by ana":        deviationRow(entities.DeviationWaive, entities.DeviationScopeTask, "approve", "why", nil),
		"this instance was already cancelled by ana": deviationRow(entities.DeviationCancel, entities.DeviationScopeInstance, "", "why", nil),
		"this instance was already held by ana":      deviationRow(entities.DeviationHold, entities.DeviationScopeInstance, "approve", "why", nil),
	} {
		err := alreadyActedOn(row)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("got %v, want it refused as the caller's to fix, saying %q", err, want)
		}
	}
}

// What the record keeps of how things were: the values the instance held
// under the names the waiver sets, and nothing for a name it held nothing
// under.
func TestTheRecordOfAWaiveKeepsOnlyValuesTheInstanceHeld(t *testing.T) {
	held := valuesHeld(
		map[string]any{"approved": false, "amount": 900, "nothing": nil},
		map[string]any{"approved": true, "nothing": 1, "new": "x"})
	if len(held) != 2 || held["approved"] != false {
		t.Fatalf("held %v, want approved as it was and the name that held nil", held)
	}
	if value, has := held["nothing"]; !has || value != nil {
		t.Errorf("a name the instance held nil under is %v (kept %v); it was held, as nothing", value, has)
	}
	if _, has := held["new"]; has {
		t.Error("a name the instance did not hold is recorded as having been held")
	}
	if _, has := held["amount"]; has {
		t.Error("a value the waiver does not set is recorded")
	}
}

// What somebody who asked for a waive is told when what follows the step
// fails. A gateway with no way out is told as a refusal, by name, in a
// sentence that says where the gateway is and that nothing was changed; the
// whole of what the caller reads is pinned here, word for word. Everything
// else is the server's: the words are kept, and whatever class the failure
// carried — not found, invalid, forbidden — is not, since the instance
// exists, the request was well formed and the caller may make it.
func TestAWaiveThatFailsIsToldAsWhatItIs(t *testing.T) {
	instance := uuid.Must(uuid.NewV7())
	caller := uuid.Must(uuid.NewV7())
	other := uuid.Must(uuid.NewV7())
	gateway := func(of uuid.UUID, name string) error {
		return &entities.NoFlowSelectedError{GatewayKind: entities.GatewayKindExclusive, GatewayID: "decide", GatewayName: name, InstanceID: of}
	}
	asTheEffectWraps := func(err error) error {
		return fmt.Errorf("advancing instance %s past %q: %w", instance, "review", err)
	}
	asAResumedCallerWraps := func(err error) error {
		return asTheEffectWraps(fmt.Errorf("resume parent instance %s at call activity %q: %w", caller, "check", err))
	}
	// As a signal or a message delivered to several instances reports what
	// failed: each failure wrapped, all of them joined.
	asABroadcastJoins := func(failures ...error) error {
		wrapped := make([]error, len(failures))
		for i, failure := range failures {
			wrapped[i] = fmt.Errorf("trigger signal subscription %d: %w", i, failure)
		}
		return asTheEffectWraps(errors.Join(wrapped...))
	}
	const unchanged = "so the waive was not applied and nothing was changed."
	const given, none = true, false

	for name, tc := range map[string]struct {
		failure     error
		gaveOutputs bool
		want        string
	}{
		"a gateway of this instance, with values given": {asTheEffectWraps(gateway(instance, "Verdict?")), given,
			"The values given fit no way out of “Verdict?”, " + unchanged + " Preview again and give a value one of its branches accepts."},
		"a gateway nobody named": {asTheEffectWraps(gateway(instance, "")), given,
			"The values given fit no way out of “decide”, " + unchanged + " Preview again and give a value one of its branches accepts."},
		"a gateway with a very long name": {asTheEffectWraps(gateway(instance, strings.Repeat("é", 300))), given,
			"The values given fit no way out of “" + strings.Repeat("é", deviationNodeNameLength) + "”, " + unchanged +
				" Preview again and give a value one of its branches accepts."},
		// Nothing was given, so nothing given can be said to fit badly.
		"a gateway of this instance, with no values given": {asTheEffectWraps(gateway(instance, "Verdict?")), none,
			"“Verdict?” had no way out for the values this instance holds, " + unchanged},
		"a gateway of the process that called this one": {asAResumedCallerWraps(gateway(caller, "Supplier approved?")), given,
			"“Supplier approved?”, in the process that started this one, had no way out for the result, " + unchanged},
		"a caller's gateway nobody named": {asAResumedCallerWraps(gateway(caller, "")), none,
			"“decide”, in the process that started this one, had no way out for the result, " + unchanged},
		// Neither this instance nor the one that started it: a process the
		// advance went on to start or to wake, or a caller further up. It is
		// not said to be the caller's, and nobody is told to supply a value.
		"a gateway of some other instance the advance reached": {asTheEffectWraps(gateway(other, "Stock in hand?")), given,
			"“Stock in hand?”, in another process this waive reached, had no way out, " + unchanged},
		"the one failure of a broadcast": {asABroadcastJoins(gateway(other, "Stock in hand?")), given,
			"“Stock in hand?”, in another process this waive reached, had no way out, " + unchanged},
		// Every part of what failed is a gateway with no way out: the first is named.
		"two gateways, of two instances a broadcast woke": {asABroadcastJoins(gateway(other, "Stock in hand?"), gateway(caller, "Supplier approved?")), given,
			"“Stock in hand?”, in another process this waive reached, had no way out, " + unchanged},
	} {
		err := waiveFailed(instance, caller, "Review the claim", tc.gaveOutputs, tc.failure)
		if want := apierr.Invalidf("%s", tc.want); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
			t.Errorf("%s: got\n  %v\nwant it refused as the caller's to fix, saying exactly\n  %v", name, err, want)
		}
	}

	notFound := fmt.Errorf("%w: no live version of decision no-such-decision", apierr.ErrNotFound)
	for name, failure := range map[string]error{
		"a decision nobody stored":  asTheEffectWraps(notFound),
		"the engine refusing":       asTheEffectWraps(apierr.Invalidf("this step has already finished")),
		"something forbidden":       asTheEffectWraps(apierr.Forbiddenf("not for you")),
		"a definition that loops":   asTheEffectWraps(errors.New("BPMN_ERROR:execution exceeded 1000 nodes at \"again\"")),
		"the database":              asTheEffectWraps(errors.New("could not update the process instance")),
		"an error thrown by a step": asTheEffectWraps(errors.New("BPMN_ERROR:charge-failed")),
		// A failure of the server's anywhere in the advance outranks a
		// gateway's: what failed is not only something a value could fix, and
		// a refusal would tell the caller that it was.
		"a gateway beside a failure of the server's":     asABroadcastJoins(gateway(other, "Stock in hand?"), notFound),
		"a failure of the server's beside a gateway":     asABroadcastJoins(errors.New("lock instance x: connection reset"), gateway(instance, "Verdict?")),
		"a gateway and a failure wrapped into one error": asTheEffectWraps(fmt.Errorf("%w, and then %w", gateway(instance, "Verdict?"), notFound)),
		"a join, inside a join, holding one other failure": asABroadcastJoins(gateway(other, "Stock in hand?"),
			errors.Join(gateway(caller, "Supplier approved?"), errors.New("delete subscription y: connection reset"))),
	} {
		err := waiveFailed(instance, caller, "Review the claim", given, failure)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		for class, kind := range map[string]error{"not found": apierr.ErrNotFound, "an invalid argument": apierr.ErrInvalidArgument, "forbidden": apierr.ErrForbidden} {
			if errors.Is(err, kind) {
				t.Errorf("%s is answered as %s: %v", name, class, err)
			}
		}
		if want := "waiving “Review the claim”: " + failure.Error(); err.Error() != want {
			t.Errorf("%s: told as\n  %s\nwant the words kept:\n  %s", name, err, want)
		}
	}
}

// The same stripping, for an act that advances nothing: what Task 7's cancel
// and hold are told by.
func TestAnEffectThatFailsAnswersAsTheServers(t *testing.T) {
	failure := fmt.Errorf("reading the incidents already on instance x: %w", apierr.ErrNotFound)
	err := effectFailed("holding this instance", failure)
	if errors.Is(err, apierr.ErrNotFound) || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a failed effect is answered with a class: %v", err)
	}
	if want := "holding this instance: " + failure.Error(); err.Error() != want {
		t.Errorf("told as %q, want %q", err, want)
	}
}

// An instance nothing started has no caller: a gateway of another instance is
// then never said to be "in the process that started this one".
func TestAWaiveWithNoCallerNeverBlamesOne(t *testing.T) {
	instance, other := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	const elsewhere = "“decide”, in another process this waive reached, had no way out, so the waive was not applied and nothing was changed."
	failure := &entities.NoFlowSelectedError{GatewayKind: entities.GatewayKindExclusive, GatewayID: "decide", InstanceID: other}
	if err := waiveFailed(instance, uuid.Nil, "Review the claim", true, failure); err.Error() != apierr.Invalidf("%s", elsewhere).Error() {
		t.Errorf("got %v, want it refused without naming a caller: %s", err, elsewhere)
	}
	// And a gateway whose error names no instance is not taken for this one's.
	unnamed := &entities.NoFlowSelectedError{GatewayKind: entities.GatewayKindExclusive, GatewayID: "decide"}
	if err := waiveFailed(instance, uuid.Nil, "Review the claim", true, unnamed); err.Error() != apierr.Invalidf("%s", elsewhere).Error() {
		t.Errorf("a gateway of no known instance: %v; it is neither this instance's nor its caller's", err)
	}
}

// What a plan says of a caller it did not read: that the caller decides on a
// value it already holds when the waiver gives none, and undoes the waive
// when it holds none. The step that called is named when it is known.
func TestTheWarningOfACallerThatWasNotRead(t *testing.T) {
	const rest = ", which receives its results when it ends and was not read. " +
		"Where that process decides on a value this step would have set and you give none, " +
		"it decides on the value it already holds, or undoes the waive if it holds none. Check that process before applying."
	for name, tc := range map[string]struct{ process, step, want string }{
		"the process and the step that called": {"Supplier onboarding", "Check the supplier",
			"This instance was started by “Supplier onboarding” at “Check the supplier”" + rest},
		"a caller whose calling step is not known": {"Supplier onboarding", "",
			"This instance was started by “Supplier onboarding”" + rest},
	} {
		if got := callerWasNotRead(tc.process, tc.step); got != tc.want {
			t.Errorf("%s:\n  %s\nwant\n  %s", name, got, tc.want)
		}
	}
}

// A name longer than a plan lists a name at cannot be set by a waive, given
// or missing: a request that gives one is malformed, and is told which.
func TestAnOutputWithANameTooLongToSetIsAMalformedRequest(t *testing.T) {
	command := func(name string) entities.DeviationCommand {
		return entities.DeviationCommand{Kind: entities.DeviationWaive, NodeID: "approve", DryRun: true, Outputs: map[string]any{name: true}}
	}
	fits := strings.Repeat("é", deviationNodeNameLength)
	if _, err := normalizedDeviationCommand(command(fits)); err != nil {
		t.Errorf("a name of %d characters, which a plan lists whole: %v", deviationNodeNameLength, err)
	}
	tooLong := strings.Repeat("ü", deviationNodeNameLength+1)
	_, err := normalizedDeviationCommand(command(tooLong))
	want := apierr.Invalidf("“%s…” is too long a name for a waive to set: a field's name is at most %d characters",
		strings.Repeat("ü", maxNameShown), deviationNodeNameLength)
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Errorf("a name one character longer: got\n  %v\nwant\n  %v", err, want)
	}
	// A cancel that carries outputs is told that first, whatever they are called.
	hold := command(tooLong)
	hold.Kind = entities.DeviationHold
	if _, err := normalizedDeviationCommand(hold); err == nil || strings.Contains(err.Error(), "too long a name") {
		t.Errorf("a hold carrying an output with a long name: %v, want it told a hold sets none", err)
	}
}
