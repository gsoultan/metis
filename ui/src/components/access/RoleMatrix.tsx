import { Box, Card, Stack, Table, Text, TextInput } from '@mantine/core';
import { Search } from 'lucide-react';
import { useRef, useState, useTransition } from 'react';

import { legendFor } from '../../domain/roleLegend';
import { ROLE_OPTIONS } from '../../domain/roles';
import { matchesQuery } from '../../domain/textSearch';
import { useRoleLegend } from '../../hooks/useRoles';
import { useUsers } from '../../hooks/useUser';
import { useTranslation } from '../../i18n/context';
import type { ApiRoleAccess } from '../../services/domains/roleService';
import type { ApiOrganizationUser } from '../../services/types';
import { ErrorState, TableLoadingState } from '../state';
import { VirtualRows } from '../VirtualRows';
import { RoleCell } from './RoleCell';
import { RoleColumnHeader } from './RoleColumnHeader';
import type { LegendState } from './RoleLegend';

/** The person, then one column per role. */
const COLUMNS = ROLE_OPTIONS.length + 1;

/** A screenful of people; the role headings stay in view above them. */
const MATRIX_MAX_HEIGHT = '70vh';

/**
 * Who in this organization holds which role, at a glance: one row per account,
 * one column per role, and what each role is required for beside its heading.
 *
 * The accounts are the Accounts view's own list — the same query, the same
 * organization, every account, not a first page — and the legend is what the
 * server reads from its gates, so neither half can disagree with the rest of
 * the page or with what the server enforces.
 */
export function RoleMatrix() {
  const { t } = useTranslation();
  const { data, isLoading, error, refetch } = useUsers();
  const legend = useRoleLegend();
  const [query, setQuery] = useState('');
  const [, startTransition] = useTransition();
  const scrollRef = useRef<HTMLDivElement>(null);

  const accounts = data?.users ?? [];
  const shown = accounts.filter((account) => matchesQuery(query, account.username, account.full_name, account.email));

  const renderRow = (account: ApiOrganizationUser) => (
    <Table.Tr key={account.id}>
      <Table.Th scope="row">
        <Stack gap={0}>
          <Text fw={700} size="sm">
            {account.full_name || account.username}
          </Text>
          <Text size="xs" c="dimmed" fw={400}>
            @{account.username}
          </Text>
        </Stack>
      </Table.Th>
      {ROLE_OPTIONS.map((option) => (
        <RoleCell key={option.value} account={account} option={option} />
      ))}
    </Table.Tr>
  );

  return (
    <Card shadow="sm" radius="lg" withBorder p={0}>
      <Box p="md">
        <TextInput
          aria-label={t('access.search')}
          placeholder={t('access.searchPlaceholder')}
          leftSection={<Search size={16} />}
          style={{ maxWidth: 400 }}
          variant="filled"
          radius="md"
          onChange={(event) => {
            const value = event.currentTarget.value;
            startTransition(() => setQuery(value));
          }}
        />
      </Box>

      {isLoading ? (
        <TableLoadingState rows={5} columns={COLUMNS} />
      ) : error ? (
        <ErrorState error={error} action="load the accounts and their roles" onRetry={() => refetch()} />
      ) : (
        // The element that scrolls, which the rows are windowed against once
        // there are more than a hundred of them.
        <Table.ScrollContainer
          minWidth={720}
          type="native"
          ref={scrollRef}
          style={{ maxHeight: MATRIX_MAX_HEIGHT, overflowY: 'auto' }}
        >
          <Table stickyHeader verticalSpacing="sm" horizontalSpacing="lg" highlightOnHover>
            <Table.Thead bg="gray.0">
              <Table.Tr>
                <Table.Th>{t('access.columnPerson')}</Table.Th>
                {ROLE_OPTIONS.map((option) => (
                  <RoleColumnHeader key={option.value} option={option} legend={legendState(legend, option.value)} />
                ))}
              </Table.Tr>
            </Table.Thead>
            {shown.length === 0 ? (
              <Table.Tbody>
                <Table.Tr>
                  <Table.Td colSpan={COLUMNS}>
                    <Text ta="center" c="dimmed" py="xl">
                      {query ? t('access.noMatch', { query }) : t('access.empty')}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              </Table.Tbody>
            ) : (
              <VirtualRows items={shown} columnCount={COLUMNS} scrollRef={scrollRef} renderRow={renderRow} />
            )}
          </Table>
        </Table.ScrollContainer>
      )}

      {!isLoading && !error && accounts.length > 0 && (
        <Text size="xs" c="dimmed" px="md" py="sm">
          {query
            ? t('access.matching', { count: shown.length, total: accounts.length })
            : t('access.showing', { count: accounts.length })}
        </Text>
      )}
    </Card>
  );
}

/** What the legend beside one role's heading can say so far. */
function legendState(query: { data?: ApiRoleAccess[]; isError: boolean }, role: string): LegendState {
  if (query.isError) return { status: 'failed' };
  if (!query.data) return { status: 'loading' };
  return { status: 'known', areas: legendFor(query.data, role) ?? [] };
}
