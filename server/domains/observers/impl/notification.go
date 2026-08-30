package impl

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/contracts"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

type notificationObserver struct {
	notificationService serviceContracts.NotificationService
}

func NewNotificationObserver(notificationService serviceContracts.NotificationService) contracts.ProcessObserver {
	return &notificationObserver{notificationService: notificationService}
}

func (o *notificationObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	switch event.Type {
	case entities.EventTaskCreated, entities.EventTaskClaimed:
		o.handleTaskEvent(ctx, event)
	}
}

func (o *notificationObserver) handleTaskEvent(ctx context.Context, event entities.ProcessEvent) {
	if event.Instance == nil || event.Project == nil {
		return
	}

	assignee, ok := event.Variables["assignee"].(string)
	if !ok || assignee == "" {
		return
	}

	taskName := "Task"
	if event.Node != nil && event.Node.Name != "" {
		taskName = event.Node.Name
	}
	var nodeID string
	if event.Node != nil {
		nodeID = event.Node.ID
	}

	notification := entities.Notification{
		ID:       uuid.New(),
		User:     &entities.User{Username: assignee},
		Type:     entities.NotificationTaskAssignment,
		Title:    "Task Update",
		Message:  fmt.Sprintf("Task '%s' in process '%s' needs your attention.", taskName, event.Instance.ID),
		Link:     fmt.Sprintf("/tasks?id=%s", nodeID),
		Project:  event.Project,
		Instance: event.Instance,
	}

	// Nobody is waiting on this call, but somebody is waiting on the task it is
	// about — a notification that never arrives looks like a task that was never
	// assigned.
	if err := o.notificationService.Send(ctx, notification); err != nil {
		log.Warn().Err(err).Str("assignee", assignee).Msg("Could not notify the assignee about their task")
	}
}
