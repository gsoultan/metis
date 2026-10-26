package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/workflowgroup"
	"github.com/gsoultan/metis/server/repositories/store/workflowgroupmembership"
	"github.com/gsoultan/metis/server/repositories/store/workflowuser"
)

type peopleDirectoryRepository struct {
	conn *db.Conn
}

// NewPeopleDirectoryRepository reads the People directory from the main
// database, where it is, whichever environment a task is in. Removed people
// are not found: the soft delete is compiled into every read.
func NewPeopleDirectoryRepository(conn *db.Conn) contracts.PeopleDirectoryRepository {
	return &peopleDirectoryRepository{conn: conn}
}

func (r *peopleDirectoryRepository) GroupsOf(ctx context.Context, participantID uuid.UUID) ([]entities.WorkflowGroup, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	memberships, err := everyRow[workflowgroupmembership.Row](ctx, ex,
		workflowgroupmembership.New().Where(workflowgroupmembership.WorkflowUserID.Eq(participantID)))
	if err != nil {
		return nil, fmt.Errorf("could not read the groups of participant %s: %w", participantID, err)
	}
	if len(memberships) == 0 {
		return nil, nil
	}
	ids := make([][16]byte, 0, len(memberships))
	for _, m := range memberships {
		ids = append(ids, m.WorkflowGroupID)
	}
	rows, err := everyRow[workflowgroup.Row](ctx, ex, workflowgroup.New().Where(workflowgroup.ID.In(ids...)))
	if err != nil {
		return nil, fmt.Errorf("could not read the groups of participant %s: %w", participantID, err)
	}
	groups := make([]entities.WorkflowGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, groupFrom(row))
	}
	return groups, nil
}

func (r *peopleDirectoryRepository) Named(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.WorkflowUser{}, err
	}
	row, found, err := workflowuser.New().
		Where(workflowuser.ProjectID.Eq(projectID), workflowuser.Username.Eq(username)).
		One(ctx, ex)
	if err != nil {
		return entities.WorkflowUser{}, fmt.Errorf("could not read participant %q: %w", username, err)
	}
	if !found {
		return entities.WorkflowUser{}, fmt.Errorf("%w: no participant %q in this project", apierr.ErrNotFound, username)
	}
	return participantFrom(row), nil
}

func (r *peopleDirectoryRepository) Participant(ctx context.Context, id uuid.UUID) (entities.WorkflowUser, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.WorkflowUser{}, err
	}
	row, found, err := workflowuser.New().Where(workflowuser.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return entities.WorkflowUser{}, fmt.Errorf("could not read participant %s: %w", id, err)
	}
	if !found {
		return entities.WorkflowUser{}, fmt.Errorf("%w: no participant %s", apierr.ErrNotFound, id)
	}
	return participantFrom(row), nil
}

// MembersOf matches the group by name or id, as a task's candidate group is
// matched, and only within the project: another project's group of the same
// name is somebody else's.
func (r *peopleDirectoryRepository) MembersOf(ctx context.Context, projectID uuid.UUID, group string) ([]entities.WorkflowUser, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	q := workflowgroup.New().Where(workflowgroup.ProjectID.Eq(projectID), workflowgroup.Name.Eq(group))
	if id, parseErr := uuid.Parse(group); parseErr == nil {
		q = workflowgroup.New().Where(workflowgroup.ProjectID.Eq(projectID), workflowgroup.ID.Eq(id))
	}
	groups, err := everyRow[workflowgroup.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not read group %q: %w", group, err)
	}
	if len(groups) == 0 {
		return nil, nil
	}
	ids := make([][16]byte, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	memberships, err := everyRow[workflowgroupmembership.Row](ctx, ex,
		workflowgroupmembership.New().Where(workflowgroupmembership.WorkflowGroupID.In(ids...)))
	if err != nil {
		return nil, fmt.Errorf("could not read the members of group %q: %w", group, err)
	}
	if len(memberships) == 0 {
		return nil, nil
	}
	people := make([][16]byte, 0, len(memberships))
	for _, m := range memberships {
		people = append(people, m.WorkflowUserID)
	}
	rows, err := everyRow[workflowuser.Row](ctx, ex, workflowuser.New().Where(workflowuser.ID.In(people...)))
	if err != nil {
		return nil, fmt.Errorf("could not read the members of group %q: %w", group, err)
	}
	members := make([]entities.WorkflowUser, 0, len(rows))
	for _, row := range rows {
		members = append(members, participantFrom(row))
	}
	return members, nil
}
