/**
 * The decision list: a search box, one page of rows, and the way to the rest.
 *
 * It shows what the server sent for the page asked for, and says where that
 * page sits in the whole. The searching and the paging happen on the server;
 * the list used to load up to a thousand full tables, one request after
 * another, to show twenty-five rows of them.
 *
 * A row is a decision, not a version of one. Every save is a new version, and
 * a row per version showed a table edited five times five times, none of them
 * saying which was in force. Each row names the live version — what opening it
 * opens — and the newest when that is waiting behind it.
 */
import { ActionIcon, Badge, Box, Button, Card, Group, Pagination, Skeleton, Stack, Table, Text, TextInput, ThemeIcon, Tooltip } from '@mantine/core';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { Edit2, History, Search, Table2 } from 'lucide-react';

import { hitPolicyOf } from '../../domain/decisionTable';
import { decisionRowVersions } from '../../domain/decisionVersions';
import { pageSpan } from '../../domain/pageSpan';
import type { ApiDecisionSummary } from '../../services/types';
import { ErrorState } from '../state';

dayjs.extend(relativeTime);

const COLUMNS = 6;
const SEARCH_MAX_LENGTH = 255;

export interface DecisionListCardProps {
  rows: ApiDecisionSummary[];
  /** How many decisions match, across every page. */
  total: number;
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  search: string;
  onSearch: (text: string) => void;
  /** True before the first page has arrived. */
  loading: boolean;
  error: unknown;
  onRetry: () => void;
  onEdit: (id: string) => void;
  /** Opens the decision's versions: make one live, roll back, delete one. */
  onHistory: (decision: ApiDecisionSummary) => void;
  onCreate: () => void;
}

export function DecisionListCard(props: DecisionListCardProps) {
  const { rows, total, page, pageSize, onPage, search, onSearch } = props;
  const span = pageSpan(page, pageSize, total, rows.length);

  return (
    <Card shadow="sm" radius="lg" withBorder p={0}>
      <Box p="md">
        <TextInput
          aria-label="Search decisions"
          placeholder="Search decisions…"
          // The server refuses a search longer than any name or key can be.
          maxLength={SEARCH_MAX_LENGTH}
          leftSection={<Search size={16} />}
          style={{ maxWidth: 400 }}
          variant="filled"
          radius="md"
          value={search}
          onChange={(event) => onSearch(event.currentTarget.value)}
        />
      </Box>

      <ListBody {...props} />

      {span.pageCount > 1 && (
        <Group justify="space-between" p="md" gap="sm">
          <Text size="xs" c="dimmed">
            {span.first}–{span.last} of {total}
          </Text>
          <Pagination
            size="sm"
            value={page}
            onChange={onPage}
            total={span.pageCount}
            getControlProps={(control) => ({
              'aria-label': { first: 'First page', last: 'Last page', next: 'Next page', previous: 'Previous page' }[control],
            })}
          />
        </Group>
      )}
    </Card>
  );
}

function ListBody({ rows, loading, error, onRetry, search, onEdit, onHistory, onCreate }: DecisionListCardProps) {
  // A failed request is not an empty project: it used to read as "you have nothing".
  if (error) return <ErrorState error={error} action="load your decision tables" onRetry={onRetry} />;

  return (
    <Table.ScrollContainer minWidth={800}>
      <Table verticalSpacing="md" horizontalSpacing="xl" highlightOnHover>
        <Table.Thead bg="gray.0">
          <HeaderRow />
        </Table.Thead>
        <Table.Tbody>
          {loading ? (
            <SkeletonRows />
          ) : rows.length === 0 ? (
            <Table.Tr>
              <Table.Td colSpan={COLUMNS}>
                <NothingListed search={search} onCreate={onCreate} />
              </Table.Td>
            </Table.Tr>
          ) : (
            rows.map((def) => <DecisionRow key={def.id} decision={def} onEdit={onEdit} onHistory={onHistory} />)
          )}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

function NothingListed({ search, onCreate }: { search: string; onCreate: () => void }) {
  if (search.trim()) {
    return (
      <Text ta="center" c="dimmed" py="xl">
        No decision matches “{search.trim()}”.
      </Text>
    );
  }
  return (
    <Stack align="center" py={60} gap="sm">
      <ThemeIcon size={60} radius="xl" variant="light" color="gray">
        <Table2 size={32} />
      </ThemeIcon>
      <Text fw={700} size="lg">
        No decisions found
      </Text>
      <Text ta="center" c="dimmed" maw={400}>
        Define business rules in a tabular format to use them in your process flows.
      </Text>
      <Button variant="subtle" mt="md" onClick={onCreate}>
        Create your first decision
      </Button>
    </Stack>
  );
}

function DecisionRow({
  decision,
  onEdit,
  onHistory,
}: {
  decision: ApiDecisionSummary;
  onEdit: (id: string) => void;
  onHistory: (decision: ApiDecisionSummary) => void;
}) {
  const policy = hitPolicyOf(decision.hit_policy || 'FIRST');
  return (
    <Table.Tr>
      <Table.Td>
        <Group gap="sm">
          <ThemeIcon color="cyan" variant="light" radius="md">
            <Table2 size={16} />
          </ThemeIcon>
          <Text fw={700} size="sm">
            {decision.name}
          </Text>
        </Group>
      </Table.Td>
      <Table.Td>
        <Badge variant="outline" color="gray" radius="sm" styles={{ label: { textTransform: 'none' } }}>
          {decision.key}
        </Badge>
      </Table.Td>
      <Table.Td>
        <VersionBadges decision={decision} />
      </Table.Td>
      <Table.Td>
        {/* The letter code means nothing to whoever owns the rule; what it settles does. */}
        <Tooltip label={policy?.description ?? decision.hit_policy}>
          <Badge variant="light" color="blue" styles={{ label: { textTransform: 'none' } }}>
            {policy?.label ?? decision.hit_policy}
          </Badge>
        </Tooltip>
      </Table.Td>
      <Table.Td>
        <Text size="xs" c="dimmed">
          {decision.last_changed_at ? dayjs(decision.last_changed_at).fromNow() : '—'}
        </Text>
      </Table.Td>
      <Table.Td>
        <Group gap="xs" justify="flex-end">
          <Tooltip label="Open the live version">
            <ActionIcon aria-label={`Edit ${decision.name}`} variant="light" color="blue" onClick={() => onEdit(decision.id)}>
              <Edit2 size={16} />
            </ActionIcon>
          </Tooltip>
          {/* Deleting is in here too, a version at a time: the row stands for
              the live version, and deleting that out from under the others
              is the one delete the server refuses. */}
          <Tooltip label="Versions: make one live, roll back, or delete one">
            <ActionIcon aria-label={`Version history of ${decision.name}`} variant="light" color="orange" onClick={() => onHistory(decision)}>
              <History size={16} />
            </ActionIcon>
          </Tooltip>
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}

/**
 * Which version is in force, and which is waiting behind it. The live version
 * is what a step that names no version reads, so it is the single most
 * important thing a row can say about a decision.
 */
function VersionBadges({ decision }: { decision: ApiDecisionSummary }) {
  const { live, staged } = decisionRowVersions(decision);
  return (
    <Group gap={6} wrap="nowrap">
      {live !== null ? (
        <Tooltip label="Steps that name no version use this one.">
          <Badge color="green" variant="light" styles={{ label: { textTransform: 'none' } }}>
            v{live} live
          </Badge>
        </Tooltip>
      ) : (
        <Tooltip label="No version is in force, so a step that names no version fails. Make one live from its versions.">
          <Badge color="orange" variant="light" styles={{ label: { textTransform: 'none' } }}>
            Nothing live
          </Badge>
        </Tooltip>
      )}
      {staged !== null && (
        <Tooltip
          label={
            live !== null
              ? `Saved, and not in use: steps still use v${live}. Make it live from its versions.`
              : 'Saved, and not in use. Make it live from its versions.'
          }
        >
          <Badge color="blue" variant="outline" styles={{ label: { textTransform: 'none' } }}>
            v{staged} staged
          </Badge>
        </Tooltip>
      )}
    </Group>
  );
}

function SkeletonRows() {
  return Array.from({ length: 4 }).map((_, i) => (
    <Table.Tr key={i}>
      <Table.Td>
        <Skeleton height={16} width="60%" />
      </Table.Td>
      <Table.Td>
        <Skeleton height={16} width="40%" />
      </Table.Td>
      <Table.Td>
        <Skeleton height={16} width={60} />
      </Table.Td>
      <Table.Td>
        <Skeleton height={16} width={60} />
      </Table.Td>
      <Table.Td>
        <Skeleton height={16} width="50%" />
      </Table.Td>
      <Table.Td>
        <Skeleton height={16} width={80} ml="auto" />
      </Table.Td>
    </Table.Tr>
  ));
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
      <Table.Th>Version</Table.Th>
      <Table.Th>Hit Policy</Table.Th>
      {/* When a version was last saved or made live: the history, not the row's birthday. */}
      <Table.Th>Last changed</Table.Th>
      <Table.Th ta="right">Actions</Table.Th>
    </Table.Tr>
  );
}
