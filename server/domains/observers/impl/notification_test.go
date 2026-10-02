package impl

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// Being told a task is yours.
//
// The observer read the assignee out of event.Variables, which is the
// instance's business data. Only two events put an "assignee" key there — the
// ones built by hand when somebody claims a task or hands one over — so the
// case that matters most, a process creating a task already assigned to you,
// told nobody.

type recordingNotifier struct {
	sent []entities.Notification
}

func (r *recordingNotifier) Send(_ context.Context, n entities.Notification) error {
	r.sent = append(r.sent, n)
	return nil
}

func (r *recordingNotifier) ListByUser(context.Context, string) ([]entities.Notification, error) {
	return nil, nil
}
func (r *recordingNotifier) MarkAsRead(context.Context, uuid.UUID, string) error { return nil }
func (r *recordingNotifier) MarkAllAsRead(context.Context, string) error         { return nil }
func (r *recordingNotifier) Delete(context.Context, uuid.UUID, string) error     { return nil }

func (r *recordingNotifier) CountUnreadByUser(context.Context, string) (int64, error) { return 0, nil }
func (r *recordingNotifier) ListByUserPaged(_ context.Context, _ string, p repocontracts.Pagination) (repocontracts.Page[entities.Notification], error) {
	return repocontracts.NewPage([]entities.Notification{}, 0, p), nil
}

func (r *recordingNotifier) recipients() []string {
	out := make([]string, 0, len(r.sent))
	for _, n := range r.sent {
		if n.User != nil {
			out = append(out, n.User.Username)
		}
	}
	return out
}

func taskCreated(node *entities.Node) entities.ProcessEvent {
	return entities.ProcessEvent{
		Type:    entities.EventTaskCreated,
		Project: &entities.Project{ID: uuid.New()},
		Instance: &entities.ProcessInstance{
			ID:         uuid.New(),
			Definition: &entities.ProcessDefinition{Key: "quotation", Name: "Quotation approval"},
		},
		Node: node,
		// What a real EventTaskCreated carries: the instance's own variables,
		// which is the business data and says nothing about who does the work.
		Variables: map[string]any{"amount": 9000, "customer": "Acme"},
	}
}

func TestTheAssigneeIsToldWhenTheProcessCreatesTheirTask(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	observer.OnEvent(context.Background(), taskCreated(&entities.Node{
		ID: "opsApprove", Name: "Operations approve", Assignee: "ollie",
	}))

	if got := notifier.recipients(); len(got) != 1 || got[0] != "ollie" {
		t.Fatalf("the assignee was told %v; a task created for ollie should tell ollie", got)
	}
}

func TestEverybodyWhoCouldPickItUpIsTold(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	observer.OnEvent(context.Background(), taskCreated(&entities.Node{
		ID: "review", Name: "Review",
		CandidateUsers: []*entities.User{
			{Username: "ada"},
			{Username: "bo"},
			// A duplicate and an empty one: neither should produce a
			// notification of its own.
			{Username: "ada"},
			{Username: ""},
			nil,
		},
	}))

	if got := notifier.recipients(); len(got) != 2 || got[0] != "ada" || got[1] != "bo" {
		t.Fatalf("an unassigned task told %v; everybody who could take it should hear once", got)
	}
}

func TestAHandedOverTaskTellsThePersonItWasHandedTo(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	// Assigning builds the event by hand and names the recipient, who is not
	// whoever the diagram nominated. The named one wins.
	event := taskCreated(&entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"})
	event.Type = entities.EventTaskClaimed
	event.Variables = map[string]any{"assignee": "dita"}
	observer.OnEvent(context.Background(), event)

	if got := notifier.recipients(); len(got) != 1 || got[0] != "dita" {
		t.Fatalf("a task handed to dita told %v", got)
	}
}

func TestATaskNobodyCanBeFoundForTellsNobody(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	observer.OnEvent(context.Background(), taskCreated(&entities.Node{ID: "review", Name: "Review"}))

	if got := notifier.recipients(); len(got) != 0 {
		t.Fatalf("a task with no assignee and no candidates told %v", got)
	}
}

func TestTheMessageNamesTheWorkRatherThanQuotingAnId(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	event := taskCreated(&entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"})
	observer.OnEvent(context.Background(), event)

	if len(notifier.sent) != 1 {
		t.Fatalf("expected one notification, got %d", len(notifier.sent))
	}
	sent := notifier.sent[0]

	if !strings.Contains(sent.Message, "Operations approve") {
		t.Errorf("the message does not name the step: %q", sent.Message)
	}
	if !strings.Contains(sent.Message, "Quotation approval") {
		t.Errorf("the message does not name the process: %q", sent.Message)
	}
	// The instance id used to be interpolated into the sentence, so somebody
	// opening their notifications read a raw uuid mid-sentence.
	if strings.Contains(sent.Message, event.Instance.ID.String()) {
		t.Errorf("the message quotes the instance id at a person: %q", sent.Message)
	}
	// The link needs to identify one running instance. A node id does not:
	// it is unique within a definition, not across the instances running it.
	if !strings.Contains(sent.Link, event.Instance.ID.String()) {
		t.Errorf("the link does not identify the instance: %q", sent.Link)
	}
}

func TestAnEventWithNoInstanceIsIgnored(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	observer.OnEvent(context.Background(), entities.ProcessEvent{
		Type:    entities.EventTaskCreated,
		Project: &entities.Project{ID: uuid.New()},
		Node:    &entities.Node{ID: "x", Assignee: "ollie"},
	})

	if len(notifier.sent) != 0 {
		t.Fatalf("an event with no instance produced %d notifications", len(notifier.sent))
	}
}

// Being told work was taken away.
//
// The observer handled a task arriving and a task being claimed. A task
// *withdrawn* — a boundary timer firing, or a migration skipping the step —
// simply vanished from somebody's inbox, which from their side is
// indistinguishable from a colleague completing it, or from a bug.

func taskCancelled(node *entities.Node, heldBy string) entities.ProcessEvent {
	event := taskCreated(node)
	event.Type = entities.EventTaskCanceled
	event.Assignee = heldBy
	return event
}

func TestWhoeverHeldAWithdrawnTaskIsTold(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	// Claimed by dita, whatever the diagram nominated. She is the one who has
	// it open and the only one who needs telling.
	observer.OnEvent(context.Background(), taskCancelled(
		&entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"}, "dita"))

	if got := notifier.recipients(); len(got) != 1 || got[0] != "dita" {
		t.Fatalf("a withdrawn task told %v; dita was holding it", got)
	}
	if title := notifier.sent[0].Title; title != "A task was withdrawn" {
		t.Errorf("the notification is titled %q, which reads like work arriving", title)
	}
	if msg := notifier.sent[0].Message; !strings.Contains(msg, "no longer needed") {
		t.Errorf("the message does not say the work is gone: %q", msg)
	}
}

func TestAWithdrawnTaskNobodyHadTellsTheQueue(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	// Nobody had claimed it, so everybody who could have is told it is gone —
	// otherwise it is simply absent from a queue they were watching.
	observer.OnEvent(context.Background(), taskCancelled(&entities.Node{
		ID: "review", Name: "Review",
		CandidateUsers: []*entities.User{{Username: "ada"}, {Username: "bo"}},
	}, ""))

	if got := notifier.recipients(); len(got) != 2 {
		t.Fatalf("an unclaimed withdrawn task told %v", got)
	}
}

func TestTheExplicitAssigneeWinsOverTheOldVariablesRoute(t *testing.T) {
	notifier := &recordingNotifier{}
	observer := NewNotificationObserver(notifier)

	// Both present and disagreeing: the field is the one that means "this event
	// is about this person", and Variables is business data that happened to
	// have a key of the same name.
	event := taskCancelled(&entities.Node{ID: "opsApprove", Name: "Operations approve"}, "dita")
	event.Variables = map[string]any{"assignee": "someone-elses-process-variable"}
	observer.OnEvent(context.Background(), event)

	if got := notifier.recipients(); len(got) != 1 || got[0] != "dita" {
		t.Fatalf("told %v; the event's own assignee field should win", got)
	}
}

// handedTo is the event a hand-over raises: about the person it names, whatever
// the diagram nominated.
func handedTo(eventType, who string) entities.ProcessEvent {
	event := taskCreated(&entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"})
	event.Type = eventType
	event.Assignee = who
	return event
}

// A delegation was raised as TaskUpdated, which nothing here listens for: the
// work arrived in the delegate's inbox and nobody told them.
func TestADelegateIsToldTheTaskIsWithThem(t *testing.T) {
	notifier := &recordingNotifier{}
	NewNotificationObserver(notifier).OnEvent(context.Background(), handedTo(entities.EventTaskDelegated, "dita"))

	if got := notifier.recipients(); len(got) != 1 || got[0] != "dita" {
		t.Fatalf("a task delegated to dita told %v", got)
	}
	if title := notifier.sent[0].Title; title != "A task was delegated to you" {
		t.Errorf("the notification is titled %q", title)
	}
	if msg := notifier.sent[0].Message; !strings.Contains(msg, `"Operations approve"`) || !strings.Contains(msg, "Quotation approval") || !strings.Contains(msg, "Hand it back") {
		t.Errorf("the message does not name the work, the process and what to do with it: %q", msg)
	}
}

func TestAnOwnerIsToldTheirTaskIsBack(t *testing.T) {
	notifier := &recordingNotifier{}
	NewNotificationObserver(notifier).OnEvent(context.Background(), handedTo(entities.EventTaskResolved, "ollie"))

	if got := notifier.recipients(); len(got) != 1 || got[0] != "ollie" {
		t.Fatalf("a task handed back to ollie told %v", got)
	}
	if title := notifier.sent[0].Title; title != "A task was handed back to you" {
		t.Errorf("the notification is titled %q", title)
	}
	if msg := notifier.sent[0].Message; !strings.Contains(msg, `"Operations approve"`) || !strings.Contains(msg, "yours to complete") {
		t.Errorf("the message does not say the work is theirs to finish: %q", msg)
	}
}

// The pin for the wording function this change rewrites: the two sentences it
// already wrote, with a process to name and without one, word for word.
func TestTheWaitingAndWithdrawnSentencesAreWhatTheyWere(t *testing.T) {
	named := &entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"}
	for _, c := range []struct {
		name  string
		event entities.ProcessEvent
		want  string
	}{
		{"waiting", taskCreated(named), `"Operations approve" is waiting for you in Quotation approval.`},
		{"waiting, no process", withoutProcess(taskCreated(named)), `"Operations approve" is waiting for you.`},
		{"waiting, no step name", taskCreated(&entities.Node{ID: "opsApprove", Assignee: "ollie"}), `A task is waiting for you in Quotation approval.`},
		{"claimed", handedTo(entities.EventTaskClaimed, "dita"), `"Operations approve" is waiting for you in Quotation approval.`},
		{"withdrawn", taskCancelled(named, "dita"), `"Operations approve" in Quotation approval is no longer needed and has been taken off your list.`},
		{"withdrawn, no process", withoutProcess(taskCancelled(named, "dita")), `"Operations approve" is no longer needed and has been taken off your list.`},
	} {
		t.Run(c.name, func(t *testing.T) {
			notifier := &recordingNotifier{}
			NewNotificationObserver(notifier).OnEvent(context.Background(), c.event)
			if len(notifier.sent) != 1 || notifier.sent[0].Message != c.want {
				t.Fatalf("sent %+v; want one notification reading %q", notifier.sent, c.want)
			}
		})
	}
}

// withoutProcess is the event with nothing to call its process by.
func withoutProcess(event entities.ProcessEvent) entities.ProcessEvent {
	event.Instance = &entities.ProcessInstance{ID: event.Instance.ID}
	return event
}

// A withdrawn task that was with a delegate had somebody else waiting on it:
// its owner, who was told nothing and went on waiting for it to come back.
func TestTheOwnerOfAWithdrawnDelegationIsToldWhatTheDelegateIs(t *testing.T) {
	notifier := &recordingNotifier{}
	event := taskCancelled(&entities.Node{ID: "opsApprove", Name: "Operations approve", Assignee: "ollie"}, "dita")
	event.Owner = "ollie"
	NewNotificationObserver(notifier).OnEvent(context.Background(), event)

	if got := notifier.recipients(); len(got) != 2 || got[0] != "dita" || got[1] != "ollie" {
		t.Fatalf("a task withdrawn from dita, who had it from ollie, told %v; want dita, then ollie", got)
	}
	delegate, owner := notifier.sent[0], notifier.sent[1]
	if owner.Title != "A task was withdrawn" || owner.Title != delegate.Title || owner.Message != delegate.Message ||
		owner.Type != delegate.Type || owner.Link != delegate.Link {
		t.Errorf("the owner was sent %+v, which is not what the delegate was sent: %+v", owner, delegate)
	}
}

func TestAnOwnerIsToldOfAWithdrawalOnceAndOfNothingElse(t *testing.T) {
	node := &entities.Node{ID: "opsApprove", Name: "Operations approve"}
	for _, c := range []struct {
		name  string
		event func() entities.ProcessEvent
		want  []string
	}{
		{"the owner is the holder", func() entities.ProcessEvent {
			event := taskCancelled(node, "ollie")
			event.Owner = "ollie"
			return event
		}, []string{"ollie"}},
		{"an owner on an event that is not a withdrawal", func() entities.ProcessEvent {
			event := handedTo(entities.EventTaskDelegated, "dita")
			event.Owner = "ollie"
			return event
		}, []string{"dita"}},
		{"an owner on an event the notifier does not act on", func() entities.ProcessEvent {
			event := handedTo(entities.EventTaskUpdated, "dita")
			event.Owner = "ollie"
			return event
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			notifier := &recordingNotifier{}
			NewNotificationObserver(notifier).OnEvent(context.Background(), c.event())
			if got := notifier.recipients(); !slices.Equal(got, c.want) {
				t.Fatalf("told %v, want %v", got, c.want)
			}
		})
	}
}
