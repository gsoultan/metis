import {
  Handle,
  Position,
  NodeToolbar,
  type NodeProps,
} from '@xyflow/react';
import { Paper, Text, Stack, Box, Group, Avatar, Badge, ActionIcon, Tooltip } from '@mantine/core';
import { 
  User, 
  Settings, 
  Play, 
  Square, 
  Plus, 
  FileCode, 
  Circle, 
  Clock, 
  Bell, 
  Zap, 
  ExternalLink,
  Hand,
  Briefcase,
  X,
  Trash2,
  ArrowRight
} from 'lucide-react';
import { vocabularyFor } from '../domain/bpmnVocabulary';
import { useAppStore } from '../store/useAppStore';

const getStatusStyles = (status?: string, heatmapValue?: number) => {
  const styles: React.CSSProperties = {};
  
  if (heatmapValue !== undefined) {
    // Heatmap: scale from light orange to dark red
    const intensity = Math.min(heatmapValue / 100, 1);
    styles.backgroundColor = `rgba(255, ${200 * (1 - intensity)}, 0, ${0.1 + intensity * 0.4})`;
    styles.borderWidth = 1 + intensity * 3;
    styles.borderColor = `rgba(255, 0, 0, ${intensity})`;
  }

  if (status === 'completed') {
    return {
      ...styles,
      outline: '4px solid var(--mantine-color-green-5)',
      outlineOffset: '2px',
      borderRadius: 'inherit',
    };
  }
  if (status === 'active') {
    return {
      ...styles,
      outline: '4px solid var(--mantine-color-blue-5)',
      outlineOffset: '2px',
      borderRadius: 'inherit',
      boxShadow: '0 0 15px var(--mantine-color-blue-5)',
    };
  }
  return styles;
};

const ContextPad = ({ selected }: { selected?: boolean }) => {
  if (!selected) return null;
  
  return (
    <NodeToolbar isVisible={selected} position={Position.Right} offset={12}>
      <Paper shadow="md" withBorder radius="xl" p={4} bg="var(--mantine-color-body)">
        <Group gap={4}>
          <Tooltip label="Next Step">
            <ActionIcon aria-label="Continue" variant="light" color="blue" radius="xl" size="sm">
              <ArrowRight size={12} />
            </ActionIcon>
          </Tooltip>
          <Tooltip label="Delete Element">
            <ActionIcon aria-label="Delete" variant="light" color="red" radius="xl" size="sm">
              <Trash2 size={12} />
            </ActionIcon>
          </Tooltip>
        </Group>
      </Paper>
    </NodeToolbar>
  );
};

/**
 * The caption under a node's name.
 *
 * Every node used to print its BPMN type in uppercase — "SERVICE TASK",
 * "EXCLUSIVE GATEWAY". A diagram is read far more often than it is edited, and
 * mostly by people who did not draw it: the manager approving the process, the
 * operator working out where an instance is stuck. Naming the notation tells
 * them nothing.
 *
 * Expert mode switches back to the specification terms.
 */
function useNodeCaption(nodeType: string): string {
  const { expertMode } = useAppStore();
  const vocab = vocabularyFor(nodeType);
  if (!vocab) return nodeType;
  return expertMode ? vocab.bpmnName : vocab.plainName;
}

/** Explains a node on hover: what it does, plus its other name. */
function NodeHelp({ nodeType, children }: { nodeType: string; children: React.ReactElement }) {
  const { expertMode } = useAppStore();
  const vocab = vocabularyFor(nodeType);
  if (!vocab) return children;
  return (
    <Tooltip
      multiline
      w={250}
      openDelay={400}
      position="top"
      withArrow
      label={
        <Box>
          <Text size="xs" fw={600}>{expertMode ? vocab.bpmnName : vocab.plainName}</Text>
          <Text size="xs" mt={2}>{vocab.whatItDoes}</Text>
          <Text size="xs" mt={4} opacity={0.75}>
            {expertMode ? vocab.plainName : `BPMN: ${vocab.bpmnName}`}
          </Text>
        </Box>
      }
    >
      {children}
    </Tooltip>
  );
}

export const StartNode = ({ data, selected }: NodeProps) => (
  <Stack align="center" gap={4} style={{ position: 'relative', ...getStatusStyles(data.status as string, data.heatmapValue as number) }}>
    <ContextPad selected={selected} />
    <Box 
      style={{ 
        width: 40, 
        height: 40, 
        borderRadius: '50%', 
        background: 'var(--mantine-color-green-0)',
        border: '1px solid var(--mantine-color-green-7)',
        display: 'flex', 
        alignItems: 'center', 
        justifyContent: 'center',
        boxShadow: 'var(--mantine-shadow-xs)'
      }}
    >
      <Play size={16} color="var(--mantine-color-green-7)" fill="var(--mantine-color-green-7)" />
      <Handle type="source" position={Position.Right} style={{ background: 'var(--mantine-color-green-7)' }} />
      <Handle type="source" position={Position.Bottom} style={{ background: 'var(--mantine-color-green-7)' }} />
    </Box>
    <Text size="xs" fw={700} style={{ position: 'absolute', top: 44, whiteSpace: 'nowrap' }}>
      {data.label as string}
    </Text>
  </Stack>
);

export const EndNode = ({ data, selected }: NodeProps) => {
  const isTerminate = data.nodeType === 'terminateEndEvent';
  const isError = data.nodeType === 'errorEndEvent';

  return (
    <Stack align="center" gap={4} style={{ position: 'relative', ...getStatusStyles(data.status as string, data.heatmapValue as number) }}>
      <ContextPad selected={selected} />
      <Box 
        style={{ 
          width: 40, 
          height: 40, 
          borderRadius: '50%', 
          background: 'var(--mantine-color-red-0)',
          border: '3px solid var(--mantine-color-red-7)',
          display: 'flex', 
          alignItems: 'center', 
          justifyContent: 'center',
          boxShadow: 'var(--mantine-shadow-xs)'
        }}
      >
        {isTerminate ? (
          <Circle size={24} color="var(--mantine-color-red-7)" fill="var(--mantine-color-red-7)" />
        ) : isError ? (
          <Zap size={20} color="var(--mantine-color-red-7)" fill="var(--mantine-color-red-7)" />
        ) : (
          <Square size={16} color="var(--mantine-color-red-7)" fill="var(--mantine-color-red-7)" />
        )}
        <Handle type="target" position={Position.Left} style={{ background: 'var(--mantine-color-red-7)' }} />
        <Handle type="target" position={Position.Top} style={{ background: 'var(--mantine-color-red-7)' }} />
      </Box>
      <Text size="xs" fw={700} style={{ position: 'absolute', top: 44, whiteSpace: 'nowrap' }}>
        {data.label as string}
      </Text>
    </Stack>
  );
};

export const IntermediateNode = ({ data, selected }: NodeProps) => {
  const Icon = data.icon === 'timer' ? Clock : (data.icon === 'signal' ? Bell : Zap);
  
  return (
    <Box 
      style={{ 
        width: 36, 
        height: 36, 
        borderRadius: '50%', 
        background: 'var(--mantine-color-blue-0)',
        border: '1px solid var(--mantine-color-blue-7)',
        display: 'flex', 
        alignItems: 'center', 
        justifyContent: 'center',
        boxShadow: 'var(--mantine-shadow-xs)',
        padding: 2,
        ...getStatusStyles(data.status as string, data.heatmapValue as number)
      }}
    >
      <ContextPad selected={selected} />
      <Box 
        style={{ 
          width: '100%', 
          height: '100%', 
          borderRadius: '50%', 
          border: '1px solid var(--mantine-color-blue-7)',
          display: 'flex', 
          alignItems: 'center', 
          justifyContent: 'center',
        }}
      >
        <Icon size={14} color="var(--mantine-color-blue-7)" />
      </Box>
      <Handle type="target" position={Position.Left} style={{ background: 'var(--mantine-color-blue-7)' }} />
      <Handle type="source" position={Position.Right} style={{ background: 'var(--mantine-color-blue-7)' }} />
    </Box>
  );
};

export const TaskNode = ({ data, selected }: NodeProps) => {
  // data.nodeType, not data.type. Nothing has ever set data.type — the mapper
  // that loads a definition and the handler that drops a new node both write
  // nodeType — so every one of these comparisons was false and every fallback
  // won. The visible effect was that every task rendered as "Call another
  // system" with a service-task gear, and every gateway as exclusive,
  // whichever kind it actually was.
  const isUserTask = data.nodeType === 'userTask';
  const isScriptTask = data.nodeType === 'scriptTask';
  const isManualTask = data.nodeType === 'manualTask';
  const isBusinessRuleTask = data.nodeType === 'businessRuleTask';
  const isCallActivity = data.nodeType === 'callActivity';
  
  let Icon = Settings;
  let accentColor = 'teal';

  if (isUserTask) {
    Icon = User;
    accentColor = 'blue';
  } else if (isScriptTask) {
    Icon = FileCode;
    accentColor = 'violet';
  } else if (isManualTask) {
    Icon = Hand;
    accentColor = 'orange';
  } else if (isBusinessRuleTask) {
    Icon = Briefcase;
    accentColor = 'indigo';
  } else if (isCallActivity) {
    Icon = ExternalLink;
    accentColor = 'cyan';
  }

  // The caption said "SERVICE TASK", "CALL ACTIVITY" — the notation, shouted.
  // Someone reading the diagram to understand the process needs to know what
  // the step does, so it now reads "Calls another system" unless expert mode
  // is on. Both names appear on hover, so the notation stays learnable.
  const typeLabel = useNodeCaption(String(data.nodeType ?? 'serviceTask'));

  return (
    <Paper 
      shadow={selected ? "xl" : "md"} 
      p="xs" 
      radius="sm" 
      withBorder
      style={{ 
        borderLeft: `5px solid var(--mantine-color-${accentColor}-6)`,
        minWidth: 160,
        backgroundColor: 'var(--mantine-color-body)',
        borderColor: selected ? `var(--mantine-color-${accentColor}-6)` : undefined,
        transition: 'all 0.2s ease',
        ...getStatusStyles(data.status as string, data.heatmapValue as number)
      }}
    >
      <ContextPad selected={selected} />
      <Handle type="target" position={Position.Left} style={{ width: 6, height: 6 }} />
      <Stack gap={4}>
        <Group gap={6} align="center" justify="space-between">
          <Group gap={4}>
            <NodeHelp nodeType={String(data.nodeType ?? 'serviceTask')}>
              <Icon size={14} color={`var(--mantine-color-${accentColor}-6)`} />
            </NodeHelp>
            <Text size="10px" c="dimmed">
              {typeLabel}
            </Text>
          </Group>
        </Group>
        <Text size="xs" fw={700} lineClamp={2}>{data.label as string}</Text>
        
        {!!data.httpUrl && (
          <Group gap={4} mt={2}>
             <Badge size="xs" color="teal" variant="light" p={4} radius="xs">{String(data.httpMethod || 'GET')}</Badge>
             <Text size="10px" c="dimmed" truncate maw={100}>{String(data.httpUrl)}</Text>
          </Group>
        )}

        {!!data.externalTopic && (
          <Group gap={4} mt={2}>
             <Badge size="xs" color="orange" variant="light" p={4} radius="xs">External</Badge>
             <Text size="10px" c="dimmed" truncate maw={100}>{String(data.externalTopic)}</Text>
          </Group>
        )}

        {!!data.connector_id && (
          <Group gap={4} mt={2}>
             <Badge size="xs" color="yellow" variant="light" p={4} radius="xs">
                <Group gap={2}>
                  <Zap size={8} />
                  <Text size="8px" fw={800}>Connector</Text>
                </Group>
             </Badge>
          </Group>
        )}

        {!!data.script && (
          <Group gap={4} mt={2}>
             <Badge size="xs" color="violet" variant="light" p={4} radius="xs">Script</Badge>
             <Text size="10px" c="dimmed" truncate>{String(data.scriptFormat || 'js')}</Text>
          </Group>
        )}

        {!!data.assignee && (
          <Group gap={4} mt={2}>
            <Avatar size={14} radius="xl" color={accentColor} src={null}>
              <User size={8} />
            </Avatar>
            <Text size="10px" c="dimmed" fw={600}>
              {String(data.assignee)}
            </Text>
          </Group>
        )}
      </Stack>
      <Handle type="source" position={Position.Right} style={{ width: 6, height: 6 }} />
    </Paper>
  );
};

export const GatewayNode = ({ data, selected }: NodeProps) => {
  const gatewayCaption = useNodeCaption(String(data.nodeType ?? 'exclusiveGateway'));
  const isExclusive = data.nodeType === 'exclusiveGateway';
  const isParallel = data.nodeType === 'parallelGateway';
  const isInclusive = data.nodeType === 'inclusiveGateway';
  const isEventBased = data.nodeType === 'eventBasedGateway';
  
  let iconElement = null;
  if (isExclusive) {
    iconElement = <X size={20} color="var(--mantine-color-orange-filled)" strokeWidth={3} />;
  } else if (isParallel) {
    iconElement = <Plus size={20} color="var(--mantine-color-orange-filled)" strokeWidth={3} />;
  } else if (isInclusive) {
    iconElement = <Circle size={18} color="var(--mantine-color-orange-filled)" strokeWidth={3} />;
  } else if (isEventBased) {
    iconElement = (
      <Box 
        style={{ 
          width: 28, 
          height: 28, 
          borderRadius: '50%', 
          border: '1px solid var(--mantine-color-orange-7)',
          display: 'flex', 
          alignItems: 'center', 
          justifyContent: 'center'
        }}
      >
        <Box 
          style={{ 
            width: 22, 
            height: 22, 
            borderRadius: '50%', 
            border: '1px solid var(--mantine-color-orange-7)',
            display: 'flex', 
            alignItems: 'center', 
            justifyContent: 'center'
          }}
        >
          <Zap size={14} color="var(--mantine-color-orange-7)" fill="var(--mantine-color-orange-7)" />
        </Box>
      </Box>
    );
  }

  return (
    <Stack align="center" gap={4} style={{ position: 'relative', ...getStatusStyles(data.status as string, data.heatmapValue as number) }}>
      <ContextPad selected={selected} />
      <Box 
        style={{ 
          width: 50, 
          height: 50, 
          position: 'relative',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Box
          style={{
            width: 50,
            height: 50,
            position: 'absolute',
            transform: 'rotate(45deg)',
            background: 'var(--mantine-color-orange-0)',
            border: `2px solid ${selected ? 'var(--mantine-color-orange-8)' : 'var(--mantine-color-orange-6)'}`,
            borderRadius: '4px',
            boxShadow: 'var(--mantine-shadow-xs)',
            transition: 'all 0.2s ease'
          }}
        />
        <NodeHelp nodeType={String(data.nodeType ?? 'exclusiveGateway')}>
          <Box style={{ zIndex: 1, position: 'relative' }}>
            {iconElement}
          </Box>
        </NodeHelp>
        <Handle type="target" position={Position.Left} style={{ left: -10, top: 25 }} />
        <Handle type="source" position={Position.Right} style={{ right: -10, top: 25 }} />
        <Handle type="target" position={Position.Top} style={{ top: -10, left: 25 }} />
        <Handle type="source" position={Position.Bottom} style={{ bottom: -10, left: 25 }} />
      </Box>
      <Stack gap={0} align="center" style={{ position: 'absolute', top: 52 }}>
        <Text size="xs" fw={600} style={{ whiteSpace: 'nowrap' }}>
          {data.label as string}
        </Text>
        {/* A diamond on a canvas is unreadable without saying what kind it is,
            and "EXCLUSIVE GATEWAY" only helps someone who knows BPMN. */}
        <Text size="10px" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
          {gatewayCaption}
        </Text>
        {!!data.defaultFlow && (
          <Badge size="xs" color="orange" variant="outline" p={2} style={{ fontSize: '8px', height: '14px' }}>
             Default: {String(data.defaultFlow).slice(0, 8)}
          </Badge>
        )}
      </Stack>
    </Stack>
  );
};

export const SubProcessNode = ({ data, selected }: NodeProps) => (
  <Paper 
    shadow={selected ? "xl" : "md"} 
    p="xl" 
    radius="lg" 
    withBorder
    style={{ 
      minWidth: 400,
      minHeight: 250,
      backgroundColor: 'rgba(var(--mantine-color-indigo-0-rgb), 0.1)',
      border: `2px dashed var(--mantine-color-indigo-4)`,
      borderColor: selected ? `var(--mantine-color-indigo-6)` : undefined,
      transition: 'all 0.2s ease',
      position: 'relative',
      ...getStatusStyles(data.status as string, data.heatmapValue as number)
    }}
  >
    <ContextPad selected={selected} />
    <Handle type="target" position={Position.Left} />
    <Box style={{ position: 'absolute', top: 10, left: 10 }}>
       <Group gap={4}>
         <Plus size={14} color="var(--mantine-color-indigo-6)" />
         <Text size="xs" fw={800} c="indigo.6" tt="uppercase">{data.label as string || 'Sub-Process'}</Text>
       </Group>
    </Box>
    <Handle type="source" position={Position.Right} />
  </Paper>
);

export const PoolNode = ({ data, selected }: NodeProps) => (
  <Paper 
    shadow={selected ? "md" : "xs"} 
    p={0} 
    radius={0} 
    withBorder
    style={{ 
      minWidth: 800,
      minHeight: 200,
      backgroundColor: 'var(--mantine-color-gray-0)',
      border: `1px solid var(--mantine-color-gray-4)`,
      borderColor: selected ? `var(--mantine-color-blue-6)` : undefined,
      display: 'flex',
      flexDirection: 'row',
      overflow: 'hidden',
      ...getStatusStyles(data.status as string, data.heatmapValue as number)
    }}
  >
    <Box style={{ 
      width: 40, 
      borderRight: '1px solid var(--mantine-color-gray-4)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: 'var(--mantine-color-gray-1)'
    }}>
       <Text style={{ transform: 'rotate(-90deg)', whiteSpace: 'nowrap' }} fw={700} size="sm">
         {data.label as string || 'Pool'}
       </Text>
    </Box>
    <Box style={{ flex: 1, position: 'relative' }}>
    </Box>
  </Paper>
);

export const LaneNode = ({ data, selected }: NodeProps) => (
  <Paper 
    shadow={selected ? "md" : "none"} 
    p={0} 
    radius={0} 
    withBorder
    style={{ 
      minWidth: 760,
      minHeight: 100,
      backgroundColor: 'transparent',
      border: `1px solid var(--mantine-color-gray-3)`,
      borderColor: selected ? `var(--mantine-color-blue-4)` : undefined,
      display: 'flex',
      flexDirection: 'row',
      ...getStatusStyles(data.status as string, data.heatmapValue as number)
    }}
  >
    <Box style={{ 
      width: 30, 
      borderRight: '1px solid var(--mantine-color-gray-3)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: 'rgba(var(--mantine-color-gray-1-rgb), 0.5)'
    }}>
       <Text style={{ transform: 'rotate(-90deg)', whiteSpace: 'nowrap' }} fw={600} size="xs">
         {data.label as string || 'Lane'}
       </Text>
    </Box>
    <Box style={{ flex: 1, position: 'relative' }}>
    </Box>
  </Paper>
);

export const BoundaryNode = ({ data, selected }: NodeProps) => {
  const Icon = data.icon === 'timer' ? Clock : (data.icon === 'error' ? X : Zap);
  return (
    <Box 
      style={{ 
        width: 30, 
        height: 30, 
        borderRadius: '50%', 
        background: 'white',
        border: `2px ${data.interrupting === false ? 'dashed' : 'solid'} var(--mantine-color-orange-7)`,
        display: 'flex', 
        alignItems: 'center', 
        justifyContent: 'center',
        boxShadow: 'var(--mantine-shadow-xs)',
      }}
    >
      <ContextPad selected={selected} />
      <Icon size={14} color="var(--mantine-color-orange-7)" />
      <Handle type="source" position={Position.Bottom} style={{ background: 'var(--mantine-color-orange-7)' }} />
    </Box>
  );
};

