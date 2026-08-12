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
  Checkbox,
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
import { UserTaskConfig } from './properties/UserTaskConfig';
import { ManualTaskConfig } from './properties/ManualTaskConfig';
import { BusinessRuleTaskConfig } from './properties/BusinessRuleTaskConfig';
import { CallActivityConfig } from './properties/CallActivityConfig';
import { ServiceTaskConfig } from './properties/ServiceTaskConfig';
import { ScriptTaskConfig } from './properties/ScriptTaskConfig';
import { EventConfig } from './properties/EventConfig';
import { GatewayConfig } from './properties/GatewayConfig';
import { ApiExample } from './properties/CommonProperties';

/**
 * FE-ARCH-10: Typed node configuration registry.
 *
 * NodeConfigProps is the base contract every property panel component must follow.
 * Components that need extra context (e.g. GatewayConfig needs edges, CallActivityConfig
 * needs nodeId) extend this interface with optional fields.
 */
 
export interface NodeConfigProps {
  data: any;
  onUpdate: (data: any) => void;
  /** Provided to GatewayConfig for outgoing-flow condition editing. */
  selectedNode?: Node;
  /** Provided to GatewayConfig for outgoing-flow condition editing. */
  edges?: Edge[];
  /** Provided to CallActivityConfig for sub-process lookup. */
  nodeId?: string;
  /** Provided to CallActivityConfig for sub-process instance viewing. */
  instanceId?: string | null;
  /** Provided to CallActivityConfig for sub-process instance viewing. */
  onViewInstance?: (id: string, defId: string) => void;
}

/** Node type string → property config component. */
const CONFIG_REGISTRY: Record<string, React.ComponentType<NodeConfigProps>> = {
  userTask: UserTaskConfig,
  manualTask: ManualTaskConfig,
  businessRuleTask: BusinessRuleTaskConfig,
  callActivity: CallActivityConfig,
  serviceTask: ServiceTaskConfig,
  scriptTask: ScriptTaskConfig,
  intermediateCatchEvent: EventConfig,
  intermediateThrowEvent: EventConfig,
  boundaryEvent: EventConfig,
  signalEvent: EventConfig,
  messageEvent: EventConfig,
  timerEvent: EventConfig,
  startEvent: EventConfig,
  exclusiveGateway: GatewayConfig,
  inclusiveGateway: GatewayConfig,
  eventBasedGateway: GatewayConfig,
};

interface PropertyPanelProps {
  selectedNode: Node | null;
  selectedEdge: Edge | null;
  onClose: () => void;
  onDelete: () => void;
  updateNodeData: (id: string, data: any) => void;
  updateEdgeData: (id: string, label: string, data?: any) => void;
  edges?: Edge[];
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
  instanceId = null,
  onViewInstance,
}: PropertyPanelProps) {
  const { expertMode, setExpertMode } = useAppStore();
  if (!selectedNode && !selectedEdge) return null;

  const title = selectedNode 
    ? `Node Properties: ${selectedNode.data.label || selectedNode.id}` 
    : `Connection Properties: ${selectedEdge?.label || selectedEdge?.id}`;

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
            <Title order={3}>{title}</Title>
            <Text size="xs" c="dimmed" fw={500}>Configure parameters, execution logic, and view API details</Text>
          </Box>
          <Group gap="xs" mr="xl">
             <Text size="xs" fw={700} c={expertMode ? "indigo" : "dimmed"}>Expert Mode</Text>
             <Checkbox 
                checked={expertMode} 
                onChange={(e) => setExpertMode(e.currentTarget.checked)}
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
                        
                        <Box>
                           <Text size="xs" fw={700} c="dimmed" tt="uppercase" mb={8}>Element Metadata</Text>
                           <Group gap="xs">
                             <Badge variant="filled" color="indigo" size="lg" radius="sm">
                                {selectedNode?.type || 'sequenceFlow'}
                             </Badge>
                             <MantineCode style={{ padding: '4px 8px' }}>{selectedNode?.id || selectedEdge?.id}</MantineCode>
                           </Group>
                        </Box>

                        <Divider variant="dashed" />

                        {selectedNode && (
                          <Stack gap="md">
                            <TextInput
                              label="Name / Label"
                              placeholder="Enter node name"
                              description="The human-readable name of this node"
                              size="md"
                              value={selectedNode.data.label as string || ''}
                              onChange={(e) => updateNodeData(selectedNode.id, { label: e.target.value })}
                            />
                            <Textarea
                              label="Documentation"
                              placeholder="Describe what this node does..."
                              description="Additional details or implementation notes"
                              size="md"
                              minRows={4}
                              value={selectedNode.data.documentation as string || ''}
                              onChange={(e) => updateNodeData(selectedNode.id, { documentation: e.target.value })}
                            />
                          </Stack>
                        )}

                        {selectedEdge && (
                          <Stack gap="md">
                            <TextInput
                              label="Label"
                              placeholder="e.g. Yes / No"
                              description="Text displayed on the flow arrow"
                              size="md"
                              value={selectedEdge.label as string || ''}
                              onChange={(e) => updateEdgeData(selectedEdge.id, e.target.value)}
                            />
                            <TextInput
                              label="Condition Expression"
                              placeholder="e.g. status == 'approved'"
                              size="md"
                              description="JS expression that returns a boolean"
                              value={selectedEdge.data?.condition as string || ''}
                              onChange={(e) => updateEdgeData(selectedEdge.id, selectedEdge.label as string, { ...selectedEdge.data, condition: e.target.value })}
                            />
                            <Textarea
                              label="Documentation"
                              placeholder="Why is this flow here?"
                              description="Explain the purpose of this sequence flow"
                              size="md"
                              minRows={4}
                              value={selectedEdge.data?.documentation as string || ''}
                              onChange={(e) => updateEdgeData(selectedEdge.id, selectedEdge.label as string, { ...selectedEdge.data, documentation: e.target.value })}
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
                  node={selectedNode} 
                  edge={selectedEdge} 
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
                      
                      <Textarea
                        label="Raw Node Schema"
                        description="Modify properties directly in JSON format"
                        placeholder="Raw JSON data"
                        minRows={40}
                        autosize
                        maxRows={80}
                        styles={{ 
                          input: { 
                            fontFamily: 'monospace', 
                            fontSize: '11px', 
                            backgroundColor: 'var(--mantine-color-dark-8)',
                            color: 'var(--mantine-color-gray-3)'
                          } 
                        }}
                        value={JSON.stringify(selectedNode ? selectedNode.data : selectedEdge?.data || {}, null, 2)}
                        onChange={(e) => {
                          try {
                            const parsed = JSON.parse(e.target.value);
                            if (selectedNode) {
                              updateNodeData(selectedNode.id, parsed);
                            } else if (selectedEdge) {
                              updateEdgeData(selectedEdge.id, selectedEdge.label as string, parsed);
                            }
                          } catch {
                            // Silently ignore parse errors while typing
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
                      <Text fw={700} ta="center">Simplified View</Text>
                      <Text size="xs" c="dimmed" ta="center">Advanced technical settings and API schemas are hidden. Toggle "Expert Mode" at the top to see them.</Text>
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
  instanceId,
  onViewInstance,
}: { 
  selectedNode: Node, 
  updateNodeData: (id: string, data: any) => void, 
  edges: Edge[],
  instanceId?: string | null,
  onViewInstance?: (id: string, defId: string) => void,
}) {
  const type = selectedNode.type || '';
  const ConfigComponent = CONFIG_REGISTRY[type];

  const configContent = ConfigComponent ? (
    <ConfigComponent 
      data={selectedNode.data} 
      onUpdate={(d: any) => updateNodeData(selectedNode.id, d)}
      selectedNode={selectedNode}
      edges={edges}
      instanceId={instanceId}
      onViewInstance={onViewInstance}
      nodeId={selectedNode.id}
    />
  ) : (
    <Box py="xl" style={{ textAlign: 'center' }}>
       {['terminateEndEvent', 'errorEndEvent'].includes(type) ? (
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
          <Text size="sm" c="dimmed">No specific configuration for this node type</Text>
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
  updateEdgeData: (id: string, label: string, data?: any) => void 
}) {
  const data = selectedEdge.data || {};
  const label = selectedEdge.label as string || '';

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <Settings size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Sequence Flow Properties</Text>
        </Group>

        <TextInput
          label="Label"
          placeholder="e.g. Yes / No / Approved"
          description="Name displayed on the connection"
          size="md"
          value={label}
          onChange={(e) => updateEdgeData(selectedEdge.id, e.target.value, data)}
        />
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
          onChange={(c) => updateEdgeData(selectedEdge.id, label, { ...data, condition: c })} 
        />
      </Stack>
    </Stack>
  );
}
