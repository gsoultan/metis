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
	case entities.EventTaskCreated, entities.EventTaskClaimed, entities.EventTaskCanceled:
		o.handleTaskEvent(ctx, event)
	}
}

// handleTaskEvent tells whoever now has work that they have it.
func (o *notificationObserver) handleTaskEvent(ctx context.Context, event entities.ProcessEvent) {
	if event.Instance == nil || event.Project == nil {
		return
	}

	for _, recipient := range taskRecipients(event) {
		notification := entities.Notification{
			ID:       uuid.New(),
			User:     &entities.User{Username: recipient},
			Type:     entities.NotificationTaskAssignment,
			Title:    taskNotificationTitle(event),
			Message:  taskNotificationMessage(event),
			Link:     taskNotificationLink(event),
			Project:  event.Project,
			Instance: event.Instance,
		}

		// Nobody is waiting on this call, but somebody is waiting on the task it
		// is about — a notification that never arrives looks like a task that
		// was never assigned.
		if err := o.notificationService.Send(ctx, notification); err != nil {
			log.Warn().Err(err).Str("assignee", recipient).Msg("Could not notify the assignee about their task")
		}
	}
}

// taskRecipients is everybody who should be told about this task.
//
// The assignee used to be read only from event.Variables, which is the
// *instance's* business data — the amount on a quotation, the customer's name.
// It carries an "assignee" key on exactly the two events that build one by
// hand: claiming a task, and handing one to somebody. A task the process
// created already assigned carries its assignee on the node instead, and
// Variables there is whatever the process happens to contain, so the lookup
// missed and nobody was told.
//
// The effect was that you were notified when you claimed a task yourself, which
// you already knew, and not when one arrived for you, which is the entire
// point.
func taskRecipients(event entities.ProcessEvent) []string {
	// The event says who it is about, and more precisely than the node can:
	// work taken back from whoever claimed it concerns them, not whoever the
	// diagram nominated.
	if event.Assignee != "" {
		return []string{event.Assignee}
	}
	// The older way of saying the same thing, still used by claiming and
	// assigning.
	if assignee, ok := event.Variables["assignee"].(string); ok && assignee != "" {
		return []string{assignee}
	}
	if event.Node == nil {
		return nil
	}
	if event.Node.Assignee != "" {
		return []string{event.Node.Assignee}
	}

	// Nobody is named, so the task is offered rather than assigned. Everybody
	// who could pick it up is told: a queue nobody is told about is one that is
	// read when something in it is already overdue.
	seen := make(map[string]struct{}, len(event.Node.CandidateUsers))
	recipients := make([]string, 0, len(event.Node.CandidateUsers))
	for _, candidate := range event.Node.CandidateUsers {
		if candidate == nil || candidate.Username == "" {
			continue
		}
		if _, duplicate := seen[candidate.Username]; duplicate {
			continue
		}
		seen[candidate.Username] = struct{}{}
		recipients = append(recipients, candidate.Username)
	}
	return recipients
}

func taskNotificationTitle(event entities.ProcessEvent) string {
	switch event.Type {
	case entities.EventTaskCanceled:
		return "A task was withdrawn"
	case entities.EventTaskCreated:
		return "A task is waiting for you"
	default:
		return "Task Update"
	}
}

// taskNotificationMessage names the work and the process it belongs to.
//
// It used to interpolate the instance id, so somebody opening their
// notifications read a sentence with a raw uuid in the middle of it. The
// process is what the work is *for*, and the step name is which part of it.
func taskNotificationMessage(event entities.ProcessEvent) string {
	taskName := "A task"
	if event.Node != nil && event.Node.Name != "" {
		taskName = fmt.Sprintf("%q", event.Node.Name)
	}
	process := processName(event)

	// Work being taken away needs saying as plainly as work arriving. A task
	// that vanishes from an inbox with no explanation is indistinguishable
	// from one somebody else completed, or from a bug.
	if event.Type == entities.EventTaskCanceled {
		if process != "" {
			return fmt.Sprintf("%s in %s is no longer needed and has been taken off your list.", taskName, process)
		}
		return fmt.Sprintf("%s is no longer needed and has been taken off your list.", taskName)
	}

	if process != "" {
		return fmt.Sprintf("%s is waiting for you in %s.", taskName, process)
	}
	return fmt.Sprintf("%s is waiting for you.", taskName)
}

// processName is what to call the process in a sentence.
func processName(event entities.ProcessEvent) string {
	if event.Instance == nil || event.Instance.Definition == nil {
		return ""
	}
	if name := event.Instance.Definition.Name; name != "" {
		return name
	}
	return event.Instance.Definition.Key
}

// taskNotificationLink points at the instance rather than the node.
//
// It used to be /tasks?id=<node id>, which is not something the inbox can look
// a task up by: a node id is unique within a definition, not across the
// instances running it, so the link was ambiguous the moment two were running.
func taskNotificationLink(event entities.ProcessEvent) string {
	if event.Instance == nil {
		return "/tasks"
	}
	return fmt.Sprintf("/tasks?instance=%s", event.Instance.ID)
}
