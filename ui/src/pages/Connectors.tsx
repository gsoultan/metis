import {
  Badge,
  Box,
  Button,
  Card,
  Divider,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  ThemeIcon,
  Title,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { useQueryClient } from '@tanstack/react-query';
import { Plus, Search } from 'lucide-react';
import { useState, useTransition } from 'react';

import { ConnectorManifests } from '../components/ConnectorManifests';
import { PageHeader } from '../components/PageHeader';
import { WebhookSettings } from '../components/WebhookSettings';
import { ConnectorInstanceTable } from '../components/connectors/ConnectorInstanceTable';
import { ConnectorSetupWizard } from '../components/connectors/ConnectorSetupWizard';
import { ConnectorTestModal, type TestResult } from '../components/connectors/ConnectorTestModal';
import { CustomConnectorModal } from '../components/connectors/CustomConnectorModal';
import { ConnectorGlyph } from '../components/connectors/ConnectorGlyph';
import { emptyFormFor, type InstanceFormState } from '../components/connectors/instanceForm';
import { CardGridLoadingState, ErrorState } from '../components/state';
import { matchesQuery } from '../domain/textSearch';
import {
  useConnectorInstances,
  useConnectors,
  useCreateConnector,
  useCreateConnectorInstance,
  useDeleteConnectorInstance,
  useExecuteConnector,
  useUpdateConnectorInstance,
} from '../hooks/useProcess';
import { errorMessage } from '../services/shared/errors';
import type { ApiConnector, ApiConnectorInstance, CreateConnectorPayload } from '../services/types';
import { useAppStore } from '../store/useAppStore';
import { useTranslation } from '../i18n/context';

const DEFAULT_TEST_PAYLOAD = '{\n  "text": "Hello from Metis!"\n}';
const CONFIGURATION_STEP = 1;

export function Connectors() {
  const { t } = useTranslation();
  const { currentProjectId, expertMode } = useAppStore();
  const {
    data: connectorsData,
    isLoading: connectorsLoading,
    error: connectorsError,
    refetch: refetchConnectors,
  } = useConnectors();
  const { data: instancesData, isLoading: instancesLoading, isFetching: instancesFetching } = useConnectorInstances();
  const queryClient = useQueryClient();
  const createInstance = useCreateConnectorInstance();
  const updateInstance = useUpdateConnectorInstance();
  const deleteInstance = useDeleteConnectorInstance();
  const testConnector = useExecuteConnector();
  const createConnector = useCreateConnector();

  const [wizardOpened, wizard] = useDisclosure(false);
  const [testOpened, testModal] = useDisclosure(false);
  const [customOpened, customModal] = useDisclosure(false);

  const [selectedConnector, setSelectedConnector] = useState<ApiConnector | null>(null);
  const [editingInstance, setEditingInstance] = useState<ApiConnectorInstance | null>(null);
  const [form, setForm] = useState<InstanceFormState>({ name: '', config: {} });
  const [activeStep, setActiveStep] = useState(0);
  const [testPayload, setTestPayload] = useState(DEFAULT_TEST_PAYLOAD);
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const [catalogueQuery, setCatalogueQuery] = useState('');
  const [, startTransition] = useTransition();

  const connectors = connectorsData?.connectors ?? [];
  const instances = instancesData?.instances ?? [];
  const catalogue = connectors.filter((c) => matchesQuery(catalogueQuery, c.name, c.description, c.key));

  const beginSetup = (connector: ApiConnector) => {
    setSelectedConnector(connector);
    setEditingInstance(null);
    setActiveStep(0);
    setForm(emptyFormFor(connector));
    wizard.open();
  };

  const beginEdit = (instance: ApiConnectorInstance) => {
    setSelectedConnector(connectors.find((c) => c.id === instance.connector?.id) ?? null);
    setEditingInstance(instance);
    setActiveStep(CONFIGURATION_STEP);
    setForm({ name: instance.name, config: { ...(instance.config ?? {}) } });
    wizard.open();
  };

  const beginTest = (instance: ApiConnectorInstance, connector: ApiConnector | undefined) => {
    setSelectedConnector(connector ?? null);
    setForm({ name: instance.name, config: { ...(instance.config ?? {}) } });
    setTestResult(null);
    testModal.open();
  };

  const save = async () => {
    if (!currentProjectId || !selectedConnector) return;
    const payload = { project: { id: currentProjectId }, connector: { id: selectedConnector.id }, ...form };
    try {
      if (editingInstance) {
        await updateInstance.mutateAsync({ id: editingInstance.id, ...payload });
        notifications.show({ title: 'Saved', message: `${form.name} was updated.`, color: 'green' });
      } else {
        await createInstance.mutateAsync(payload);
        notifications.show({ title: 'Connected', message: `${form.name} is ready to use.`, color: 'green' });
      }
      wizard.close();
    } catch (err: unknown) {
      notifications.show({ title: 'Could not save it', message: errorMessage(err, 'The connection was not saved.'), color: 'red' });
    }
  };

  const remove = async (instance: ApiConnectorInstance) => {
    const consequence =
      `Remove the ${instance.name} connection? ` +
      'Steps in this project that use it will fail until they are pointed at another connection.';
    if (!window.confirm(consequence)) return;
    try {
      await deleteInstance.mutateAsync(instance.id);
      notifications.show({ title: 'Removed', message: `${instance.name} is gone.`, color: 'blue' });
    } catch (err: unknown) {
      notifications.show({ title: 'Could not remove it', message: errorMessage(err, 'The connection was not removed.'), color: 'red' });
    }
  };

  const runTest = async () => {
    if (!selectedConnector) return;
    setTestResult(null);
    try {
      const payload = JSON.parse(testPayload);
      const data = await testConnector.mutateAsync({ connectorKey: selectedConnector.key, config: form.config, payload });
      setTestResult({ success: true, data });
    } catch (err: unknown) {
      setTestResult({ success: false, error: errorMessage(err, 'The test failed.') });
    }
  };

  const saveCustomConnector = async (values: CreateConnectorPayload) => {
    try {
      await createConnector.mutateAsync(values);
      notifications.show({ title: 'Template created', message: `${values.name} is in the catalogue.`, color: 'green' });
      customModal.close();
    } catch (err: unknown) {
      notifications.show({ title: 'Could not create it', message: errorMessage(err, 'The template was not created.'), color: 'red' });
    }
  };

  const refreshInstances = () => queryClient.invalidateQueries({ queryKey: ['connector-instances', currentProjectId] });

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.connectors.title')}
        description={t('page.connectors.subtitle')}
        actions={
          <Group gap="sm">
            <Paper withBorder px="md" py={4} radius="md" bg="gray.0">
              <Text size="xs" fw={700}>
                {instances.length === 1 ? '1 connection' : `${instances.length} connections`}
              </Text>
            </Paper>
            {expertMode && (
              <Button variant="light" leftSection={<Plus size={16} />} onClick={customModal.open}>
                Custom connector
              </Button>
            )}
          </Group>
        }
      />

      {/* Outbound and inbound belong together: to whoever is wiring a system
          up, calling a partner and being called by one are the same job. */}
      <ConnectorManifests />
      <WebhookSettings />

      <Paper p="xl" radius="lg" withBorder shadow="sm">
        <Stack gap="lg">
          <Group justify="space-between">
            <Box>
              <Title order={4}>Catalogue</Title>
              <Text size="xs" c="dimmed">Pick a service to connect this project to.</Text>
            </Box>
            <TextInput
              aria-label="Search the catalogue"
              placeholder="Search the catalogue…"
              leftSection={<Search size={16} />}
              size="sm"
              radius="md"
              w={300}
              onChange={(e) => {
                const value = e.currentTarget.value;
                startTransition(() => setCatalogueQuery(value));
              }}
            />
          </Group>
          <Divider />
          {connectorsLoading || instancesLoading ? (
            <CardGridLoadingState count={8} cols={4} />
          ) : connectorsError ? (
            <ErrorState error={connectorsError} action="load your connectors" onRetry={() => refetchConnectors()} />
          ) : catalogue.length === 0 ? (
            <Text size="sm" c="dimmed">Nothing in the catalogue matches “{catalogueQuery}”.</Text>
          ) : (
            <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="lg">
              {catalogue.map((connector) => {
                return (
                  <Card key={connector.id} withBorder padding="lg" radius="md" shadow="xs" className="connector-card">
                    <Group justify="space-between" mb="sm">
                      <ThemeIcon size={40} radius="md" variant="light" color="blue">
                        <ConnectorGlyph name={connector.icon} size={24} />
                      </ThemeIcon>
                      <Badge variant="light" size="xs">{connector.type}</Badge>
                    </Group>
                    <Text fw={700} size="md" mb={4}>{connector.name}</Text>
                    <Text size="xs" c="dimmed" mb="lg" h={32} lineClamp={2}>{connector.description}</Text>
                    <Button variant="light" fullWidth size="xs" radius="md" leftSection={<Plus size={14} />} onClick={() => beginSetup(connector)}>
                      Connect
                    </Button>
                  </Card>
                );
              })}
            </SimpleGrid>
          )}
        </Stack>
      </Paper>

      <ConnectorInstanceTable
        instances={instances}
        connectors={connectors}
        expertMode={expertMode}
        onRefresh={refreshInstances}
        refreshing={instancesFetching}
        onTest={beginTest}
        onEdit={beginEdit}
        onDelete={remove}
      />

      <ConnectorSetupWizard
        opened={wizardOpened}
        onClose={wizard.close}
        connector={selectedConnector}
        editing={editingInstance !== null}
        storedConfig={editingInstance?.config}
        form={form}
        onFormChange={setForm}
        activeStep={activeStep}
        onStepChange={setActiveStep}
        onTest={() => { setTestResult(null); testModal.open(); }}
        onSave={save}
        saving={createInstance.isPending || updateInstance.isPending}
      />

      <ConnectorTestModal
        opened={testOpened}
        onClose={testModal.close}
        connectorName={selectedConnector?.name}
        config={form.config}
        payload={testPayload}
        onPayloadChange={setTestPayload}
        onExecute={runTest}
        pending={testConnector.isPending}
        result={testResult}
      />

      {expertMode && (
        <CustomConnectorModal
          opened={customOpened}
          onClose={customModal.close}
          onSubmit={saveCustomConnector}
          pending={createConnector.isPending}
        />
      )}
    </Stack>
  );
}
