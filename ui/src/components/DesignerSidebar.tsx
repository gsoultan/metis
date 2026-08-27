import React, { useState } from 'react';
import { Stack, Text, Paper, ThemeIcon, Group, Tooltip, Box, Divider, TextInput, Accordion, ActionIcon, UnstyledButton, Loader } from '@mantine/core';
import { 
  Play, 
  Square, 
  User, 
  Settings, 
  FileCode, 
  GitBranch, 
  Plus,
  Circle,
  Clock,
  Bell,
  ExternalLink,
  Hand,
  Briefcase,
  Search,
  X,
  GripVertical,
  Zap,
  AlertTriangle,
  Radio,
  Send,
  TrendingUp,
  Undo2
} from 'lucide-react';
import classes from './DesignerSidebar.module.css';
import { useConnectors } from '../hooks/useProcess';
import { useAppStore } from '../store/useAppStore';
import { NODE_VOCABULARY, type NodeKind } from '../domain/bpmnVocabulary';
import type { LucideIcon } from 'lucide-react';
import { DECIDE_GROUP_KIND } from '../domain/decideGroup';
import type { BPMNNodeData } from '../types/bpmn';

/** One draggable entry in the palette, after the vocabulary has named it. */
interface PaletteEntry {
  type: string;
  label: string;
  description?: string;
  icon: React.ComponentType<{ size?: number | string }>;
  color?: string;
  data?: Partial<BPMNNodeData>;
  /** Filled in from the vocabulary: a worked example and the BPMN term. */
  example?: string;
  alsoKnownAs?: string;
}

type DragStartHandler = (
  event: React.DragEvent,
  nodeType: string,
  initialData?: Partial<BPMNNodeData>,
) => void;

/**
 * The palette, grouped and labelled by what each thing DOES.
 *
 * Every entry used to be named for the notation — "Exclusive Gateway",
 * "Call Activity", "Boundary Event" — which meant the palette could only be
 * used by someone who already knew BPMN. Names and descriptions now come from
 * the shared vocabulary, and expert mode swaps them back to the spec terms.
 *
 * Groups follow the same logic: "Decisions and branching" rather than
 * "Gateways", because a person looking for a way to split the flow does not
 * know to look under "Gateways".
 */
const paletteItems: Array<{
  kind: NodeKind;
  type: string;
  icon: LucideIcon;
  color: string;
  data?: Record<string, unknown>;
  /** Distinguishes variants that share a node type, e.g. timer vs message. */
  variantName?: string;
  variantExample?: string;
}> = [
  { kind: 'startEvent', type: 'startEvent', icon: Play, color: 'green' },
  { kind: 'endEvent', type: 'endEvent', icon: Square, color: 'gray' },
  { kind: 'terminateEndEvent', type: 'terminateEndEvent', icon: Zap, color: 'red' },
  { kind: 'errorEndEvent', type: 'errorEndEvent', icon: AlertTriangle, color: 'red' },

  { kind: 'userTask', type: 'userTask', icon: User, color: 'blue' },
  { kind: 'serviceTask', type: 'serviceTask', icon: Settings, color: 'teal' },
  { kind: 'businessRuleTask', type: 'businessRuleTask', icon: Briefcase, color: 'indigo' },
  { kind: 'scriptTask', type: 'scriptTask', icon: FileCode, color: 'violet' },
  { kind: 'manualTask', type: 'manualTask', icon: Hand, color: 'orange' },
  { kind: 'callActivity', type: 'callActivity', icon: ExternalLink, color: 'cyan' },

  // The recommended shape, made the easy one — see domain/decideGroup.ts.
  { kind: 'decideGroup', type: DECIDE_GROUP_KIND, icon: GitBranch, color: 'indigo' },
  { kind: 'exclusiveGateway', type: 'exclusiveGateway', icon: GitBranch, color: 'orange' },
  { kind: 'parallelGateway', type: 'parallelGateway', icon: Plus, color: 'orange' },
  { kind: 'inclusiveGateway', type: 'inclusiveGateway', icon: Circle, color: 'orange' },
  { kind: 'eventBasedGateway', type: 'eventBasedGateway', icon: Zap, color: 'orange' },

  {
    kind: 'intermediateCatchEvent', type: 'intermediateCatchEvent', icon: Clock, color: 'blue',
    data: { icon: 'timer' },
    variantName: 'Wait for a time',
    variantExample: 'Wait three days for the customer to respond.',
  },
  {
    kind: 'intermediateCatchEvent', type: 'intermediateCatchEvent', icon: Bell, color: 'blue',
    data: { icon: 'signal' },
    variantName: 'Wait for a message',
    variantExample: 'Wait until the payment system confirms the transfer.',
  },
  // The throwing half of the same conversation. The engine has run these since
  // boundary events landed — an error end event is how a path reports a named
  // problem for a boundary to catch — but none of them were reachable from
  // here, so the only way to author one was to hand-write BPMN XML and import
  // it. Two variants, as with the catch event above, because "announce" means
  // a broadcast or a directed message depending on which field is filled in.
  {
    kind: 'intermediateThrowEvent', type: 'intermediateThrowEvent', icon: Radio, color: 'blue',
    data: { icon: 'signal' },
    variantName: 'Announce to anyone listening',
    variantExample: 'Tell every process watching that payment has cleared.',
  },
  {
    kind: 'intermediateThrowEvent', type: 'intermediateThrowEvent', icon: Send, color: 'blue',
    data: { icon: 'message' },
    variantName: 'Send a message to one process',
    variantExample: 'Tell the shipping process for this order that it can proceed.',
  },
  { kind: 'escalationThrowEvent', type: 'escalationThrowEvent', icon: TrendingUp, color: 'orange' },
  { kind: 'compensationThrowEvent', type: 'compensationThrowEvent', icon: Undo2, color: 'orange' },
  { kind: 'boundaryEvent', type: 'boundaryEvent', icon: Circle, color: 'orange', data: { icon: 'timer' } },

  { kind: 'subProcess', type: 'subProcess', icon: Plus, color: 'indigo' },
  { kind: 'pool', type: 'pool', icon: GripVertical, color: 'gray' },
  { kind: 'lane', type: 'lane', icon: GripVertical, color: 'gray' },
];


interface DesignerSidebarProps {
  embedded?: boolean;
}

export function DesignerSidebar({ embedded }: DesignerSidebarProps) {
  const [search, setSearch] = useState('');
  const { expertMode } = useAppStore();
  const { data: connectorsData, isLoading: connectorsLoading } = useConnectors();
  
  const onDragStart = (event: React.DragEvent, nodeType: string, initialData: Partial<BPMNNodeData> = {}) => {
    event.dataTransfer.setData('application/reactflow', nodeType);
    event.dataTransfer.setData('application/initialData', JSON.stringify(initialData));
    event.dataTransfer.effectAllowed = 'move';
  };

  const connectors = connectorsData?.connectors ?? [];
  
  const connectorItems = connectors.map((c) => ({
    type: 'serviceTask',
    label: c.name,
    description: c.description,
    icon: Zap,
    color: 'yellow',
    data: {
      label: c.name,
      implementation: 'connector',
      connector_id: c.id,
    }
  }));

  // Resolve each palette entry through the vocabulary, so one place decides
  // what everything is called.
  const resolved = paletteItems.map((entry) => {
    const vocab = NODE_VOCABULARY[entry.kind];
    const primary = expertMode ? vocab.bpmnName : (entry.variantName ?? vocab.plainName);
    const secondary = expertMode ? (entry.variantName ?? vocab.plainName) : vocab.bpmnName;
    return {
      ...entry,
      label: primary,
      alsoKnownAs: secondary,
      description: entry.variantExample ?? vocab.whatItDoes,
      example: entry.variantExample ?? vocab.example,
      group: vocab.group,
    };
  });

  const grouped = resolved.reduce<Record<string, typeof resolved>>((acc, item) => {
    (acc[item.group] ??= []).push(item);
    return acc;
  }, {});

  const allGroups = [
    ...Object.entries(grouped).map(([group, items]) => ({ group, items })),
    ...(connectorItems.length > 0 ? [{ group: 'Connectors', items: connectorItems }] : []),
  ];

  // Search matches the plain name, the BPMN name and the example, so someone
  // who knows only "gateway" and someone who only knows "choose a path" both
  // find the same thing.
  const needle = search.toLowerCase();
  const filteredItems = allGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item: Record<string, unknown>) =>
        [item.label, item.alsoKnownAs, item.description, item.example]
          .filter((v): v is string => typeof v === 'string')
          .some((v) => v.toLowerCase().includes(needle)),
      ),
    }))
    .filter((group) => group.items.length > 0);

  const content = (
    <Stack gap="md" style={{ height: '100%' }}>
      {!embedded && (
        <Box>
          <Text fw={600} size="md" mb={4}>Building blocks</Text>
          <Text size="xs" c="dimmed">Drag one onto the canvas to add a step</Text>
        </Box>
      )}
      
      <TextInput
        placeholder="Search — try “approve” or “wait”"
        size="xs"
        leftSection={<Search size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
        rightSection={
          search && (
            <ActionIcon aria-label="Remove" size="xs" variant="transparent" onClick={() => setSearch('')}>
              <X size={12} />
            </ActionIcon>
          )
        }
      />
      
      <Divider />
      
      <Box className={classes.scrollArea}>
        {search ? (
          <Stack gap="xs">
            {filteredItems.flatMap(group => group.items).map((item) => (
              <DesignerItem key={`${item.type}-${item.label}`} item={item} onDragStart={onDragStart} />
            ))}
            {filteredItems.length === 0 && (
              <Box py="xl" style={{ textAlign: 'center' }}>
                <Text size="sm" c="dimmed">Nothing matches “{search}”</Text>
              </Box>
            )}
          </Stack>
        ) : (
          <Accordion 
            multiple 
            defaultValue={['Events', 'Tasks', 'Gateways', 'Connectors']}
            variant="separated"
            classNames={{
              item: classes.accordionItem,
              control: classes.accordionControl,
              content: classes.accordionContent,
              label: classes.accordionLabel
            }}
          >
            {allGroups.map((group) => (
              <Accordion.Item key={group.group} value={group.group}>
                <Accordion.Control>
                  <Group gap="xs">
                    {group.group === 'Connectors' && connectorsLoading && <Loader size={12} />}
                    <Text size="sm" fw={600}>{group.group}</Text>
                  </Group>
                </Accordion.Control>
                <Accordion.Panel>
                  <Stack gap="xs">
                    {group.items.map((item) => (
                      <DesignerItem key={item.label} item={item} onDragStart={onDragStart} />
                    ))}
                  </Stack>
                </Accordion.Panel>
              </Accordion.Item>
            ))}
          </Accordion>
        )}
      </Box>
    </Stack>
  );

  if (embedded) {
    return (
      <Box className={classes.sidebar}>
        {content}
      </Box>
    );
  }

  return (
    <Paper 
      p="md" 
      withBorder 
      className={classes.sidebar}
      radius="lg"
      shadow="md"
    >
      {content}
    </Paper>
  );
}

function DesignerItem({ item, onDragStart }: { item: PaletteEntry; onDragStart: DragStartHandler }) {
  return (
    <Tooltip
      multiline
      w={260}
      position="right"
      withArrow
      openDelay={350}
      label={
        <Box>
          <Text size="xs" fw={600}>{item.label}</Text>
          {item.description && <Text size="xs" mt={2}>{item.description}</Text>}
          {/* A concrete example is what makes an abstract construct click. */}
          {item.example && (
            <Text size="xs" mt={6} c="blue.2">
              For example: {item.example}
            </Text>
          )}
          {item.alsoKnownAs && (
            <Text size="xs" mt={6} opacity={0.7}>
              Known in BPMN as “{item.alsoKnownAs}”
            </Text>
          )}
        </Box>
      }
    >
      <UnstyledButton
        component="div"
        draggable
        onDragStart={(event) => onDragStart(event, item.type, item.data)}
        className={classes.item}
      >
        <Group gap="sm" wrap="nowrap" align="center">
          <GripVertical size={14} color="var(--mantine-color-dimmed)" />
          <ThemeIcon color={item.color} variant="light" radius="md" size="md">
            <item.icon size={16} />
          </ThemeIcon>
          <Box style={{ flex: 1, minWidth: 0 }}>
            <Text size="sm" fw={600} truncate>{item.label}</Text>
            <Text size="xs" c="dimmed">{item.description}</Text>
          </Box>
        </Group>
      </UnstyledButton>
    </Tooltip>
  );
}
