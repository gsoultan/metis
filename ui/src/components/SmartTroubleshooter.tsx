import { Stack, Group, Text, Paper, ThemeIcon, Button, Badge, Alert } from '@mantine/core';
import { AlertCircle, CheckCircle2, Lightbulb, Zap, ArrowRight } from 'lucide-react';
import type { Edge, Node } from '@xyflow/react';
import { asTextList, type BPMNEdgeData, type BPMNNodeData } from '../types/bpmn';

interface DiagnosticResult {
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

interface SmartTroubleshooterProps {
  node?: Node<BPMNNodeData>;
  edge?: Edge<BPMNEdgeData>;
  updateNodeData?: (id: string, data: Partial<BPMNNodeData>) => void;
  updateEdgeData?: (id: string, label: string, data: Partial<BPMNEdgeData>) => void;
}

export function SmartTroubleshooter({ node, edge, updateNodeData, updateEdgeData }: SmartTroubleshooterProps) {
  const diagnostics: DiagnosticResult[] = [];

  if (node) {
    const data = node.data || {};
    
    // Service Task Diagnostics
    if (node.type === 'serviceTask') {
      if (!data.implementation && !data.connector_id) {
        diagnostics.push({
          severity: 'error',
          message: 'Implementation missing.',
          suggestion: 'Choose a connector from the catalog or set a custom implementation.',
          quickFix: () => updateNodeData?.(node.id, { implementation: 'connector' })
        });
      }
    }

    // User Task Diagnostics
    if (node.type === 'userTask') {
      if (!data.assignee && asTextList(data.candidateUsers).length === 0) {
        diagnostics.push({
          severity: 'warning',
          message: 'No assignee or candidate users.',
          suggestion: 'The task might get stuck if no one can claim it.',
        });
      }
    }

    // Gateway Diagnostics
    if (node.type === 'exclusiveGateway' || node.type === 'inclusiveGateway') {
      if (!data.defaultFlow) {
        diagnostics.push({
          severity: 'warning',
          message: 'No default path selected.',
          suggestion: 'If no conditions match, the process will stop here.',
        });
      }
    }

    // Script Task Diagnostics
    if (node.type === 'scriptTask') {
      if (!data.script) {
        diagnostics.push({
          severity: 'error',
          message: 'Script content is empty.',
          suggestion: 'Provide a valid script to execute.',
        });
      }
    }

    // Timer Event Diagnostics
    if (node.type === 'intermediateCatchEvent' && data.timerType === 'duration' && !data.duration) {
      diagnostics.push({
        severity: 'error',
        message: 'Timer duration is missing.',
        suggestion: 'Set a wait time (e.g., PT1H for 1 hour).',
        quickFix: () => updateNodeData?.(node.id, { duration: 'PT5M' })
      });
    }
  }

  if (edge) {
    const data = edge.data || {};
    if (!edge.label && data.condition) {
       diagnostics.push({
         severity: 'info',
         message: 'Flow has condition but no label.',
         suggestion: 'Adding a label (e.g., "Yes") makes the diagram easier to read.',
         quickFix: () => updateEdgeData?.(edge.id, 'Condition Path', data)
       });
    }
  }

  if (diagnostics.length === 0) {
    return (
      <Alert color="green" icon={<CheckCircle2 size={16} />} title="All Good!">
        <Text size="sm">No configuration issues detected for this element.</Text>
      </Alert>
    );
  }

  return (
    <Stack gap="md">
      <Group gap="xs">
        <ThemeIcon variant="light" color="orange">
          <Lightbulb size={18} />
        </ThemeIcon>
        <Text fw={700} size="md">Smart Suggestions</Text>
      </Group>

      {diagnostics.map((diag, i) => (
        <Paper key={i} withBorder p="md" radius="md" bg={diag.severity === 'error' ? 'red.0' : 'orange.0'}>
          <Stack gap="xs">
            <Group justify="space-between" align="flex-start">
              <Group gap="xs" style={{ flex: 1 }}>
                <ThemeIcon size="sm" variant="transparent" color={diag.severity === 'error' ? 'red' : 'orange'}>
                  <AlertCircle size={16} />
                </ThemeIcon>
                <Text size="sm" fw={700} c={diag.severity === 'error' ? 'red.9' : 'orange.9'}>
                  {diag.message}
                </Text>
              </Group>
              <Badge size="xs" color={diag.severity === 'error' ? 'red' : 'orange'}>
                {diag.severity}
              </Badge>
            </Group>
            
            <Text size="xs" c="dimmed">{diag.suggestion}</Text>
            
            {diag.quickFix && (
              <Button 
                variant="light" 
                size="compact-xs" 
                color="indigo" 
                mt="xs"
                leftSection={<Zap size={12} />}
                onClick={diag.quickFix}
                rightSection={<ArrowRight size={12} />}
              >
                Apply Quick Fix
              </Button>
            )}
          </Stack>
        </Paper>
      ))}
    </Stack>
  );
}
