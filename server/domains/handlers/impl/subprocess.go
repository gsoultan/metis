package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// SubProcessHandler handles the execution of sub-processes.
// When node.IsAdHoc is true, execution is delegated to the AdHocSubProcessHandler Strategy.
type SubProcessHandler struct {
	engine       servicecontracts.EngineRunner
	adHocHandler *AdHocSubProcessHandler
}

func (h *SubProcessHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	if node.IsAdHoc && h.adHocHandler != nil {
		return h.adHocHandler.DoExecute(ctx, instance, def, node, iterationID)
	}
	// Find start nodes within this sub-process.
	var startNodes []*entities.Node
	// Try nested nodes first
	for _, n := range node.Nodes {
		if n.Type == entities.StartEvent {
			startNodes = append(startNodes, n)
		}
	}
	// Fallback to flat list with parent ID
	if len(startNodes) == 0 {
		for _, n := range def.Nodes {
			if n.ParentID == node.ID && n.Type == entities.StartEvent {
				startNodes = append(startNodes, n)
			}
		}
	}

	if len(startNodes) == 0 {
		return fmt.Errorf("sub-process %s has no start event", node.ID)
	}

	// Remove current token and add tokens for start events within the sub-process.
	instance.RemoveTokenByNode(&node)
	for _, sn := range startNodes {
		instance.AddToken(sn)
	}

	if err := h.engine.UpdateInstance(ctx, *instance); err != nil {
		return err
	}

	// Execute all start events.
	for _, sn := range startNodes {
		if err := h.engine.ExecuteNode(ctx, instance, def, sn.ID); err != nil {
			return err
		}
	}

	return nil
}
