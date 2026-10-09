package impl

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An approval is noted on the entry of what it let through in keys, and in
// keys only: the sentence is its writer's, to the letter, and a waived step's
// must never gain the word "approved". self_approved is said only when it is
// so, never as false.
func TestAnApprovalIsNotedInKeysAndNeverInTheSentence(t *testing.T) {
	request := entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), RequestedBy: "ana", RequestedByID: uuid.Must(uuid.NewV7())}
	locked := models.ProcessInstanceModel{}
	plan := entities.DeviationPlan{NodeID: "approve", NodeName: "Approve"}
	command := entities.DeviationCommand{Reason: "the CFO agreed", Outputs: map[string]any{"approved": true}}
	entry := waiveEntry(locked, plan, command, "ana", uuid.Must(uuid.NewV7()), false)
	before, said := entry.Narrative, len(entry.Data)

	second := approvalNote(entry, request, entities.DeviationDecision{Decider: "budi", DeciderID: uuid.Must(uuid.NewV7())})
	if second.Narrative != before || strings.Contains(second.Narrative, "approved") {
		t.Fatalf("the sentence changed, or says approved: %q", second.Narrative)
	}
	if second.Data["request_id"] != request.ID.String() || second.Data["approved_by"] != "budi" || len(second.Data) != said+2 {
		t.Fatalf("the approval is noted as %v", second.Data)
	}
	if _, has := second.Data["self_approved"]; has {
		t.Fatalf("a second administrator's approval says something of a self-approval: %v", second.Data)
	}
	if len(entry.Data) != said {
		t.Fatal("noting the approval changed the entry it was handed")
	}
	for key, value := range second.Data {
		if value == false {
			t.Errorf("the entry's data says %s is false; an entry says what is so", key)
		}
	}
	alone := approvalNote(entry, request, entities.DeviationDecision{Decider: "ana", DeciderID: request.RequestedByID, SelfApproved: true})
	if alone.Data["self_approved"] != true || alone.Data["approved_by"] != "ana" || alone.Narrative != before {
		t.Fatalf("a self-approval is noted as %v, %q", alone.Data, alone.Narrative)
	}
	// An entry with no data yet is given some, not a panic.
	if bare := approvalNote(entities.AuditEntry{}, request, entities.DeviationDecision{Decider: "budi"}); bare.Data["approved_by"] != "budi" {
		t.Fatalf("an entry with no data: %v", bare.Data)
	}
}

// What an approval adds to a migration's entries, and what the approval's own
// entry says: who the second administrator was — or, in so many words, that
// there was none.
func TestWhoApprovedIsSaidInWords(t *testing.T) {
	request := entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), Kind: entities.DeviationRequestInstanceWaive, RequestedBy: "ana"}
	row := entities.Deviation{ID: uuid.Must(uuid.NewV7()), RunID: uuid.Must(uuid.NewV7()), Origin: entities.DeviationOriginInPlace,
		Project: &entities.Project{ID: uuid.Must(uuid.NewV7())}, Instance: &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())},
		Node: &entities.Node{ID: "approve", Name: "Approve"}}
	second := entities.DeviationDecision{Decider: "budi", Reason: "checked with finance"}
	alone := entities.DeviationDecision{Decider: "ana", Reason: "the board agreed", SelfApproved: true}

	if got, want := approvalSentence(request, second), " A second administrator, budi, approved this migration (request "+request.ID.String()+")."; got != want {
		t.Errorf("a second administrator's approval adds %q, want %q", got, want)
	}
	if got, want := approvalSentence(request, alone), " No second administrator approved this migration: ana approved their own request (request "+request.ID.String()+")."; got != want {
		t.Errorf("a self-approval adds %q, want %q", got, want)
	}

	entry := approvalEntry(request, row, second)
	if entry.Type != EventDeviationApproved || entry.Narrative != "budi approved ana's request to waive “Approve”. Note: checked with finance" {
		t.Errorf("the approval's entry: %s %q", entry.Type, entry.Narrative)
	}
	if silent := approvalEntry(request, row, entities.DeviationDecision{Decider: "budi"}); silent.Narrative != "budi approved ana's request to waive “Approve”." {
		t.Errorf("an approval with no note reads %q", silent.Narrative)
	}
	want := map[string]any{"request_id": request.ID.String(), "requested_by": "ana", "request_kind": "instance_waive",
		"origin": "in_place", "run_id": row.RunID.String(), "node_id": "approve", "approved_by": "budi"}
	if !reflect.DeepEqual(entry.Data, want) {
		t.Errorf("the approval's entry carries %v, want %v", entry.Data, want)
	}
	if entry.Instance != row.Instance || entry.Project != row.Project || entry.Node != row.Node {
		t.Error("the approval's entry is not on the row's instance, project and step")
	}
	self := approvalEntry(request, row, alone)
	if self.Type != EventDeviationSelfApproved || self.Data["self_approved"] != true ||
		self.Narrative != "No second administrator approved this. ana approved their own request to waive “Approve”, "+
			"which this installation allows only while nobody else administers the organization. Reason: the board agreed" {
		t.Errorf("a self-approval's entry: %s %v %q", self.Type, self.Data, self.Narrative)
	}
}

// Why a request is stale is said in a few words: the plan's own refusals when
// the work is what was asked for and the plan now refuses it, and otherwise
// that the instance has moved.
func TestWhyARequestNoLongerHolds(t *testing.T) {
	command := entities.DeviationCommand{VisitKey: "dv1-k"}
	for name, c := range map[string]struct {
		plan entities.DeviationPlan
		want string
	}{
		"another visit":                   {entities.DeviationPlan{VisitKey: "dv1-other"}, movedSinceAsked},
		"another visit that also refuses": {entities.DeviationPlan{VisitKey: "dv1-other", Refusals: []string{"No."}}, movedSinceAsked},
		"the same visit, left the step":   {entities.DeviationPlan{VisitKey: "dv1-k"}, movedSinceAsked},
		"the same visit, now refused": {entities.DeviationPlan{VisitKey: "dv1-k", Refusals: []string{"“Verdict?” has no value to decide on.", "Say why."}},
			"“Verdict?” has no value to decide on. Say why"},
	} {
		if got := whyStale(c.plan, command); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// uowThatCommits is a unit of work that runs its work and reports what
// became of it.
type uowThatCommits struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (u *uowThatCommits) Do(ctx context.Context, fn func(context.Context) error) error {
	if err := fn(ctx); err != nil {
		u.rolledBack = true
		return err
	}
	if u.commitErr != nil {
		u.rolledBack = true
		return u.commitErr
	}
	u.committed = true
	return nil
}

func (u *uowThatCommits) Attempt(ctx context.Context, fn func(context.Context) error) error {
	return u.Do(ctx, fn)
}

func (*uowThatCommits) AfterCommit(context.Context, func()) {}

// A refusal whose record has to be kept is given after the commit; every
// other failure undoes everything; and a commit that fails is the failure,
// not the refusal that would say something was kept.
func TestADecisionKeepsTheRecordOfARefusalAndNothingOfAFailure(t *testing.T) {
	refusal := apierr.Invalidf("This request expired on 5 October 2026 02:30 UTC before anybody approved it, so nothing was applied.")
	failure := errors.New("the database is away")

	kept := &uowThatCommits{}
	if err := runDecision(context.Background(), kept, func(context.Context) error { return refuseAfterCommit(refusal) }); !kept.committed ||
		!errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != refusal.Error() {
		t.Fatalf("a refusal to keep: committed %v, answered %v", kept.committed, err)
	}
	undone := &uowThatCommits{}
	if err := runDecision(context.Background(), undone, func(context.Context) error { return failure }); undone.committed || !errors.Is(err, failure) {
		t.Fatalf("a failure: committed %v, answered %v", undone.committed, err)
	}
	plain := &uowThatCommits{}
	if err := runDecision(context.Background(), plain, func(context.Context) error { return refusal }); plain.committed || err.Error() != refusal.Error() {
		t.Fatalf("a refusal with nothing to keep: committed %v, answered %v; want it undone like any error", plain.committed, err)
	}
	done := &uowThatCommits{}
	if err := runDecision(context.Background(), done, func(context.Context) error { return nil }); !done.committed || err != nil {
		t.Fatalf("work that succeeded: committed %v, answered %v", done.committed, err)
	}
	lost := &uowThatCommits{commitErr: failure}
	if err := runDecision(context.Background(), lost, func(context.Context) error { return refuseAfterCommit(refusal) }); lost.committed ||
		!errors.Is(err, failure) || errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a commit that failed: answered %v, want the failure and not a refusal that says something was kept", err)
	}
	if wrapped := refuseAfterCommit(refusal); !errors.Is(wrapped, apierr.ErrInvalidArgument) || wrapped.Error() != refusal.Error() {
		t.Fatalf("a refusal to keep does not read as the refusal it is: %v", wrapped)
	}
}
