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
		ns = append(ns, notificationOf(m))
	}
	return ns, nil
}

// ListByUserPaged returns one page of a person's notifications, newest first.
//
// This is what the notification list reads. It was sent the newest thousand
// on every poll, which was both too much to send each minute and too little to
// hold anybody's older notifications.
func (s *notificationService) ListByUserPaged(ctx context.Context, userID string, page repoContracts.Pagination) (repoContracts.Page[entities.Notification], error) {
	stored, err := s.repo.ListByUserPaged(ctx, userID, page)
	if err != nil {
		return repoContracts.NewPage([]entities.Notification{}, 0, page), err
	}
	items := make([]entities.Notification, 0, len(stored.Items))
	for _, m := range stored.Items {
		items = append(items, notificationOf(m))
	}
	return repoContracts.NewPage(items, stored.Total, page), nil
}

// notificationOf is the entity for one stored notification. What it was about
// comes back as shells carrying only their ids.
func notificationOf(m models.NotificationModel) entities.Notification {
	var project *entities.Project
	if m.ProjectID != nil {
		project = &entities.Project{ID: uuid.UUID(*m.ProjectID)}
	}
	var instance *entities.ProcessInstance
	if m.InstanceID != nil {
		instance = &entities.ProcessInstance{ID: uuid.UUID(*m.InstanceID)}
	}
	return entities.Notification{
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
	}
}

// CountUnreadByUser counts a person's unread notifications where they are kept.
//
// The bell used to count them in the browser, among the notifications the list
// had sent it — the newest thousand — so an unread one older than those was
// never counted. Counting here is one statement and a number on the wire,
// which is also what makes it cheap enough to poll.
func (s *notificationService) CountUnreadByUser(ctx context.Context, userID string) (int64, error) {
	return s.repo.CountUnreadByUser(ctx, userID)
}

func (s *notificationService) MarkAsRead(ctx context.Context, id uuid.UUID, recipient string) error {
	return s.repo.MarkAsRead(ctx, id, recipient)
}

func (s *notificationService) MarkAllAsRead(ctx context.Context, userID string) error {
	return s.repo.MarkAllAsRead(ctx, userID)
}

func (s *notificationService) Delete(ctx context.Context, id uuid.UUID, recipient string) error {
	return s.repo.Delete(ctx, id, recipient)
}
