import { useMemo, useState } from 'react';
import {
  Stack,
  Group,
  Text,
  ScrollArea,
  Badge,
  TextInput,
  Textarea,
  Box,
  Button,
  Divider,
  Modal,
  Grid,
  Code as MantineCode,
  Card,
  ThemeIcon,
  Container,
  Tabs,
  Switch,
  ActionIcon,
  CloseButton,
  Tooltip,
} from '@mantine/core';
import {
  Settings,
  Trash2,
  Play,
  History,
  Maximize2,
  Minimize2,
  GitCommitHorizontal,
} from 'lucide-react';
import classes from './PropertyPanel.module.css';
import { type Node, type Edge } from '@xyflow/react';
import { SmartTroubleshooter } from './SmartTroubleshooter';
import { BusinessTimeline } from './BusinessTimeline';
import { HelpTooltip, VisualConditionBuilder } from './LowCodeComponents';
import { useAppStore } from '../store/useAppStore';
import { DataFlowPanel } from './properties/DataFlowPanel';
import { CONFIG_REGISTRY } from './properties/nodeConfigRegistry';
import { PropertySection } from './properties/PropertySection';
import { RawSchemaCard } from './RawSchemaCard';
import { computeDataFlow, sampleDataOf } from '../domain/dataFlow';
import { ApiExample } from './properties/CommonProperties';
import { vocabularyFor } from '../domain/bpmnVocabulary';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';

/**
 * FE-ARCH-10: Typed node configuration registry.
 *
 * NodeConfigProps is the base contract every property panel component must follow.
 * Components that need extra context (e.g. GatewayConfig needs edges, CallActivityConfig
 * needs nodeId) extend this interface with optional fields.
 */
 
export interface NodeConfigProps {
  data: BPMNNodeData;
  onUpdate: (data: Partial<BPMNNodeData>) => void;
  /** Provided to GatewayConfig for outgoing-flow condition editing. */
  selectedNode?: Node;
  /** Provided to GatewayConfig for outgoing-flow condition editing. */
  edges?: Edge[];
  /** Provided to SubProcessConfig, which has to count the steps drawn inside it. */
  nodes?: Node<BPMNNodeData>[];
  /** Provided to CallActivityConfig for sub-process lookup. */
  nodeId?: string;
  /** Provided to CallActivityConfig for sub-process instance viewing. */
  instanceId?: string | null;
  /** Provided to CallActivityConfig for sub-process instance viewing. */
  onViewInstance?: (id: string, defId: string) => void;
}

interface PropertyPanelProps {
  selectedNode: Node<BPMNNodeData> | null;
  selectedEdge: Edge<BPMNEdgeData> | null;
  onClose: () => void;
  onDelete: () => void;
  updateNodeData: (id: string, data: Partial<BPMNNodeData>) => void;
  updateEdgeData: (id: string, data: Partial<BPMNEdgeData>) => void;
  edges?: Edge[];
  /** The whole diagram, so the panel can trace what data reaches this step. */
  nodes?: Node<BPMNNodeData>[];
  instanceId?: string | null;
  onViewInstance?: (id: string, defId: string) => void;
  /** A card floating over the canvas, its sections in tabs, rather than a docked column. */
  floating?: boolean;
}


export function PropertyPanel({
  selectedNode,
  selectedEdge,
  onClose,
  onDelete,
  updateNodeData,
  updateEdgeData,
  edges = [],
  nodes = [],
  instanceId = null,
  onViewInstance,
  floating = false,
}: PropertyPanelProps) {
  const expertMode = useAppStore((state) => state.expertMode);
  const setExpertMode = useAppStore((state) => state.setExpertMode);
  // Docked beside the canvas by default; full screen only when asked for, for
  // the steps whose settings want the room (a connector's mapping, a script).
  const [expanded, setExpanded] = useState(false);

  // Recomputed as the diagram changes: what reaches each step depends on every
  // step before it, so editing one changes the answer for the rest.
  const dataFlow = useMemo(
    () => computeDataFlow(nodes, edges as Edge<BPMNEdgeData>[]),
    [nodes, edges],
  );
  const hasSample = useMemo(() => Object.keys(sampleDataOf(nodes)).length > 0, [nodes]);
  if (!selectedNode && !selectedEdge) return null;

  // The heading names the thing the user is editing, not the notation. It
  // read "Node Properties: Approve" — "Node Properties" is a category from the
  // spec, and the person editing it just wants to know what this step does.
  const nodeVocab = selectedNode ? vocabularyFor(String(selectedNode.type)) : undefined;
  const title = selectedNode
    ? String(selectedNode.data.label || selectedNode.id)
    : String(selectedEdge?.label || selectedEdge?.id || 'Connection');
  const kindName = selectedNode
    ? nodeVocab
      ? expertMode ? nodeVocab.bpmnName : nodeVocab.plainName
      : String(selectedNode.type)
    : 'Path';
  const subtitle = selectedNode
    ? nodeVocab?.whatItDoes ?? ''
    : 'The path between two steps. Add a condition to control when it is taken.';
  const elementWord = selectedNode ? 'step' : 'path';

  const header = (
    <Group gap="sm" wrap="nowrap" align="flex-start" style={{ flex: 1, minWidth: 0 }}>
      <ThemeIcon variant="light" color={selectedNode ? 'indigo' : 'orange'} radius="md" size="lg">
        {selectedNode ? <Settings size={18} /> : <GitCommitHorizontal size={18} />}
      </ThemeIcon>
      <Box style={{ flex: 1, minWidth: 0 }}>
        <Group gap={6} wrap="nowrap">
          <Badge size="xs" variant="light" color="gray" radius="sm" style={{ flexShrink: 0 }}>
            {kindName}
          </Badge>
        </Group>
        <Text fw={700} size={expanded ? 'lg' : 'md'} truncate mt={2}>
          {title}
        </Text>
        <Text size="xs" c="dimmed" lineClamp={expanded ? 1 : 2}>
          {subtitle}
        </Text>
      </Box>
    </Group>
  );

  const headerControls = (
    <Group gap={4} wrap="nowrap">
      <Tooltip label={expanded ? 'Back beside the diagram' : 'Open in full screen'}>
        <ActionIcon
          variant="subtle"
          color="gray"
          aria-label={expanded ? 'Dock the settings beside the diagram' : 'Open the settings in full screen'}
          onClick={() => setExpanded((open) => !open)}
        >
          {expanded ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
        </ActionIcon>
      </Tooltip>
      <CloseButton aria-label={`Close the ${elementWord} settings`} onClick={onClose} />
    </Group>
  );

  // The same flag as the switch in the account menu, under the same name. It
  // shows the BPMN names, the raw schema and the API example, and makes
  // advanced settings editable.
  const expertSwitch = (
    <Switch
      label="Expert mode"
      checked={expertMode}
      onChange={(event) => setExpertMode(event.currentTarget.checked)}
      size="xs"
      color="indigo"
    />
  );

  const about = (
    <Card withBorder radius="md" p="md">
      <PropertySection title={selectedNode ? 'About this step' : 'About this path'}>
        {selectedNode && (
          <>
            <TextInput
              label="Name"
              placeholder="e.g. Approve the expense"
              description="What this step is called, wherever it appears — on the diagram, in someone's task list, in the history."
              value={selectedNode.data.label as string || ''}
              onChange={(e) => updateNodeData(selectedNode.id, { label: e.target.value })}
              data-autofocus
            />
            <Textarea
              label="Notes"
              placeholder="Anything the next person needs to know about this step"
              description="Shown to whoever opens this step later. Optional."
              autosize
              minRows={2}
              maxRows={6}
              value={selectedNode.data.documentation as string || ''}
              onChange={(e) => updateNodeData(selectedNode.id, { documentation: e.target.value })}
            />

            {/* The id identifies the step to the engine and to anything
                integrating over the API. It is not something to fill in, so it
                sits at the end rather than being the first thing read. */}
            <Group gap={6} align="baseline">
              <Text size="xs" c="dimmed">Reference</Text>
              <MantineCode>{selectedNode.id}</MantineCode>
              <Badge size="xs" variant="light" color="gray" radius="sm">
                {selectedNode.type}
              </Badge>
            </Group>
          </>
        )}

        {selectedEdge && (
          <>
            {/*
              There is no separate "label" field here any more.
              A sequence flow has no name on the server, so a
              typed caption could never be saved — and the save
              mapper used to fall back to it as the *condition*,
              which deployed a path captioned "Yes" with the
              unbound condition `Yes`: never true, never taken,
              no warning. The arrow is captioned with its
              condition instead.
            */}
            <TextInput
              label="Take this path when"
              placeholder="e.g. approvalLevel = director"
              description={
                'One "=" and no quotes: approvalLevel = director. ' +
                'Writing == looks more like code and never matches, ' +
                'so the path is silently never taken. Leave empty to always take it. ' +
                'This text is what the arrow shows on the canvas.'
              }
              value={selectedEdge.data?.condition as string || ''}
              onChange={(e) => updateEdgeData(selectedEdge.id, { ...selectedEdge.data, condition: e.target.value })}
              data-autofocus
            />
            <Textarea
              label="Documentation"
              placeholder="Why is this flow here?"
              description="Explain the purpose of this sequence flow"
              autosize
              minRows={2}
              maxRows={6}
              value={selectedEdge.data?.documentation as string || ''}
              onChange={(e) => updateEdgeData(selectedEdge.id, { ...selectedEdge.data, documentation: e.target.value })}
            />
          </>
        )}
      </PropertySection>
    </Card>
  );

  const configuration = (
    <Card withBorder radius="md" p="md">
      {/* No heading of its own: each step's settings bring their own sections. */}
      <Stack gap="sm">
        {selectedNode ? (
          <NodeConfigSection
            selectedNode={selectedNode}
            updateNodeData={updateNodeData}
            edges={edges}
            nodes={nodes}
            instanceId={instanceId}
            onViewInstance={onViewInstance}
          />
        ) : selectedEdge ? (
          <EdgeConfigSection
            selectedEdge={selectedEdge}
            updateEdgeData={updateEdgeData}
          />
        ) : null}
      </Stack>
    </Card>
  );

  const data = selectedNode && (
    <Card withBorder radius="md" p="md">
      <PropertySection
        title="Data"
        hint="What this step receives, and what it leaves for the steps after it."
      >
        <DataFlowPanel
          flow={dataFlow.get(selectedNode.id)}
          hasSample={hasSample}
          readsOnly={String(selectedNode.type).toLowerCase().includes('gateway')}
        />
      </PropertySection>
    </Card>
  );

  const checks = (
    <Stack gap="md">
      <SmartTroubleshooter
        node={selectedNode ?? undefined}
        edge={selectedEdge ?? undefined}
        updateNodeData={updateNodeData}
        updateEdgeData={updateEdgeData}
      />

      {expertMode && selectedNode && (
        <ApiExample
          type={selectedNode.type as string}
          id={selectedNode.id}
          data={selectedNode.data}
        />
      )}

      <RawSchemaCard
        key={selectedNode?.id ?? selectedEdge?.id}
        settings={selectedNode ? selectedNode.data : selectedEdge?.data ?? {}}
        onApply={(patch) => {
          if (selectedNode) {
            updateNodeData(selectedNode.id, patch as Partial<BPMNNodeData>);
          } else if (selectedEdge) {
            updateEdgeData(selectedEdge.id, patch as Partial<BPMNEdgeData>);
          }
        }}
      />
    </Stack>
  );

  /*
    One layout for both places the settings appear. The grid answers to its own
    width, not the window's: beside the diagram it is one column, in full
    screen it is the form with the checks beside it.
  */
  const body = (
    <Grid type="container" breakpoints={CONTAINER_BREAKPOINTS} gap="md">
      <Grid.Col span={{ base: 12, md: 8 }}>
        <Stack gap="md">
          {about}
          {configuration}
          {data}
        </Stack>
      </Grid.Col>
      <Grid.Col span={{ base: 12, md: 4 }}>{checks}</Grid.Col>
    </Grid>
  );

  /*
    The edits are live: every keystroke changes the diagram, and Undo takes it
    back. The footer used to offer "Discard" and "Apply Changes", and both only
    closed the panel — a Discard that kept everything it was asked to throw
    away. It now says what actually happens.
  */
  const footer = (
    <Group justify="space-between" wrap="nowrap" gap="sm">
      <Button
        variant="subtle"
        color="red"
        size="sm"
        leftSection={<Trash2 size={16} />}
        style={{ flexShrink: 0 }}
        onClick={() => {
          onDelete();
          onClose();
        }}
      >
        Delete {elementWord}
      </Button>
      <Group gap="sm" wrap="nowrap">
        <Text size="xs" c="dimmed" visibleFrom="xs" style={{ whiteSpace: 'nowrap' }}>
          Changes apply as you type
        </Text>
        <Button color="indigo" size="sm" onClick={onClose}>
          Done
        </Button>
      </Group>
    </Group>
  );

  if (expanded) {
    return (
      <Modal
        opened
        onClose={onClose}
        withCloseButton={false}
        fullScreen
        padding={0}
        radius={0}
        transitionProps={{ transition: 'fade', duration: 150 }}
        aria-label={`Settings for ${title}`}
        styles={{
          content: { display: 'flex', flexDirection: 'column', backgroundColor: 'var(--mantine-color-gray-0)' },
          body: { flex: 1, overflow: 'hidden', display: 'flex', flexDirection: 'column', padding: 0 },
        }}
      >
        <Box px="xl" py="md" className={classes.header}>
          <Group justify="space-between" wrap="nowrap" align="flex-start">
            {header}
            <Group gap="md" wrap="nowrap">
              {expertSwitch}
              {headerControls}
            </Group>
          </Group>
        </Box>
        <ScrollArea style={{ flex: 1 }} scrollbarSize={6}>
          <Container size="xl" py="xl">{body}</Container>
        </ScrollArea>
        <Box px="xl" py="md" className={classes.footer}>
          <Container size="xl">{footer}</Container>
        </Box>
      </Modal>
    );
  }

  if (floating) {
    /*
      The floating card shows one part at a time. A step's settings run to
      four sections, and as one scroll the checks — the part that says what is
      wrong — were the last thing reached. Here each is one click away.
    */
    return (
      <Box component="aside" aria-label={`Settings for ${title}`} className={classes.floating}>
        <Box px="md" pt="md" pb="xs" className={classes.header}>
          <Group justify="space-between" wrap="nowrap" align="flex-start" gap="xs">
            {header}
            {headerControls}
          </Group>
        </Box>
        <Tabs defaultValue="about" keepMounted={false} className={classes.tabs}>
          <Tabs.List grow px="xs">
            <Tabs.Tab value="about">About</Tabs.Tab>
            <Tabs.Tab value="setup">{selectedNode ? 'Setup' : 'Condition'}</Tabs.Tab>
            {selectedNode && <Tabs.Tab value="data">Data</Tabs.Tab>}
            <Tabs.Tab value="checks">Checks</Tabs.Tab>
          </Tabs.List>
          <ScrollArea style={{ flex: 1 }} scrollbarSize={6}>
            <Box p="md">
              <Tabs.Panel value="about">{about}</Tabs.Panel>
              <Tabs.Panel value="setup">{configuration}</Tabs.Panel>
              {selectedNode && <Tabs.Panel value="data">{data}</Tabs.Panel>}
              <Tabs.Panel value="checks">
                <Stack gap="md">
                  <Group justify="space-between">
                    <Text size="xs" c="dimmed">Expert settings</Text>
                    {expertSwitch}
                  </Group>
                  {checks}
                </Stack>
              </Tabs.Panel>
            </Box>
          </ScrollArea>
        </Tabs>
        <Box px="md" py="sm" className={classes.footer}>
          {footer}
        </Box>
      </Box>
    );
  }

  return (
    <Box component="aside" aria-label={`Settings for ${title}`} className={classes.panel}>
      <Box p="md" className={classes.header}>
        <Group justify="space-between" wrap="nowrap" align="flex-start" gap="xs">
          {header}
          {headerControls}
        </Group>
        <Group mt="sm">{expertSwitch}</Group>
      </Box>
      <ScrollArea style={{ flex: 1 }} scrollbarSize={6} className={classes.body}>
        <Box p="md">{body}</Box>
      </ScrollArea>
      <Box px="md" py="sm" className={classes.footer}>
        {footer}
      </Box>
    </Box>
  );
}

/**
 * Container widths for the settings grid. Two columns only once there is room
 * for a form and the checks beside it — never in the docked panel.
 */
const CONTAINER_BREAKPOINTS = { xs: '320px', sm: '480px', md: '820px', lg: '1100px', xl: '1400px' };

function NodeConfigSection({ 
  selectedNode, 
  updateNodeData, 
  edges,
  nodes,
  instanceId,
  onViewInstance,
}: { 
  selectedNode: Node<BPMNNodeData>, 
  updateNodeData: (id: string, data: Partial<BPMNNodeData>) => void, 
  edges: Edge[],
  /** The whole canvas, so a sub-process can count the steps drawn inside it. */
  nodes?: Node<BPMNNodeData>[],
  instanceId?: string | null,
  onViewInstance?: (id: string, defId: string) => void,
}) {
  const type = selectedNode.type || '';
  const ConfigComponent = CONFIG_REGISTRY[type];

  const configContent = ConfigComponent ? (
    <ConfigComponent 
      data={selectedNode.data} 
      onUpdate={(d) => updateNodeData(selectedNode.id, d)}
      selectedNode={selectedNode}
      edges={edges}
      nodes={nodes}
      instanceId={instanceId}
      onViewInstance={onViewInstance}
      nodeId={selectedNode.id}
    />
  ) : (
    <Box py="xl" style={{ textAlign: 'center' }}>
       {['terminateEndEvent'].includes(type) ? (
          <>
            <Text size="sm" fw={700} c="indigo">This is a specialized end event.</Text>
            <Text size="xs" c="dimmed">No additional configuration required for this element.</Text>
          </>
       ) : ['subProcess', 'pool', 'lane'].includes(type) ? (
          <>
            <Text size="sm" fw={700} c="indigo">Container Element</Text>
            <Text size="xs" c="dimmed">Use the general info section to change the label of this container.</Text>
          </>
       ) : (
          <Text size="sm" c="dimmed">This step has nothing to configure — it does its job as soon as the process reaches it.</Text>
       )}
    </Box>
  );

  if (!instanceId) return configContent;

  return (
    <Tabs defaultValue="settings" variant="outline" radius="md">
      <Tabs.List mb="md">
        <Tabs.Tab value="settings" leftSection={<Settings size={14} />}>Settings</Tabs.Tab>
        <Tabs.Tab value="activity" leftSection={<History size={14} />}>Activity History</Tabs.Tab>
      </Tabs.List>

      <Tabs.Panel value="settings">
        {configContent}
      </Tabs.Panel>

      <Tabs.Panel value="activity">
        <BusinessTimeline instanceId={instanceId} />
      </Tabs.Panel>
    </Tabs>
  );
}

function EdgeConfigSection({ 
  selectedEdge, 
  updateEdgeData 
}: { 
  selectedEdge: Edge, 
  updateEdgeData: (id: string, data: Partial<BPMNEdgeData>) => void 
}) {
  const data = selectedEdge.data || {};

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <Settings size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Sequence Flow Properties</Text>
        </Group>

        <Text size="sm" c="dimmed">
          The arrow shows the condition below, so the canvas always says what
          decides this path. A flow has no separate name to save.
        </Text>
      </Stack>

      <Divider variant="dashed" />

      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="orange" radius="md">
            <Play size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Flow Condition</Text>
          <HelpTooltip label="Define when this path should be taken. If empty, it's always followed (or acts as default)." />
        </Group>
        
        <VisualConditionBuilder 
          condition={typeof data.condition === 'string' ? data.condition : ''} 
          onChange={(c) => updateEdgeData(selectedEdge.id, { ...data, condition: c })} 
        />
      </Stack>
    </Stack>
  );
}
