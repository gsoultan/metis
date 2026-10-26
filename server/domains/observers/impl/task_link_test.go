package impl

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

type issuingLinks struct {
	serviceContracts.TaskLinkService
	asked []uuid.UUID
}

func (l *issuingLinks) IssueTaskLinks(_ context.Context, id uuid.UUID) ([]serviceContracts.IssuedTaskLink, error) {
	l.asked = append(l.asked, id)
	return []serviceContracts.IssuedTaskLink{{TaskID: id, Token: "mtl_x"}}, nil
}

func TestALinkIsSentWhenWorkArrivesForSomebody(t *testing.T) {
	cases := []struct {
		event entities.ProcessEvent
		sends bool
	}{
		{entities.ProcessEvent{Type: entities.EventTaskCreated}, true},
		{entities.ProcessEvent{Type: entities.EventTaskDelegated, Assignee: "bob"}, true},
		{entities.ProcessEvent{Type: entities.EventTaskResolved, Assignee: "ada"}, true},
		{entities.ProcessEvent{Type: entities.EventTaskClaimed, Assignee: "bob"}, true},
		// Somebody taking a task themselves needs no link to it.
		{entities.ProcessEvent{Type: entities.EventTaskClaimed}, false},
		{entities.ProcessEvent{Type: entities.EventTaskCompleted}, false},
		{entities.ProcessEvent{Type: entities.EventTaskCanceled, Assignee: "ada"}, false},
	}
	for _, tc := range cases {
		links := &issuingLinks{}
		sent := 0
		observer := NewTaskLinkObserver(links, func(context.Context, serviceContracts.IssuedTaskLink, entities.ProcessEvent) { sent++ })
		tc.event.TaskID = uuid.New()
		observer.OnEvent(t.Context(), tc.event)
		if (sent == 1) != tc.sends {
			t.Errorf("%s (assignee %q): sent %d links", tc.event.Type, tc.event.Assignee, sent)
		}
	}
	// An event that names no task issues nothing.
	links := &issuingLinks{}
	NewTaskLinkObserver(links, func(context.Context, serviceContracts.IssuedTaskLink, entities.ProcessEvent) {}).
		OnEvent(t.Context(), entities.ProcessEvent{Type: entities.EventTaskCreated})
	if len(links.asked) != 0 {
		t.Fatal("links were issued for an event that names no task")
	}
}
