package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
)

type AuditLogObserver struct {
	repo contracts.AuditRepository
}

func NewAuditLogObserver(repo contracts.AuditRepository) *AuditLogObserver {
	return &AuditLogObserver{repo: repo}
}

func (o *AuditLogObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	entry := entities.AuditEntry{
		Project:   event.Project,
		Instance:  event.Instance,
		Type:      event.Type,
		Node:      event.Node,
		Data:      event.Variables,
		Timestamp: time.Unix(event.Timestamp, 0),
	}

	switch event.Type {
	case entities.EventProcessStarted:
		entry.Message = "Process instance started"
		initiator := "system"
		if v, ok := event.Variables["initiator"]; ok {
			initiator = fmt.Sprintf("%v", v)
		}
		entry.Narrative = fmt.Sprintf("The process was initiated by %s.", initiator)
	case entities.EventNodeReached:
		id := ""
		if event.Node != nil {
			id = event.Node.ID
		}
		entry.Message = "Reached node: " + id
		nodeName := ""
		if event.Node != nil && event.Node.Name != "" {
			nodeName = event.Node.Name
		} else {
			nodeName = id
		}
		entry.Narrative = fmt.Sprintf("Process reached the '%s' step.", nodeName)
	case entities.EventTaskCreated:
		id := ""
		if event.Node != nil {
			id = event.Node.ID
		}
		entry.Message = "Task created for node: " + id
		nodeName := "Human Action"
		if event.Node != nil && event.Node.Name != "" {
			nodeName = event.Node.Name
		}
		entry.Narrative = fmt.Sprintf("A new task '%s' is now pending.", nodeName)
	case entities.EventTaskCanceled:
		id := ""
		if event.Node != nil {
			id = event.Node.ID
		}
		entry.Message = "Task canceled for node: " + id
		nodeName := "Human Action"
		if event.Node != nil && event.Node.Name != "" {
			nodeName = event.Node.Name
		}
		entry.Narrative = fmt.Sprintf("'%s' is no longer needed and has been withdrawn.", nodeName)
	case entities.EventTaskClaimed:
		id := ""
		if event.Node != nil {
			id = event.Node.ID
		}
		entry.Message = "Task claimed for node: " + id
		assignee := "an user"
		if v, ok := event.Variables["assignee"]; ok {
			assignee = fmt.Sprintf("%v", v)
		}
		nodeName := "the task"
		if event.Node != nil && event.Node.Name != "" {
			nodeName = event.Node.Name
		}
		entry.Narrative = fmt.Sprintf("%s has started working on '%s'.", assignee, nodeName)
	case entities.EventTaskCompleted:
		id := ""
		if event.Node != nil {
			id = event.Node.ID
		}
		entry.Message = "Task completed for node: " + id
		assignee := "The user"
		if v, ok := event.Variables["assignee"]; ok {
			assignee = fmt.Sprintf("%v", v)
		}
		nodeName := "the task"
		if event.Node != nil && event.Node.Name != "" {
			nodeName = event.Node.Name
		}

		decision := ""
		if v, ok := event.Variables["approved"]; ok {
			if b, ok := v.(bool); ok {
				if b {
					decision = " and approved the request"
				} else {
					decision = " and rejected the request"
				}
			}
		}

		entry.Narrative = fmt.Sprintf("%s completed '%s'%s.", assignee, nodeName, decision)
	case entities.EventProcessCompleted:
		entry.Message = "Process instance completed"
		entry.Narrative = "The process has been successfully concluded."
	case "incident_created":
		entry.Narrative = "An issue occurred that requires attention: " + fmt.Sprintf("%v", event.Variables["error"])
	default:
		entry.Message = "Event: " + event.Type
		entry.Narrative = entry.Message
	}

	_ = o.repo.Create(ctx, adapters.AuditModelAdapter{Entry: entry}.ToModel())
}
