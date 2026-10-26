package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/workflowgroup"
	"github.com/gsoultan/metis/server/repositories/store/workflowgroupmembership"
)

type personGroupsRepository struct {
	conn *db.Conn
}

// NewPersonGroupsRepository reads People group membership from the main
// database, where the directory is, whichever environment a task is in.
func NewPersonGroupsRepository(conn *db.Conn) contracts.PersonGroupsRepository {
	return &personGroupsRepository{conn: conn}
}

func (r *personGroupsRepository) GroupsOf(ctx context.Context, participantID uuid.UUID) ([]entities.WorkflowGroup, error) {
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
