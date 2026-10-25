package impl

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/contracts"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// TaskLinkSender hands a link just issued to the person it is for. It is
// called inside the transaction that created or moved the task, so it must
// only schedule the sending, never wait on a mail server.
type TaskLinkSender func(ctx context.Context, link serviceContracts.IssuedTaskLink, event entities.ProcessEvent)

type taskLinkObserver struct {
	links serviceContracts.TaskLinkService
	send  TaskLinkSender
}

// NewTaskLinkObserver issues a link to each person work arrives for, and
// sends it: when a task is created, handed to somebody, delegated, or handed
// back.
func NewTaskLinkObserver(links serviceContracts.TaskLinkService, send TaskLinkSender) contracts.ProcessObserver {
	return &taskLinkObserver{links: links, send: send}
}

func (o *taskLinkObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	if !workArrives(event) || o.links == nil || o.send == nil {
		return
	}
	issued, err := o.links.IssueTaskLinks(ctx, event.TaskID)
	if err != nil {
		// The task stands either way, and is in the person's inbox: a link is
		// a way to it, not the record of it.
		log.Warn().Err(err).Str("task_id", event.TaskID.String()).Msg("Could not issue links to a task; it is in the inbox and nowhere else")
		return
	}
	for _, link := range issued {
		o.send(ctx, link, event)
	}
}

// workArrives reports whether the event brings a task to somebody new. A
// claim does only when it names who the task was given to: a person taking
// a task themselves needs no link to it.
func workArrives(event entities.ProcessEvent) bool {
	if event.TaskID == [16]byte{} {
		return false
	}
	switch event.Type {
	case entities.EventTaskCreated, entities.EventTaskDelegated, entities.EventTaskResolved:
		return true
	case entities.EventTaskClaimed:
		return event.Assignee != ""
	}
	return false
}
