package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// HeatmapReader computes and returns live execution statistics for all nodes
// of a process definition, enabling the BPMN heatmap overlay in the designer.
type HeatmapReader interface {
	// GetHeatmap returns aggregated node statistics for the given definition.
	// Results include active token counts, completion counts, and average durations.
	GetHeatmap(ctx context.Context, definitionID uuid.UUID) ([]entities.HeatmapNode, error)
}
