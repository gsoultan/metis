package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/storm/runtime"
)

type notificationRepository struct{ conn }

// NewNotificationRepository returns the messages waiting for a person.
//
// Scoped by recipient *and* by project. The recipient alone is not enough: one
// person can be in two organizations under the same user id, and their inbox in
// one must not show — or be marked read by — what happened in the other.
//
// A notification carrying no project is left visible. It is reachable only
// through the user id, which already names its owner, and filtering it out
// would hide the system messages that belong to no project at all.
func NewNotificationRepository(c *db.Conn) contracts.NotificationRepository {
	return &notificationRepository{conn{conn: c}}
}

// Create records one notification.
func (r *notificationRepository) Create(ctx context.Context, n models.NotificationModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := notification.Create()
	if id := uuid.UUID(n.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetUserID(n.UserID)
	ins.SetType(n.Type)
	ins.SetTitle(n.Title)
	ins.SetMessage(n.Message)
	ins.SetIsRead(n.IsRead)
	setOrNullString(ins.SetLink, ins.SetLinkNull, n.Link)
	// Both references are optional: a notification outlives what it was about,
	// and deleting a project must not silently delete the messages telling
	// people what happened in it.
	if n.ProjectID != nil {
		ins.SetProjectID(uuid.UUID(*n.ProjectID))
	}
	if n.InstanceID != nil {
		ins.SetInstanceID(uuid.UUID(*n.InstanceID))
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not create the notification: %w", err)
	}
	return nil
}

// ListByUser returns one person's notifications, newest first.
func (r *notificationRepository) ListByUser(ctx context.Context, userID string) ([]models.NotificationModel, error) {
	visible, err := r.projectFilter(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := notification.New().
		Where(notification.UserID.Eq(userID)).
		Order(notification.CreatedAt.Desc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the notifications: %w", err)
	}
	out := make([]models.NotificationModel, 0, len(rows))
	for _, row := range rows {
		if !visible(row.ProjectID) {
			continue
		}
		out = append(out, notificationFrom(row))
	}
	return out, nil
}

// ListByUserPaged returns one page of a person's notifications, newest first,
// and how many they have in all.
//
// The order is total — creation time, then id — because the notifications one
// transaction writes share its created_at, and among ties LIMIT and OFFSET may
// come back in a different order for every page: ordered by creation time
// alone, walking 1,050 notifications fifty to a created_at in pages of twenty
// showed 851 of them, 151 more than once.
func (r *notificationRepository) ListByUserPaged(ctx context.Context, userID string, p contracts.Pagination) (contracts.Page[models.NotificationModel], error) {
	empty := contracts.NewPage([]models.NotificationModel{}, 0, p)
	inbox, err := r.inboxOf(ctx, userID)
	if err != nil {
		return empty, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return empty, err
	}
	total, err := inbox.Count(ctx, ex)
	if err != nil {
		return empty, fmt.Errorf("could not count the notifications: %w", err)
	}
	n := p.Normalize()
	rows, err := inbox.
		Order(notification.CreatedAt.Desc(), notification.ID.Desc()).
		Limit(int64(n.PageSize)).
		Offset(int64(p.Offset())).
		All(ctx, ex, nil)
	if err != nil {
		return empty, fmt.Errorf("could not read the notifications: %w", err)
	}
	items := make([]models.NotificationModel, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationFrom(row))
	}
	return contracts.NewPage(items, total, p), nil
}

// CountUnreadByUser counts one person's unread notifications: one COUNT, over
// every one of them the caller's organization may see.
func (r *notificationRepository) CountUnreadByUser(ctx context.Context, userID string) (int64, error) {
	inbox, err := r.inboxOf(ctx, userID)
	if err != nil {
		return 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	unread, err := inbox.Where(notification.IsRead.Eq(false)).Count(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not count the unread notifications: %w", err)
	}
	return unread, nil
}

// inboxOf is one person's notifications as the caller may see them, with the
// scope in the query rather than applied to its rows afterwards.
//
// It is the scope projectFilter applies row by row: the projects of the
// caller's organization, and no project at all, because a system message is
// addressed to the person and belongs to no organization. Filtering after the
// read is only right when the read is every row. After a capped one it counts
// and pages what the cap let through — the newest thousand of the person's
// notifications across all their organizations, of which this one may hold
// none.
func (r *notificationRepository) inboxOf(ctx context.Context, userID string) (notification.Query, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return notification.Query{}, err
	}
	inbox := notification.New().Where(notification.UserID.Eq(userID))
	if scope.unrestricted() {
		return inbox, nil
	}
	if len(scope.projects) == 0 {
		return inbox.Where(notification.ProjectID.IsNull()), nil
	}
	// The project list before the null test: storm's Any keeps what comes
	// before an IsNull and silently drops what follows it, so the other order
	// would match the system messages alone.
	return inbox.Any(
		notification.ProjectID.In(uuidsToRaw(scope.projects)...),
		notification.ProjectID.IsNull(),
	), nil
}

func (r *notificationRepository) MarkAsRead(ctx context.Context, id uuid.UUID, recipient string) error {
	row, err := r.ownedBy(ctx, id, recipient)
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := notification.Mutate(row)
	mut.SetIsRead(true)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not mark the notification read: %w", err)
	}
	return nil
}

// MarkAllAsRead clears one person's unread notifications.
//
// Raw SQL: this is an update over a predicate, and reading the rows first to
// name them would be a read of exactly the rows about to change.
func (r *notificationRepository) MarkAllAsRead(ctx context.Context, userID string) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.unrestricted() {
		if _, err := ex.Exec(ctx,
			"UPDATE notifications SET is_read = true, updated_at = now() WHERE user_id = $1 AND is_read = false",
			[]any{userID}); err != nil {
			return fmt.Errorf("could not mark the notifications read: %w", err)
		}
		return nil
	}
	// Scoped, because one person can hold the same user id in two
	// organizations: clearing their inbox here must not clear the other one.
	// A notification with no project is theirs wherever they are.
	if _, err := ex.Exec(ctx,
		`UPDATE notifications SET is_read = true, updated_at = now()
		  WHERE user_id = $1 AND is_read = false
		    AND (project_id IS NULL OR project_id = ANY($2))`,
		[]any{userID, uuidsToRaw(scope.projects)}); err != nil {
		return fmt.Errorf("could not mark the notifications read: %w", err)
	}
	return nil
}

// projectFilter answers whether one row's project is one the caller may see.
//
// A closure rather than a predicate because the column is nullable and "no
// project" must stay visible: expressing that as SQL means an OR that every
// caller of this table would have to remember, and forgetting it hides the
// system messages rather than showing too many.
func (r *notificationRepository) projectFilter(ctx context.Context) (func(runtime.Null[[16]byte]) bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if scope.unrestricted() {
		return func(runtime.Null[[16]byte]) bool { return true }, nil
	}
	allowed := make(map[uuid.UUID]struct{}, len(scope.projects))
	for _, id := range scope.projects {
		allowed[id] = struct{}{}
	}
	return func(projectID runtime.Null[[16]byte]) bool {
		id, ok := projectID.Get()
		if !ok {
			return true
		}
		_, permitted := allowed[uuid.UUID(id)]
		return permitted
	}, nil
}

func (r *notificationRepository) Delete(ctx context.Context, id uuid.UUID, recipient string) error {
	if _, err := r.ownedBy(ctx, id, recipient); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := notification.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such notification", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the notification: %w", err)
	}
	return nil
}

// ownedBy reads a notification in the caller's organization that is
// recipient's, and answers somebody else's as not found rather than as
// forbidden, so an id does not tell anybody whose it is or that it exists.
func (r *notificationRepository) ownedBy(ctx context.Context, id uuid.UUID, recipient string) (notification.Row, error) {
	row, err := r.one(ctx, id)
	if err != nil {
		return notification.Row{}, err
	}
	if recipient == "" || row.UserID != recipient {
		return notification.Row{}, fmt.Errorf("%w: no such notification", apierr.ErrNotFound)
	}
	return row, nil
}

func notificationFrom(row notification.Row) models.NotificationModel {
	n := models.NotificationModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		UserID:  row.UserID,
		Type:    row.Type,
		Title:   row.Title,
		Message: row.Message,
		IsRead:  row.IsRead,
		Link:    valueOr(row.Link),
	}
	if projectID, ok := row.ProjectID.Get(); ok {
		id := models.UUID(projectID)
		n.ProjectID = &id
	}
	if instanceID, ok := row.InstanceID.Get(); ok {
		id := models.UUID(instanceID)
		n.InstanceID = &id
	}
	return n
}

// one reads a notification the caller may see.
//
// Read before every write, so a refusal happens before the row is touched:
// deleting first and checking afterwards is not a refusal at all.
func (r *notificationRepository) one(ctx context.Context, id uuid.UUID) (notification.Row, error) {
	visible, err := r.projectFilter(ctx)
	if err != nil {
		return notification.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return notification.Row{}, err
	}
	row, found, err := notification.New().Where(notification.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return notification.Row{}, fmt.Errorf("could not read the notification: %w", err)
	}
	if !found || !visible(row.ProjectID) {
		return notification.Row{}, fmt.Errorf("%w: no such notification", apierr.ErrNotFound)
	}
	return row, nil
}
