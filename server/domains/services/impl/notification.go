package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repoContracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

type notificationService struct {
	repo repoContracts.NotificationRepository
}

func NewNotificationService(repo repoContracts.NotificationRepository) contracts.NotificationService {
	return &notificationService{repo: repo}
}

func (s *notificationService) Send(ctx context.Context, n entities.Notification) error {
	var userID string
	if n.User != nil {
		userID = n.User.Username
	}
	var projectID, instanceID *uuid.UUID
	if n.Project != nil {
		projectID = &n.Project.ID
	}
	if n.Instance != nil {
		instanceID = &n.Instance.ID
	}
	m := models.NotificationModel{
		UserID:     userID,
		Type:       string(n.Type),
		Title:      n.Title,
		Message:    n.Message,
		Link:       n.Link,
		ProjectID:  models.FromUUIDPtr(projectID),
		InstanceID: models.FromUUIDPtr(instanceID),
	}
	if n.ID != uuid.Nil {
		m.ID = models.UUID(n.ID)
	} else {
		m.ID = models.UUID(uuid.New())
	}
	m.IsRead = n.IsRead
	return s.repo.Create(ctx, m)
}

func (s *notificationService) ListByUser(ctx context.Context, userID string) ([]entities.Notification, error) {
	ms, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var ns []entities.Notification
	for _, m := range ms {
		var project *entities.Project
		if m.ProjectID != nil {
			project = &entities.Project{ID: uuid.UUID(*m.ProjectID)}
		}
		var instance *entities.ProcessInstance
		if m.InstanceID != nil {
			instance = &entities.ProcessInstance{ID: uuid.UUID(*m.InstanceID)}
		}
		ns = append(ns, entities.Notification{
			ID:        uuid.UUID(m.ID),
			User:      &entities.User{Username: m.UserID},
			Type:      entities.NotificationType(m.Type),
			Title:     m.Title,
			Message:   m.Message,
			IsRead:    m.IsRead,
			Link:      m.Link,
			CreatedAt: m.CreatedAt,
			Project:   project,
			Instance:  instance,
		})
	}
	return ns, nil
}

func (s *notificationService) MarkAsRead(ctx context.Context, id uuid.UUID) error {
	return s.repo.MarkAsRead(ctx, id)
}

func (s *notificationService) MarkAllAsRead(ctx context.Context, userID string) error {
	return s.repo.MarkAllAsRead(ctx, userID)
}

func (s *notificationService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
