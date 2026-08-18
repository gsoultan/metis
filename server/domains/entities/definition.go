package entities

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// ProcessDefinition represents a definition of a process based on BPMN 2.0.
type ProcessDefinition struct {
	ID            uuid.UUID       `json:"id"`
	Project       *Project        `json:"project,omitzero"`
	Key           string          `json:"key"`
	Name          string          `json:"name"`
	Version       int             `json:"version"`
	Documentation string          `json:"documentation,omitzero"`
	Nodes         []*Node         `json:"nodes,omitzero"`
	Flows         []*SequenceFlow `json:"flows,omitzero"`
	CreatedAt     time.Time       `json:"created_at,omitzero"`
	Deployment    *Deployment     `json:"deployment,omitzero"`

	// Cache maps for performance
	initOnce    sync.Once
	nodeMap     map[string]*Node
	outgoingMap map[string][]*SequenceFlow
	incomingMap map[string][]*SequenceFlow
	boundaryMap map[string][]*Node
}

func (pd *ProcessDefinition) initialize() {
	pd.initOnce.Do(func() {
		pd.nodeMap = make(map[string]*Node)
		pd.outgoingMap = make(map[string][]*SequenceFlow)
		pd.incomingMap = make(map[string][]*SequenceFlow)
		pd.boundaryMap = make(map[string][]*Node)

		pd.traverseNodes(func(n *Node) {
			pd.nodeMap[n.ID] = n
			if n.Type == BoundaryEvent && n.AttachedToRef != "" {
				pd.boundaryMap[n.AttachedToRef] = append(pd.boundaryMap[n.AttachedToRef], n)
			}
		})

		pd.traverseFlows(func(f *SequenceFlow) {
			pd.outgoingMap[f.SourceRef] = append(pd.outgoingMap[f.SourceRef], f)
			pd.incomingMap[f.TargetRef] = append(pd.incomingMap[f.TargetRef], f)
		})
	})
}

func (pd *ProcessDefinition) FindNode(id string) *Node {
	pd.initialize()
	return pd.nodeMap[id]
}

func (pd *ProcessDefinition) GetStartNode() *Node {
	return getStartNodeRecursively(pd.Nodes)
}

func getStartNodeRecursively(nodes []*Node) *Node {
	for i := range nodes {
		if nodes[i].Type == StartEvent {
			return nodes[i]
		}
		if res := getStartNodeRecursively(nodes[i].Nodes); res != nil {
			return res
		}
	}
	return nil
}

func (pd *ProcessDefinition) GetOutgoingFlows(nodeID string) []*SequenceFlow {
	pd.initialize()
	return pd.outgoingMap[nodeID]
}

func (pd *ProcessDefinition) traverseFlows(fn func(*SequenceFlow)) {
	for i := range pd.Flows {
		fn(pd.Flows[i])
	}
	for i := range pd.Nodes {
		pd.Nodes[i].traverseFlows(fn)
	}
}

func (pd *ProcessDefinition) Accept(visitor DefinitionVisitor) {
	visitor.VisitDefinition(pd)
	// The visitor is given the nil so it can record it as a validation error;
	// walking the children of one is what would fault.
	if pd == nil {
		return
	}
	for i := range pd.Nodes {
		pd.Nodes[i].Accept(visitor)
	}
	for i := range pd.Flows {
		visitor.VisitSequenceFlow(pd.Flows[i])
	}
}

func (pd *ProcessDefinition) GetIncomingFlows(nodeID string) []*SequenceFlow {
	pd.initialize()
	return pd.incomingMap[nodeID]
}

func (pd *ProcessDefinition) GetBoundaryEvents(nodeID string) []*Node {
	pd.initialize()
	return pd.boundaryMap[nodeID]
}

func (pd *ProcessDefinition) traverseNodes(fn func(*Node)) {
	for i := range pd.Nodes {
		pd.Nodes[i].traverse(fn)
	}
}

// BuildAncestorSet returns the set of all node IDs that have at least one path
// leading to targetNodeID.  Building the map once and doing O(1) lookups per
// token is more efficient than calling HasPath (which re-builds the flow map)
// inside a loop over every active token.
func (pd *ProcessDefinition) BuildAncestorSet(targetNodeID string) map[string]bool {
	// Build a reverse adjacency map: target → sources
	reverseMap := make(map[string][]string)
	pd.traverseFlows(func(f *SequenceFlow) {
		reverseMap[f.TargetRef] = append(reverseMap[f.TargetRef], f.SourceRef)
	})

	ancestors := make(map[string]bool)
	queue := []string{targetNodeID}
	visited := map[string]bool{targetNodeID: true}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, src := range reverseMap[current] {
			if !visited[src] {
				visited[src] = true
				ancestors[src] = true
				queue = append(queue, src)
			}
		}
	}
	return ancestors
}

// HasPath checks if there is a path from fromNodeID to toNodeID in the process definition.
func (pd *ProcessDefinition) HasPath(fromNodeID, toNodeID string) bool {
	if fromNodeID == toNodeID {
		return true
	}

	// Build flow map for efficiency
	flowMap := make(map[string][]string)
	pd.traverseFlows(func(f *SequenceFlow) {
		flowMap[f.SourceRef] = append(flowMap[f.SourceRef], f.TargetRef)
	})

	visited := make(map[string]bool)
	var queue []string
	queue = append(queue, fromNodeID)
	visited[fromNodeID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		targets := flowMap[current]
		for _, target := range targets {
			if target == toNodeID {
				return true
			}
			if !visited[target] {
				visited[target] = true
				queue = append(queue, target)
			}
		}
	}

	return false
}
