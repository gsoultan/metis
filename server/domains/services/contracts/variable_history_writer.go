package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// VariableHistoryWriter writes variable snapshots to the audit store.
// Call CaptureSnapshot after every UpdateInstance to maintain a full history.
type VariableHistoryWriter interface {
	CaptureSnapshot(ctx context.Context, snapshot entities.VariableSnapshot) error
}
