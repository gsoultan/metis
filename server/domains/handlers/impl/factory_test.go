package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Deploy accepts every type entities declares. Each must reach a handler that
// runs it, except the pool and the lane: they draw the diagram's containers,
// and no sequence flow can lead to either. A type that fell to NullNodeHandler
// would deploy and then fail every instance that reached it.
func TestEveryTypeDeployAcceptsHasAHandler(t *testing.T) {
	factory := NewNodeHandlerFactory(nil, nil, nil, nil, nil, nil, nil, nil)
	for _, nodeType := range entities.NodeTypes() {
		handler, err := factory.GetHandler(nodeType)
		if err != nil {
			t.Fatalf("%s: %v", nodeType, err)
		}
		_, null := handler.(*NullNodeHandler)
		container := nodeType == entities.Pool || nodeType == entities.Lane
		if null != container {
			t.Errorf("%s: null handler = %v, want %v", nodeType, null, container)
		}
	}
}
