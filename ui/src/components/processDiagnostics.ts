/**
 * The checks that look for problems in a drawn process.
 *
 * Separate from the panel that shows them: a module exporting both a component
 * and a function cannot be hot-reloaded, and editing a check would otherwise
 * reload the designer and lose the diagram in progress.
 */
import type { Edge, Node } from '@xyflow/react';
import type { BPMNEdgeData, BPMNNodeData } from '../types/bpmn';

export interface DiagnosticResult {
  severity: 'error' | 'warning' | 'info';
  message: string;
  suggestion: string;
  quickFix?: () => void;
}

export function validateProcess(nodes: Node<BPMNNodeData>[], edges: Edge<BPMNEdgeData>[]): DiagnosticResult[] {
  const diagnostics: DiagnosticResult[] = [];
  const startEvents = nodes.filter(n => n.type === 'startEvent');
  const endEvents = nodes.filter(n => n.type === 'endEvent');

  if (nodes.length > 0 && startEvents.length === 0) {
    diagnostics.push({
      severity: 'error',
      message: 'Missing Start Event',
      suggestion: 'Processes must begin with a Start Event.',
    });
  }

  if (nodes.length > 0 && endEvents.length === 0) {
    diagnostics.push({
      severity: 'warning',
      message: 'Missing End Event',
      suggestion: 'It is recommended to have at least one End Event to properly conclude the process.',
    });
  }

  // Reachability check
  if (startEvents.length > 0) {
    const visited = new Set<string>();
    const stack = startEvents.map(s => s.id);
    
    while (stack.length > 0) {
      const current = stack.pop()!;
      if (visited.has(current)) continue;
      visited.add(current);
      
      const outgoingEdges = edges.filter(e => e.source === current);
      outgoingEdges.forEach(e => stack.push(e.target));
    }
    
    const unreachableNodes = nodes.filter(n => !visited.has(n.id));
    if (unreachableNodes.length > 0) {
      diagnostics.push({
        severity: 'error',
        message: 'Unreachable Nodes Detected',
        suggestion: `${unreachableNodes.length} node(s) cannot be reached from any Start Event. Check your flow connections.`,
      });
    }
  }

  // Check for dead-end gateways
  nodes.filter(n => n.type?.includes('Gateway')).forEach(gw => {
    const outgoing = edges.filter(e => e.source === gw.id);
    if (outgoing.length === 0) {
       diagnostics.push({
         severity: 'error',
         message: `Gateway "${gw.data?.name || gw.id}" has no outgoing paths.`,
         suggestion: 'Gateways must direct the flow to at least one succeeding node.',
       });
    }

    if ((gw.type === 'exclusiveGateway' || gw.type === 'inclusiveGateway') && outgoing.length > 1) {
      const missingConditions = outgoing.filter(e => !e.data?.condition && e.id !== gw.data?.defaultFlow);
      if (missingConditions.length > 0) {
        diagnostics.push({
          severity: 'warning',
          message: `Gateway "${gw.data?.name || gw.id}" has paths without conditions.`,
          suggestion: 'Ensure all non-default paths from an Exclusive/Inclusive Gateway have a condition to avoid runtime ambiguity.',
        });
      }
    }
  });

  // Check for dead-end nodes (not End Events)
  nodes.filter(n => n.type !== 'endEvent' && !n.type?.includes('BoundaryEvent')).forEach(node => {
    const outgoing = edges.filter(e => e.source === node.id);
    if (outgoing.length === 0) {
      diagnostics.push({
        severity: 'warning',
        message: `Node "${node.data?.name || node.id}" is a dead end.`,
        suggestion: 'This node doesn\'t lead to anything. If it\'s supposed to finish the process, use an End Event.',
      });
    }
  });

  return diagnostics;
}
