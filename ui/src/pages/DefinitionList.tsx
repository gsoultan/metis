import {
  Table,
  Card,
  Text,
  Button,
  Group,
  Stack,
  ThemeIcon,
  TextInput,
  ActionIcon,
  Badge,
  Modal,
  Box,
  Tooltip,
  Skeleton,
  Center,
  Loader,
} from '@mantine/core';
import { 
  Search, 
  Plus, 
  Play, 
  Eye, 
  Filter, 
  Network, 
  GitBranch, 
  History,
  RotateCcw,
} from 'lucide-react';
import { useDefinitions, useStartProcess, useDefinition } from '../hooks/useProcess';
import { PageHeader } from '../components/PageHeader';
import { BPMNGraph } from '../components/BPMNGraph';
import { CreationWizard } from '../components/CreationWizard';
import { useState, useMemo } from 'react';
import { useNavigate } from '@tanstack/react-router';
import dayjs from 'dayjs';
import { ErrorState } from '../components/state';

export function DefinitionList({ onEditModel, hideHeader }: { onEditModel?: (id: string) => void, hideHeader?: boolean }) {
  const navigate = useNavigate();
  const { data, isLoading, error, refetch } = useDefinitions();
  const startProcess = useStartProcess();
  const [selectedDef, setSelectedDef] = useState<any>(null);
  const [historyKey, setHistoryKey] = useState<string | null>(null);
  const [wizardOpened, setWizardOpened] = useState(false);
  
  const { data: fullDefData, isLoading: isFullLoading } = useDefinition(selectedDef?.id || null);

  const definitions = data?.definitions || [];

  const groupedDefinitions = useMemo(() => {
    const groups: Record<string, any[]> = {};
    definitions.forEach((def: any) => {
      if (!groups[def.key]) groups[def.key] = [];
      groups[def.key].push(def);
    });
    Object.keys(groups).forEach(key => {
      groups[key].sort((a, b) => b.version - a.version);
    });
    return groups;
  }, [definitions]);

  const latestDefinitions = useMemo(() => 
    Object.values(groupedDefinitions).map(versions => versions[0]),
  [groupedDefinitions]);

  if (isLoading) {
    return (
      <Stack gap="xl">
        <Skeleton height={40} radius="md" />
        <Card shadow="sm" radius="lg" withBorder p={0}>
          <Table.ScrollContainer minWidth={800}>
            <Table verticalSpacing="md" horizontalSpacing="xl">
              <Table.Thead bg="gray.0">
                <Table.Tr>
                  <Table.Th>Process Name</Table.Th>
                  <Table.Th>Key</Table.Th>
                  <Table.Th>Version</Table.Th>
                  <Table.Th>Deployment</Table.Th>
                  <Table.Th ta="right">Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {Array.from({ length: 4 }).map((_, i) => (
                  <Table.Tr key={i}>
                    <Table.Td><Skeleton height={16} width="60%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width="40%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={40} /></Table.Td>
                    <Table.Td><Skeleton height={16} width="50%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={100} ml="auto" /></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </Card>
      </Stack>
    );
  }

  // A rejected request previously fell through to the empty state, so an
  // outage was reported to the user as "you have nothing".
  if (error) {
    return <ErrorState error={error} action="load your process models" onRetry={() => refetch()} />;
  }

  const historyVersions = historyKey ? groupedDefinitions[historyKey] || [] : [];

  return (
    <Stack gap="xl">
      {!hideHeader && (
        <PageHeader 
          title="Processes" 
          description="Design, deploy and manage your business process models."
          actions={
            <Button 
              variant="filled" 
              color="indigo" 
              leftSection={<Plus size={16} />}
              onClick={() => setWizardOpened(true)}
            >
              Create New
            </Button>
          }
        />
      )}

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput 
                placeholder="Search models..." 
                leftSection={<Search size={16} />} 
                style={{ flex: 1, maxWidth: 400 }}
                variant="filled"
                radius="md"
              />
              <Button variant="light" leftSection={<Filter size={16} />} radius="md">Filter</Button>
            </Group>
          </Group>
        </Box>

        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>Process Name</Table.Th>
                <Table.Th>Key</Table.Th>
                <Table.Th>Version</Table.Th>
                <Table.Th>Deployment</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {latestDefinitions.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={5}>
                    <Stack align="center" py={60} gap="sm">
                      <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                        <GitBranch size={32} />
                      </ThemeIcon>
                      <Text fw={700} size="lg">No models found</Text>
                      <Text ta="center" c="dimmed" maw={400}>
                        Start by creating your first process definition to automate your workflow.
                      </Text>
                      <Button variant="subtle" mt="md" onClick={() => setWizardOpened(true)}>Create your first process</Button>
                    </Stack>
                  </Table.Td>
                </Table.Tr>
              ) : (
                latestDefinitions.map((def: any) => (
                  <Table.Tr key={def.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="indigo" variant="light" radius="md">
                          <Network size={16} />
                        </ThemeIcon>
                        <Stack gap={0}>
                          <Text fw={700} size="sm">{def.name}</Text>
                          <Text size="xs" c="dimmed">BPMN 2.0</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="outline" color="gray" radius="sm">{def.key}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="light" color="blue">v{def.version}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        <Text size="xs" c="dimmed">{dayjs(def.created_at).fromNow()}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Button 
                          size="xs" 
                          variant="light"
                          color="green"
                          leftSection={<Play size={14} />}
                          onClick={() => startProcess.mutate({ definitionKey: def.key })}
                          loading={startProcess.isPending}
                        >
                          Run
                        </Button>
                        <Tooltip label="Version History">
                          <ActionIcon aria-label="History process model" 
                            variant="light" 
                            color="orange" 
                            onClick={() => setHistoryKey(def.key)}
                          >
                            <History size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Edit Flow">
                          <ActionIcon aria-label="View process versions" 
                            variant="light" 
                            color="blue" 
                            onClick={() => onEditModel?.(def.id)}
                          >
                            <GitBranch size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="View Graph">
                          <ActionIcon aria-label="View process model" 
                            variant="light" 
                            color="indigo" 
                            onClick={() => setSelectedDef(def)}
                          >
                            <Eye size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))
              )}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Card>

      {/* Version History Modal */}
      <Modal
        opened={!!historyKey}
        onClose={() => setHistoryKey(null)}
        title={<Group gap="xs"><History size={20} color="orange" /><Text fw={800}>Version History: {historyKey}</Text></Group>}
        size="lg"
        radius="lg"
      >
        <Stack gap="md">
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Version</Table.Th>
                <Table.Th>Deployed At</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {historyVersions.map((v: any) => (
                <Table.Tr key={v.id}>
                  <Table.Td><Badge color={v.version === historyVersions[0].version ? "blue" : "gray"}>v{v.version}</Badge></Table.Td>
                  <Table.Td><Text size="sm">{dayjs(v.created_at).format('YYYY-MM-DD HH:mm')}</Text></Table.Td>
                  <Table.Td>
                    <Group justify="flex-end" gap="xs">
                      <Button size="compact-xs" variant="light" leftSection={<Eye size={12} />} onClick={() => { setSelectedDef(v); setHistoryKey(null); }}>View</Button>
                      <Button size="compact-xs" variant="light" color="indigo" leftSection={<RotateCcw size={12} />}>Rollback</Button>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Stack>
      </Modal>

      <Modal 
        opened={!!selectedDef} 
        onClose={() => setSelectedDef(null)} 
        title={<Text fw={700}>Process Visualization: {selectedDef?.name} (v{selectedDef?.version})</Text>}
        size="xl"
        radius="lg"
      >
        {isFullLoading ? (
          <Center py="xl">
            <Loader />
          </Center>
        ) : !!fullDefData?.definition && (
          <Stack gap="md">
            <BPMNGraph nodes={fullDefData.definition.nodes} flows={fullDefData.definition.flows} />
            <Group justify="flex-end">
              <Button onClick={() => setSelectedDef(null)}>Close</Button>
              <Button 
                variant="filled" 
                color="green" 
                leftSection={<Play size={16} />}
                onClick={() => {
                  startProcess.mutate({ definitionKey: fullDefData.definition.key });
                  setSelectedDef(null);
                }}
              >
                Start Instance
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>

      <CreationWizard 
        opened={wizardOpened}
        onClose={() => setWizardOpened(false)}
        initialType="process"
        onCreateProcess={(data) => {
          // Pass new process data to designer (could be via search params or state)
          navigate({ to: '/designer', search: { name: data.name, key: data.key } });
        }}
        onCreateDecision={(data) => {
          // In the future, we could navigate to Decision Editor with pre-filled name/key
          navigate({ to: '/decision-editor', search: { name: data.name, key: data.key } });
        }}
      />
    </Stack>
  );
}
