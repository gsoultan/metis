package contracts

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

// NodeHandlerFactory defines the interface for creating node handlers.
type NodeHandlerFactory interface {
	GetHandler(nodeType entities.NodeType) (NodeHandler, error)
}
