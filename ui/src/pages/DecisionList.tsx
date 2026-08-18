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
  Box,
  Tooltip,
  Skeleton,
} from '@mantine/core';
import { 
  Search, 
  Plus, 
  Filter, 
  Table2,
  Edit2,
  Trash2,
} from 'lucide-react';
import { useDecisions } from '../hooks/useProcess';
import { PageHeader } from '../components/PageHeader';
import { DecisionGraphSection } from '../components/DecisionGraphView';
import { CreationWizard } from '../components/CreationWizard';
import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import dayjs from 'dayjs';
import { ErrorState } from '../components/state';
import { hitPolicyOf } from '../domain/decisionTable';

export function DecisionList({ onEdit, hideHeader }: { onEdit: (id: string) => void, hideHeader?: boolean }) {
  const navigate = useNavigate();
  const { data, isLoading, error, refetch } = useDecisions();
  const [wizardOpened, setWizardOpened] = useState(false);

  if (isLoading) {
    return (
      <Stack gap="xl">
        <Skeleton height={40} radius="md" />
        <Card shadow="sm" radius="lg" withBorder p={0}>
          <Table.ScrollContainer minWidth={800}>
            <Table verticalSpacing="md" horizontalSpacing="xl">
              <Table.Thead bg="gray.0">
                <Table.Tr>
                  <Table.Th>Decision Name</Table.Th>
                  <Table.Th>Key</Table.Th>
                  <Table.Th>Hit Policy</Table.Th>
                  <Table.Th>Last Modified</Table.Th>
                  <Table.Th ta="right">Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
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

  return (
    <Stack gap="xl">
      {!hideHeader && (
        <PageHeader 
          title="Decision Tables" 
          description="Manage your DMN-compatible decision tables and business rules."
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
          <Group justify="space-between">
            <Group flex={1}>
              <TextInput 
                placeholder="Search decisions..." 
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
                <Table.Th>Decision Name</Table.Th>
                <Table.Th>Key</Table.Th>
                <Table.Th>Hit Policy</Table.Th>
                <Table.Th>Last Modified</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {decisions.length === 0 ? (
                <Table.Tr>
                  <Table.Td colSpan={5}>
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
                  </Table.Td>
                </Table.Tr>
              ) : (
                decisions.map((def) => (
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
                      <Badge variant="outline" color="gray" radius="sm">{def.key}</Badge>
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
                        <Tooltip label="Edit Decision">
                          <ActionIcon aria-label="Edit decision table" 
                            variant="light" 
                            color="blue" 
                            onClick={() => onEdit(def.id)}
                          >
                            <Edit2 size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Delete">
                          <ActionIcon aria-label="Delete decision table" variant="light" color="red">
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

      <CreationWizard 
        opened={wizardOpened}
        onClose={() => setWizardOpened(false)}
        initialType="decision"
        onCreateDecision={(data) => {
          navigate({ to: '/decision-editor', search: { name: data.name, key: data.key } });
        }}
        onCreateProcess={(data) => {
          navigate({ to: '/designer', search: { name: data.name, key: data.key } });
        }}
      />
    </Stack>
  );
}
