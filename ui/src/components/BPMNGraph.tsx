import { useMemo } from 'react';
import {
  ReactFlow,
  Controls,
  Background,
  type Node,
  type Edge,
  BackgroundVariant,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Box } from '@mantine/core';
import { nodeTypes } from './bpmnNodeTypes';

interface BPMNNode {
  id: string;
  name: string;
  type: string;
  assignee?: string;
  x?: number;
  y?: number;
}

interface BPMNFlow {
  id: string;
  source_ref: string;
  target_ref: string;
  condition?: string;
}

interface BPMNGraphProps {
  nodes?: BPMNNode[];
  flows?: BPMNFlow[];
  heatmapData?: Record<string, number>;
  isReadOnly?: boolean;
}

export function BPMNGraph({ nodes = [], flows = [], heatmapData = {}, isReadOnly = false }: BPMNGraphProps) {
  const reactFlowNodes: Node[] = useMemo(() => {
    return (nodes || []).map((node, index) => ({
      id: node.id,
      type: node.type,
      position: { x: node.x || (50 + index * 250), y: node.y || 80 },
      data: { 
        label: node.name, 
        type: node.type,
        assignee: node.assignee,
        heatmapValue: heatmapData[node.id],
      },
    }));
  }, [nodes, heatmapData]);

  const reactFlowEdges: Edge[] = useMemo(() => {
    return (flows || []).map((flow) => ({
      id: flow.id || `e-${flow.source_ref}-${flow.target_ref}`,
      source: flow.source_ref,
      target: flow.target_ref,
      label: flow.condition,
      animated: true,
      style: { strokeWidth: 2 },
    }));
  }, [flows]);

  return (
    <Box style={{ 
      height: 450, 
      border: '1px solid var(--mantine-color-default-border)', 
      borderRadius: 'var(--mantine-radius-lg)', 
      background: 'var(--mantine-color-body)',
      overflow: 'hidden',
      boxShadow: 'var(--mantine-shadow-sm)'
    }}>
      <ReactFlow
        nodes={reactFlowNodes}
        edges={reactFlowEdges}
        nodeTypes={nodeTypes}
        fitView
        nodesDraggable={!isReadOnly}
        nodesConnectable={!isReadOnly}
        elementsSelectable={!isReadOnly}
        defaultEdgeOptions={{
          style: { strokeWidth: 2, stroke: 'var(--mantine-color-gray-4)' },
          type: 'smoothstep',
          animated: true,
        }}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--mantine-color-gray-2)" />
        <Controls />
      </ReactFlow>
    </Box>
  );
}
