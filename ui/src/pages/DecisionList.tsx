import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Group,
  List,
  Loader,
  Modal,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useNavigate } from '@tanstack/react-router';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { AlertTriangle, Edit2, Plus, Search, Table2, Trash2 } from 'lucide-react';
import { useState, useTransition } from 'react';

import { CreationWizard } from '../components/CreationWizard';
import { DecisionGraphSection } from '../components/DecisionGraphView';
import { PageHeader } from '../components/PageHeader';
import { ErrorState } from '../components/state';
import { hitPolicyOf } from '../domain/decisionTable';
import { matchesQuery } from '../domain/textSearch';
import { useDecisionImpact, useDecisions, useDeleteDecision } from '../hooks/useDecisions';
import { errorMessage } from '../services/shared/errors';
import type { ApiDecision } from '../services/types';
import { useTranslation } from '../i18n/context';

dayjs.extend(relativeTime);

const COLUMNS = 5;

export function DecisionList({ onEdit, hideHeader }: { onEdit: (id: string) => void, hideHeader?: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data, isLoading, error, refetch } = useDecisions();
  const deleteDecision = useDeleteDecision();
  const [wizardOpened, setWizardOpened] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [, startTransition] = useTransition();
  // The decision waiting for "yes, delete it", with what depends on it fetched
  // only once the question is asked.
  const [pendingDelete, setPendingDelete] = useState<ApiDecision | null>(null);
  const { data: impact, isLoading: impactLoading } = useDecisionImpact(pendingDelete?.id ?? null);

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    const { id, name } = pendingDelete;
    try {
      await deleteDecision.mutateAsync(id);
      notifications.show({ title: 'Deleted', message: `${name} is gone.`, color: 'green' });
      setPendingDelete(null);
    } catch (err: unknown) {
      notifications.show({ title: `Could not delete ${name}`, message: errorMessage(err, 'It was not deleted.'), color: 'red' });
    }
  };

  if (isLoading) {
    return (
      <Stack gap="xl">
        <Skeleton height={40} radius="md" />
        <Card shadow="sm" radius="lg" withBorder p={0}>
          <Table.ScrollContainer minWidth={800}>
            <Table verticalSpacing="md" horizontalSpacing="xl">
              <Table.Thead bg="gray.0"><HeaderRow /></Table.Thead>
              <Table.Tbody>
                {Array.from({ length: 4 }).map((_, i) => (
                  <Table.Tr key={i}>
                    <Table.Td><Skeleton height={16} width="60%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width="40%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={60} /></Table.Td>
                    <Table.Td><Skeleton height={16} width="50%" /></Table.Td>
                    <Table.Td><Skeleton height={16} width={80} ml="auto" /></Table.Td>
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
    return <ErrorState error={error} action="load your decision tables" onRetry={() => refetch()} />;
  }

  const decisions = data?.decisions || [];
  const shown = decisions.filter((def) => matchesQuery(searchQuery, def.name, def.key));

  return (
    <Stack gap="xl">
      {!hideHeader && (
        <PageHeader
          title={t('page.decisionTables.title')}
          description={t('page.decisionTables.subtitle')}
          actions={
            <Button variant="filled" color="indigo" leftSection={<Plus size={16} />} onClick={() => setWizardOpened(true)}>
              Create New
            </Button>
          }
        />
      )}

      {/* Before the list, because the list is alphabetical and says nothing
          about which decision is made first — or that two of them depend on
          each other and neither can run. */}
      <DecisionGraphSection
        decisions={decisions.map((def) => ({
          id: def.id,
          key: def.key,
          name: def.name,
          required_decisions: def.required_decisions,
        }))}
        onOpen={(id) => navigate({ to: '/decision-editor', search: { id } })}
      />

      <Card shadow="sm" radius="lg" withBorder p={0}>
        <Box p="md">
          <TextInput
            aria-label="Search decisions"
            placeholder="Search decisions…"
            leftSection={<Search size={16} />}
            style={{ maxWidth: 400 }}
            variant="filled"
            radius="md"
            onChange={(e) => {
              const value = e.currentTarget.value;
              startTransition(() => setSearchQuery(value));
            }}
          />
        </Box>

        <Table.ScrollContainer minWidth={800}>
          <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
            <Table.Thead bg="gray.0"><HeaderRow /></Table.Thead>
            <Table.Tbody>
              {shown.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    {searchQuery ? (
                      <Text ta="center" c="dimmed" py="xl">No decision matches “{searchQuery}”.</Text>
                    ) : (
                      <Stack align="center" py={60} gap="sm">
                        <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                          <Table2 size={32} />
                        </ThemeIcon>
                        <Text fw={700} size="lg">No decisions found</Text>
                        <Text ta="center" c="dimmed" maw={400}>
                          Define business rules in a tabular format to use them in your process flows.
                        </Text>
                        <Button variant="subtle" mt="md" onClick={() => setWizardOpened(true)}>Create your first decision</Button>
                      </Stack>
                    )}
                  </Table.Td>
                </Table.Tr>
              ) : (
                shown.map((def) => (
                  <Table.Tr key={def.id}>
                    <Table.Td>
                      <Group gap="sm">
                        <ThemeIcon color="cyan" variant="light" radius="md">
                          <Table2 size={16} />
                        </ThemeIcon>
                        <Text fw={700} size="sm">{def.name}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="outline" color="gray" radius="sm" styles={{ label: { textTransform: 'none' } }}>{def.key}</Badge>
                    </Table.Td>
                    <Table.Td>
                      {/* The letter code means nothing to whoever owns the rule;
                          what it settles does. */}
                      <Tooltip label={hitPolicyOf(def.hit_policy || 'FIRST')?.description ?? def.hit_policy}>
                        <Badge variant="light" color="blue" styles={{ label: { textTransform: 'none' } }}>
                          {hitPolicyOf(def.hit_policy || 'FIRST')?.label ?? def.hit_policy}
                        </Badge>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" c="dimmed">{dayjs(def.created_at).fromNow()}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap="xs" justify="flex-end">
                        <Tooltip label="Edit decision">
                          <ActionIcon aria-label={`Edit ${def.name}`} variant="light" color="blue" onClick={() => onEdit(def.id)}>
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete">
                          <ActionIcon aria-label={`Delete ${def.name}`} variant="light" color="red" onClick={() => setPendingDelete(def)}>
                            <Trash2 size={16} />
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

      <Modal
        opened={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        title={<Text fw={700}>Delete {pendingDelete?.name}?</Text>}
        radius="lg"
      >
        <Stack gap="md">
          {impactLoading ? (
            <Group gap="xs"><Loader size="xs" /><Text size="sm" c="dimmed">Checking what uses it…</Text></Group>
          ) : impact && impact.running_instances > 0 ? (
            <Alert color="red" icon={<AlertTriangle size={16} />} title={`${impact.running_instances} running ${impact.running_instances === 1 ? 'instance is' : 'instances are'} on their way to it`}>
              <Text size="sm">They will fail at the step that evaluates it.</Text>
              {(impact.processes ?? []).length > 0 && (
                <List size="sm" mt="xs">
                  {(impact.processes ?? []).map((p) => (
                    <List.Item key={`${p.definition_id}-${p.version}`}>
                      {p.definition_name || p.definition_key} v{p.version}
                      {p.running_instances > 0 ? ` — ${p.running_instances} running` : ''}
                    </List.Item>
                  ))}
                </List>
              )}
            </Alert>
          ) : (
            <Text size="sm">No running instance is using it right now.</Text>
          )}
          <Text size="sm">
            Any process step that names <b>{pendingDelete?.key}</b> fails the next time it runs. This cannot be undone.
          </Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setPendingDelete(null)}>Cancel</Button>
            <Button color="red" leftSection={<Trash2 size={14} />} onClick={confirmDelete} loading={deleteDecision.isPending} disabled={impactLoading}>
              Delete
            </Button>
          </Group>
        </Stack>
      </Modal>

      <CreationWizard
        opened={wizardOpened}
        onClose={() => setWizardOpened(false)}
        initialType="decision"
        onCreateDecision={(data) => navigate({ to: '/decision-editor', search: { name: data.name, key: data.key } })}
        onCreateProcess={(data) => navigate({ to: '/designer', search: { name: data.name, key: data.key } })}
      />
    </Stack>
  );
}

function HeaderRow() {
  return (
    <Table.Tr>
      <Table.Th>Decision</Table.Th>
      <Table.Th>
        <Tooltip label="What a process step names to evaluate it">
          <span>Reference</span>
        </Tooltip>
      </Table.Th>
      <Table.Th>Hit Policy</Table.Th>
      {/* The column read "Last Modified" and showed created_at. */}
      <Table.Th>Created</Table.Th>
      <Table.Th ta="right">Actions</Table.Th>
    </Table.Tr>
  );
}
