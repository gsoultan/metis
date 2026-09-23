package impl

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
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
func (r *recordingNotifier) MarkAsRead(context.Context, uuid.UUID) error { return nil }
func (r *recordingNotifier) MarkAllAsRead(context.Context, string) error { return nil }
func (r *recordingNotifier) Delete(context.Context, uuid.UUID) error     { return nil }

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
