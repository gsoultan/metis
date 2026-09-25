import { useMemo } from 'react';
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
  Alert,
  Modal,
  Grid,
  Title,
  Code as MantineCode,
  Card,
  ThemeIcon,
  Container,
  Tabs,
  Switch,
  Paper,
} from '@mantine/core';
import {
  Settings,
  LayoutGrid,
  Trash2,
  Info,
  Code,
  AlertCircle,
  Play,
  History,
} from 'lucide-react';
import { type Node, type Edge } from '@xyflow/react';
import { SmartTroubleshooter } from './SmartTroubleshooter';
import { BusinessTimeline } from './BusinessTimeline';
import { HelpTooltip, VisualConditionBuilder } from './LowCodeComponents';
import { useAppStore } from '../store/useAppStore';
import { DataFlowPanel } from './properties/DataFlowPanel';
import { CONFIG_REGISTRY } from './properties/nodeConfigRegistry';
import { PropertySection } from './properties/PropertySection';
import { RawSchemaEditor } from './RawSchemaEditor';
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
}: PropertyPanelProps) {
  const expertMode = useAppStore((state) => state.expertMode);
  const setExpertMode = useAppStore((state) => state.setExpertMode);

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
  const subtitle = selectedNode
    ? nodeVocab
      ? `${expertMode ? nodeVocab.bpmnName : nodeVocab.plainName} — ${nodeVocab.whatItDoes}`
      : String(selectedNode.type)
    : 'The path between two steps. Add a condition to control when it is taken.';

  return (
    <Modal
      opened={!!selectedNode || !!selectedEdge}
      onClose={onClose}
      title={
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md" size="xl">
            <Settings size={24} />
          </ThemeIcon>
          <Box style={{ flex: 1 }}>
            <Title order={4}>{title}</Title>
            <Text size="xs" c="dimmed">{subtitle}</Text>
          </Box>
          <Group gap="xs" mr="xl">
             {/* The same flag as the switch in the account menu, under the
                 same name. This panel covers the whole screen, header and all,
                 so it carries its own. It was labelled "BPMN names", which is
                 only one of the things the flag changes: it also shows the raw
                 schema and the API example, and makes advanced settings
                 editable. */}
             <Switch
                label="Expert mode"
                checked={expertMode}
                onChange={(event) => setExpertMode(event.currentTarget.checked)}
                size="xs"
                color="indigo"
             />
          </Group>
        </Group>
      }
      fullScreen
      padding={0}
      radius={0}
      transitionProps={{ transition: 'fade', duration: 200 }}
      styles={{
        header: {
          borderBottom: '1px solid var(--mantine-color-default-border)',
          padding: 'var(--mantine-spacing-xl)',
          margin: 0,
        },
        content: {
          display: 'flex',
          flexDirection: 'column',
          backgroundColor: 'var(--mantine-color-gray-0)',
        },
        body: {
          flex: 1,
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          padding: 0,
        }
      }}
    >
      <ScrollArea style={{ flex: 1 }} scrollbarSize={6}>
        <Container fluid px="xl" py="xl">
          <Grid gap="xl">
            {/* Column 1: Configuration & General */}
            <Grid.Col span={{ base: 12, md: 8 }}>
              <Stack gap="lg">
                <Card withBorder radius="md" p="xl" shadow="sm">
                  <Grid gap="xl">
                    <Grid.Col span={{ base: 12, md: 5 }}>
                      <Stack gap="md">
                        <Group gap="xs" mb="xs">
                          <ThemeIcon variant="light" color="indigo">
                            <Info size={18} />
                          </ThemeIcon>
                          <Text fw={700} size="lg">General Info</Text>
                        </Group>
                        

                        {selectedNode && (
                          <Stack gap="md">
                            <TextInput
                              label="Name"
                              placeholder="e.g. Approve the expense"
                              description="What this step is called, wherever it appears — on the diagram, in someone's task list, in the history."
                              size="md"
                              value={selectedNode.data.label as string || ''}
                              onChange={(e) => updateNodeData(selectedNode.id, { label: e.target.value })}
                            />
                            <Textarea
                              label="Notes"
                              placeholder="Anything the next person needs to know about this step"
                              description="Shown to whoever opens this step later. Optional."
                              size="md"
                              minRows={3}
                              value={selectedNode.data.documentation as string || ''}
                              onChange={(e) => updateNodeData(selectedNode.id, { documentation: e.target.value })}
                            />

                            {/* The id identifies the step to the engine and to
                                anything integrating over the API. It is not
                                something to fill in, so it sits at the end
                                rather than being the first thing read. */}
                            <Group gap={6} align="baseline">
                              <Text size="xs" c="dimmed">Reference</Text>
                              <MantineCode>{selectedNode.id}</MantineCode>
                              <Badge size="xs" variant="light" color="gray" radius="sm">
                                {selectedNode.type}
                              </Badge>
                            </Group>

                            <Divider variant="dashed" />

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
                          </Stack>
                        )}

                        {selectedEdge && (
                          <Stack gap="md">
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
                              size="md"
                              description={
                                'One "=" and no quotes: approvalLevel = director. ' +
                                'Writing == looks more like code and never matches, ' +
                                'so the path is silently never taken. Leave empty to always take it. ' +
                                'This text is what the arrow shows on the canvas.'
                              }
                              value={selectedEdge.data?.condition as string || ''}
                              onChange={(e) => updateEdgeData(selectedEdge.id, { ...selectedEdge.data, condition: e.target.value })}
                            />
                            <Textarea
                              label="Documentation"
                              placeholder="Why is this flow here?"
                              description="Explain the purpose of this sequence flow"
                              size="md"
                              minRows={4}
                              value={selectedEdge.data?.documentation as string || ''}
                              onChange={(e) => updateEdgeData(selectedEdge.id, { ...selectedEdge.data, documentation: e.target.value })}
                            />
                          </Stack>
                        )}
                      </Stack>
                    </Grid.Col>

                    <Grid.Col span={{ base: 12, md: 7 }}>
                      <Stack gap="md">
                        <Group gap="xs" mb="xs">
                          <ThemeIcon variant="light" color="teal">
                            <LayoutGrid size={18} />
                          </ThemeIcon>
                          <Text fw={700} size="lg">Configuration</Text>
                        </Group>
                        
                        <Divider variant="dashed" mb="sm" />
                        
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
                        ) : (
                          <Box py="xl" style={{ textAlign: 'center' }}>
                            <Text size="sm" c="dimmed">No advanced configuration for connection flows.</Text>
                          </Box>
                        )}
                      </Stack>
                    </Grid.Col>
                  </Grid>
                </Card>
              </Stack>
            </Grid.Col>

            {/* Column 2: API & Advanced */}
            <Grid.Col span={{ base: 12, md: 4 }}>
              <Stack gap="lg">
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

                {expertMode && (
                  <Card withBorder radius="md" p="xl" shadow="sm">
                    <Stack gap="md">
                      <Group gap="xs" mb="xs">
                        <ThemeIcon variant="light" color="orange">
                          <Code size={18} />
                        </ThemeIcon>
                        <Text fw={700} size="lg">Raw Schema</Text>
                      </Group>
                      
                      <Text size="xs" c="dimmed">Underlying JSON structure of this element</Text>
                      
                      <RawSchemaEditor
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
                      
                      <Alert color="orange" icon={<AlertCircle size={16} />} py="xs">
                        <Text size="10px" fw={500}>Caution: Manual JSON modification may cause unexpected behavior if properties are invalid.</Text>
                      </Alert>
                    </Stack>
                  </Card>
                )}
                
                {!expertMode && (
                  <Paper withBorder p="xl" radius="md" bg="blue.0" style={{ borderStyle: 'dashed' }}>
                    <Stack gap="xs" align="center" py="md">
                      <Info size={32} color="var(--mantine-color-blue-4)" />
                      <Text fw={700} ta="center">Simplified view</Text>
                      <Text size="xs" c="dimmed" ta="center">
                        Advanced settings appear here only once a step uses them. Turn on Expert mode at the top
                        of this panel to change them, and to see the raw schema and an API example.
                      </Text>
                    </Stack>
                  </Paper>
                )}
              </Stack>
            </Grid.Col>
          </Grid>
        </Container>
      </ScrollArea>

      <Box p="xl" bg="white" style={{ borderTop: '1px solid var(--mantine-color-default-border)' }}>
        <Container fluid px="xl">
          <Group justify="space-between">
            <Button 
              variant="light" 
              color="red" 
              size="md"
              leftSection={<Trash2 size={18} />}
              onClick={() => {
                  onDelete();
                  onClose();
              }}
            >
              Delete Element
            </Button>
            <Group>
              <Button variant="default" size="md" onClick={onClose}>Discard</Button>
              <Button color="indigo" size="md" onClick={onClose}>Apply Changes</Button>
            </Group>
          </Group>
        </Container>
      </Box>
    </Modal>
  );
}

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
