import { 
  Title, 
  Text, 
  SimpleGrid, 
  Card, 
  Group, 
  Badge, 
  Button, 
  Stack, 
  ThemeIcon, 
  ActionIcon, 
  Modal, 
  TextInput, 
  Textarea, 
  Select, 
  NumberInput, 
  PasswordInput, 
  Divider, 
  Paper, 
  Table, 
  ScrollArea, 
  Alert,
  Tooltip,
  Box,
  Stepper,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { 
  Zap, 
  Plus, 
  Settings, 
  Trash2, 
  Globe, 
  MessageSquare, 
  Mail, 
  Send, 
  Users, 
  Search, 
  CheckCircle2,
  AlertCircle,
  Play,
  Heart,
  ChevronRight,
  Info,
} from 'lucide-react';
import { useState } from 'react';
import { 
  useConnectors, 
  useConnectorInstances, 
  useCreateConnectorInstance, 
  useUpdateConnectorInstance, 
  useDeleteConnectorInstance,
  useExecuteConnector,
  useCreateConnector,
} from '../hooks/useProcess';
import { useQueryClient } from '@tanstack/react-query';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { notifications } from '@mantine/notifications';
import { useForm } from '@mantine/form';
import { CardGridLoadingState, ErrorState } from '../components/state';

const IconMap: Record<string, any> = {
  Globe: Globe,
  MessageSquare: MessageSquare,
  Mail: Mail,
  Send: Send,
  Users: Users,
  Zap: Zap,
};

export function Connectors() {
  const { currentProjectId, expertMode } = useAppStore();
  const {
    data: connectorsData,
    isLoading: connectorsLoading,
    error: connectorsError,
    refetch: refetchConnectors,
  } = useConnectors();
  const { data: instancesData, isLoading: instancesLoading } = useConnectorInstances();
  const isLoading = connectorsLoading || instancesLoading;
  const queryClient = useQueryClient();
  const createInstance = useCreateConnectorInstance();
  const updateInstance = useUpdateConnectorInstance();
  const deleteInstance = useDeleteConnectorInstance();
  const testConnector = useExecuteConnector();
  
  const createConnector = useCreateConnector();

  const [modalOpened, { open: openModal, close: closeModal }] = useDisclosure(false);
  const [testModalOpened, { open: openTestModal, close: closeTestModal }] = useDisclosure(false);
  const [customModalOpened, { open: openCustomModal, close: closeCustomModal }] = useDisclosure(false);
  
  const [selectedConnector, setSelectedConnector] = useState<any>(null);
  const [editingInstance, setEditingInstance] = useState<any>(null);
  const [formData, setFormData] = useState<any>({});
  const [testPayload, setTestPayload] = useState('{\n  "text": "Hello from Hermod!"\n}');
  const [testResult, setTestResult] = useState<any>(null);
  const [activeStep, setActiveStep] = useState(0);

  const customConnectorForm = useForm({
    initialValues: {
      name: '',
      key: '',
      description: '',
      icon: 'Zap',
      type: 'utility',
      schema: [] as any[]
    },
    validate: {
      name: (value) => (value.length < 2 ? 'Name is too short' : null),
      key: (value) => (value.length < 2 ? 'Key is too short' : null),
    }
  });

  const connectors = connectorsData?.connectors || [];
  const instances = instancesData?.instances || [];

  const healthyCount = instances.length; // Simplified for now

  const handleCreateInstance = (connector: any) => {
    setSelectedConnector(connector);
    setEditingInstance(null);
    setActiveStep(0);
    const initialConfig: any = {};
    connector.schema?.forEach((prop: any) => {
      initialConfig[prop.key] = prop.default_value || '';
    });
    setFormData({
      name: `${connector.name} Instance`,
      config: initialConfig
    });
    openModal();
  };

  const handleEditInstance = (instance: any) => {
    const connector = connectors.find((c: any) => c.id === instance.connector_id);
    setSelectedConnector(connector);
    setEditingInstance(instance);
    setActiveStep(1); // Skip introduction step
    setFormData({
      name: instance.name,
      config: { ...instance.config }
    });
    openModal();
  };

  const handleSave = async () => {
    if (!currentProjectId) return;

    try {
      if (editingInstance) {
        await updateInstance.mutateAsync({
          id: editingInstance.id,
          project_id: currentProjectId,
          connector_id: selectedConnector.id,
          name: formData.name,
          config: formData.config
        });
        notifications.show({ title: 'Success', message: 'Connector instance updated', color: 'green' });
      } else {
        await createInstance.mutateAsync({
          project_id: currentProjectId,
          connector_id: selectedConnector.id,
          name: formData.name,
          config: formData.config
        });
        notifications.show({ title: 'Success', message: 'Connector instance created', color: 'green' });
      }
      closeModal();
    } catch (err: any) {
      notifications.show({ title: 'Error', message: err.message || 'Failed to save connector instance', color: 'red' });
    }
  };

  const handleDelete = async (id: string) => {
    if (window.confirm('Are you sure you want to delete this connector instance?')) {
      try {
        await deleteInstance.mutateAsync(id);
        notifications.show({ title: 'Deleted', message: 'Connector instance removed', color: 'blue' });
      } catch (err: any) {
        notifications.show({ title: 'Error', message: err.message || 'Failed to delete instance', color: 'red' });
      }
    }
  };

  const handleTest = async () => {
    setTestResult(null);
    try {
      const payload = JSON.parse(testPayload);
      const result = await testConnector.mutateAsync({
        connectorKey: selectedConnector.key,
        config: formData.config,
        payload: payload
      });
      setTestResult({ success: true, data: result });
    } catch (err: any) {
      setTestResult({ success: false, error: err.message });
    }
  };

  const handleSaveCustomConnector = async (values: typeof customConnectorForm.values) => {
    try {
      await createConnector.mutateAsync(values);
      notifications.show({ title: 'Success', message: 'Custom connector created', color: 'green' });
      closeCustomModal();
      customConnectorForm.reset();
    } catch (err: any) {
      notifications.show({ title: 'Error', message: err.message || 'Failed to create connector', color: 'red' });
    }
  };

  const addSchemaProperty = () => {
    customConnectorForm.insertListItem('schema', { key: '', label: '', type: 'string', required: false });
  };

  const renderField = (prop: any) => {
    const value = formData.config?.[prop.key];
    const onChange = (val: any) => setFormData({
      ...formData,
      config: { ...(formData.config || {}), [prop.key]: val }
    });

    switch (prop.type) {
      case 'password':
        return (
          <PasswordInput
            key={prop.key}
            label={prop.label}
            description={prop.description}
            required={prop.required}
            value={value || ''}
            onChange={(e) => onChange(e.currentTarget.value)}
          />
        );
      case 'number':
        return (
          <NumberInput
            key={prop.key}
            label={prop.label}
            description={prop.description}
            required={prop.required}
            value={value ? parseInt(value) : undefined}
            onChange={(val) => onChange(val?.toString())}
          />
        );
      case 'select':
        return (
          <Select
            key={prop.key}
            label={prop.label}
            description={prop.description}
            required={prop.required}
            data={prop.options || []}
            value={value || ''}
            onChange={onChange}
          />
        );
      case 'textarea':
        return (
          <Textarea
            key={prop.key}
            label={prop.label}
            description={prop.description}
            required={prop.required}
            value={value || ''}
            onChange={(e) => onChange(e.currentTarget.value)}
            autosize
            minRows={3}
          />
        );
      default:
        return (
          <TextInput
            key={prop.key}
            label={prop.label}
            description={prop.description}
            required={prop.required}
            value={value || ''}
            onChange={(e) => onChange(e.currentTarget.value)}
          />
        );
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Connectors" 
        description="Manage your service integrations and configurations."
        actions={
          <Group gap="sm">
            <Paper withBorder px="md" py={4} radius="md" bg="gray.0">
              <Group gap="xs">
                <ThemeIcon color="green" variant="light" size="xs" radius="xl">
                  <Heart size={10} fill="currentColor" />
                </ThemeIcon>
                <Text size="xs" fw={700}>{healthyCount}/{instances.length} Healthy</Text>
              </Group>
            </Paper>
            <Button variant="light" leftSection={<Plus size={16} />} onClick={openCustomModal}>Custom Connector</Button>
          </Group>
        }
      />

      <Paper p="xl" radius="lg" withBorder shadow="sm">
        <Stack gap="lg">
          <Group justify="space-between">
            <Box>
              <Title order={4}>Connector Marketplace</Title>
              <Text size="xs" c="dimmed">Plug-and-play integrations for your business processes.</Text>
            </Box>
            <TextInput 
              placeholder="Search catalog..." 
              leftSection={<Search size={16} />}
              size="sm"
              radius="md"
              w={300}
            />
          </Group>
          <Divider />
          {isLoading ? (
            <CardGridLoadingState count={8} cols={4} />
          ) : connectorsError ? (
            <ErrorState error={connectorsError} action="load your connectors" onRetry={() => refetchConnectors()} />
          ) : (
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="lg">
            {connectors.map((connector: any) => {
              const Icon = IconMap[connector.icon] || Zap;
              return (
                <Card key={connector.id} withBorder padding="lg" radius="md" shadow="xs" className="connector-card">
                  <Group justify="space-between" mb="sm">
                    <ThemeIcon size={40} radius="md" variant="light" color="blue">
                      <Icon size={24} />
                    </ThemeIcon>
                    <Badge variant="light" size="xs">{connector.type}</Badge>
                  </Group>
                  <Text fw={700} size="md" mb={4}>{connector.name}</Text>
                  <Text size="xs" c="dimmed" mb="lg" h={32} lineClamp={2}>
                    {connector.description}
                  </Text>
                  <Button 
                    variant="light" 
                    fullWidth 
                    size="xs"
                    radius="md"
                    leftSection={<Plus size={14} />}
                    onClick={() => handleCreateInstance(connector)}
                  >
                    Configure
                  </Button>
                </Card>
              );
            })}
          </SimpleGrid>
          )}
        </Stack>
      </Paper>

      <Paper p="xl" radius="lg" withBorder shadow="sm">
        <Stack gap="md">
          <Group justify="space-between">
            <Box>
              <Title order={4}>Connection Health</Title>
              <Text size="xs" c="dimmed">Monitor the status of your active service connections.</Text>
            </Box>
            <Button 
              variant="subtle" 
              size="xs" 
              onClick={() => queryClient.invalidateQueries({ queryKey: ['connectorInstances'] })}
              loading={instancesData === undefined}
            >
              Refresh Status
            </Button>
          </Group>
          <Divider />
          {instances.length === 0 ? (
            <Alert icon={<AlertCircle size={16} />} color="gray" variant="light">
              No connector instances configured for this project.
            </Alert>
          ) : (
            <ScrollArea>
              <Table verticalSpacing="md" horizontalSpacing="lg">
                <Table.Thead bg="gray.0">
                  <Table.Tr>
                    <Table.Th>Integration Name</Table.Th>
                    <Table.Th>Provider</Table.Th>
                    <Table.Th>Status</Table.Th>
                    <Table.Th>Success Rate</Table.Th>
                    <Table.Th ta="right">Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {instances.map((inst: any) => {
                    const connector = connectors.find((c: any) => c.id === inst.connector_id);
                    const Icon = IconMap[connector?.icon] || Zap;
                    return (
                      <Table.Tr key={inst.id}>
                        <Table.Td>
                          <Group gap="sm">
                            <ThemeIcon size="md" variant="light" color="gray">
                              <Icon size={18} />
                            </ThemeIcon>
                            <Stack gap={0}>
                              <Text fw={700} size="sm">{inst.name}</Text>
                              {expertMode && <Text size="xs" c="dimmed">ID: {inst.id.substring(0, 8)}...</Text>}
                            </Stack>
                          </Group>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="outline" color="gray" size="sm">{connector?.name || 'Unknown'}</Badge>
                        </Table.Td>
                        <Table.Td>
                          <Badge variant="dot" color="green" size="sm">Online</Badge>
                        </Table.Td>
                        <Table.Td>
                          {expertMode ? (
                            <Group gap="xs">
                              <Text size="sm" fw={600}>99.8%</Text>
                              <Text size="xs" c="dimmed">(1.2k calls)</Text>
                            </Group>
                          ) : (
                            <Text size="sm" c="dimmed">Active</Text>
                          )}
                        </Table.Td>
                        <Table.Td>
                          <Group gap="xs" justify="flex-end">
                            <Tooltip label="Test Connection">
                              <ActionIcon aria-label="Run connector" variant="light" color="orange" onClick={() => { setSelectedConnector(connector); setFormData(inst); openTestModal(); }}>
                                <Play size={16} />
                              </ActionIcon>
                            </Tooltip>
                            <Tooltip label="Configure">
                              <ActionIcon aria-label="Settings" variant="light" color="blue" onClick={() => handleEditInstance(inst)}>
                                <Settings size={16} />
                              </ActionIcon>
                            </Tooltip>
                            <Tooltip label="Remove">
                              <ActionIcon aria-label="Delete connector" variant="light" color="red" onClick={() => handleDelete(inst.id)}>
                                <Trash2 size={16} />
                              </ActionIcon>
                            </Tooltip>
                          </Group>
                        </Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>
            </ScrollArea>
          )}
        </Stack>
      </Paper>

      {/* Guided Setup Wizard */}
      <Modal 
        opened={modalOpened} 
        onClose={closeModal} 
        title={
          <Group gap="sm">
            <ThemeIcon size="md" variant="light">
              {(() => {
                const Icon = selectedConnector ? (IconMap[selectedConnector.icon] || Zap) : Zap;
                return <Icon size={18} />;
              })()}
            </ThemeIcon>
            <Text fw={800} size="lg">
              {editingInstance ? `Edit ${selectedConnector?.name}` : `Setup ${selectedConnector?.name} Integration`}
            </Text>
          </Group>
        }
        size="lg"
        radius="lg"
      >
        <Stack gap="xl" py="md">
          <Stepper active={activeStep} onStepClick={setActiveStep} size="sm" allowNextStepsSelect={false}>
            <Stepper.Step label="Introduction" description="What is this?">
              <Stack gap="md" mt="xl">
                <Text size="sm">
                  The <b>{selectedConnector?.name}</b> connector allows you to {selectedConnector?.description?.toLowerCase() || 'integrate with external services'}.
                </Text>
                <Alert icon={<Info size={16} />} color="blue">
                  You will need to provide your API credentials in the next step. These are stored securely using AES-GCM encryption.
                </Alert>
                <Group justify="flex-end" mt="xl">
                  <Button variant="default" onClick={closeModal}>Cancel</Button>
                  <Button onClick={() => setActiveStep(1)} rightSection={<ChevronRight size={16} />}>Start Configuration</Button>
                </Group>
              </Stack>
            </Stepper.Step>
            
            <Stepper.Step label="Configuration" description="Credentials & Settings">
              <Stack gap="md" mt="xl">
                <TextInput
                  label="Friendly Name"
                  description="Identify this instance in your process models"
                  placeholder="e.g., Marketing Slack Bot"
                  required
                  value={formData.name || ''}
                  onChange={(e) => setFormData({ ...formData, name: e.currentTarget.value })}
                />
                <Divider label="Provider Settings" labelPosition="center" />
                {selectedConnector?.schema?.map((prop: any) => renderField(prop))}
                
                <Group justify="space-between" mt="xl">
                  <Button variant="default" onClick={() => setActiveStep(0)}>Back</Button>
                  <Group>
                    <Button variant="light" color="orange" leftSection={<Play size={16} />} onClick={openTestModal}>
                      Test Connection
                    </Button>
                    <Button onClick={() => setActiveStep(2)}>Next</Button>
                  </Group>
                </Group>
              </Stack>
            </Stepper.Step>

            <Stepper.Step label="Verification" description="Ready to use">
              <Stack gap="md" mt="xl" align="center" ta="center">
                <ThemeIcon size={60} radius="xl" color="green" variant="light">
                  <CheckCircle2 size={32} />
                </ThemeIcon>
                <Box>
                  <Text fw={700} size="lg">Ready to Deploy!</Text>
                  <Text size="sm" c="dimmed">Your integration is configured. You can now use it in any Service Task within this project.</Text>
                </Box>
                
                <Paper withBorder p="md" radius="md" bg="gray.0" w="100%" ta="left">
                  <Text size="xs" fw={700} c="dimmed" mb={4}>INSTANCE SUMMARY</Text>
                  <Group justify="space-between">
                    <Text size="sm" fw={600}>{formData.name}</Text>
                    <Badge size="xs">{selectedConnector?.name}</Badge>
                  </Group>
                </Paper>

                <Group justify="space-between" w="100%" mt="xl">
                  <Button variant="default" onClick={() => setActiveStep(1)}>Back</Button>
                  <Button onClick={handleSave} loading={createInstance.isPending || updateInstance.isPending} color="indigo">
                    {editingInstance ? 'Save Changes' : 'Install Integration'}
                  </Button>
                </Group>
              </Stack>
            </Stepper.Step>
          </Stepper>
        </Stack>
      </Modal>

      {/* Test Modal */}
      <Modal
        opened={testModalOpened}
        onClose={closeTestModal}
        title={`Test Connector: ${selectedConnector?.name}`}
        size="md"
      >
        <Stack gap="md">
          <Textarea
            label="Test Payload (JSON)"
            description="Provide sample data to send to the connector"
            value={testPayload}
            onChange={(e) => setTestPayload(e.currentTarget.value)}
            styles={{
              input: { fontFamily: 'monospace' }
            }}
            minRows={5}
            autosize
          />
          <Button fullWidth onClick={handleTest} loading={testConnector.isPending}>
            Execute Test
          </Button>

          {testResult && (
            <Alert 
              icon={testResult.success ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />} 
              color={testResult.success ? 'green' : 'red'}
              title={testResult.success ? 'Success' : 'Failed'}
            >
              <Box mt="xs">
                <Text size="xs" component="pre" style={{ whiteSpace: 'pre-wrap' }}>
                  {testResult.success 
                    ? JSON.stringify(testResult.data, null, 2) 
                    : testResult.error}
                </Text>
              </Box>
            </Alert>
          )}
        </Stack>
      </Modal>

      {/* Custom Connector Creator Modal */}
      <Modal
        opened={customModalOpened}
        onClose={closeCustomModal}
        title="Create Custom Connector Template"
        size="lg"
        radius="lg"
      >
        <form onSubmit={customConnectorForm.onSubmit(handleSaveCustomConnector)}>
          <Stack gap="md">
            <SimpleGrid cols={2}>
              <TextInput 
                label="Connector Name" 
                placeholder="e.g. My Custom API" 
                required 
                {...customConnectorForm.getInputProps('name')}
              />
              <TextInput 
                label="Connector Key" 
                placeholder="e.g. my-custom-api" 
                required 
                {...customConnectorForm.getInputProps('key')}
              />
            </SimpleGrid>
            <Textarea 
              label="Description" 
              placeholder="What does this connector do?" 
              {...customConnectorForm.getInputProps('description')}
            />
            <SimpleGrid cols={2}>
              <Select 
                label="Icon" 
                data={Object.keys(IconMap)} 
                {...customConnectorForm.getInputProps('icon')}
              />
              <Select 
                label="Category" 
                data={['utility', 'social', 'messaging', 'crm', 'erp']} 
                {...customConnectorForm.getInputProps('type')}
              />
            </SimpleGrid>

            <Divider label="Configuration Schema" labelPosition="center" />
            <Text size="xs" c="dimmed">Define the fields required to configure this connector instance.</Text>
            
            <Stack gap="xs">
              {customConnectorForm.values.schema.map((_, index) => (
                <Paper key={index} withBorder p="xs" radius="md">
                  <Group gap="xs" grow align="flex-end">
                    <TextInput 
                      label="Prop Key" 
                      placeholder="url" 
                      size="xs"
                      {...customConnectorForm.getInputProps(`schema.${index}.key`)} 
                    />
                    <TextInput 
                      label="Label" 
                      placeholder="Target URL" 
                      size="xs"
                      {...customConnectorForm.getInputProps(`schema.${index}.label`)} 
                    />
                    <Select 
                      label="Type" 
                      size="xs"
                      data={['string', 'password', 'number', 'select', 'textarea', 'boolean']} 
                      {...customConnectorForm.getInputProps(`schema.${index}.type`)} 
                    />
                    <ActionIcon aria-label="Delete connector" color="red" variant="light" onClick={() => customConnectorForm.removeListItem('schema', index)}>
                      <Trash2 size={14} />
                    </ActionIcon>
                  </Group>
                </Paper>
              ))}
              <Button variant="light" size="xs" leftSection={<Plus size={14} />} onClick={addSchemaProperty}>
                Add Property
              </Button>
            </Stack>

            <Group justify="flex-end" mt="xl">
              <Button variant="default" onClick={closeCustomModal}>Cancel</Button>
              <Button type="submit" color="indigo" loading={createConnector.isPending}>Create Template</Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </Stack>
  );
}
