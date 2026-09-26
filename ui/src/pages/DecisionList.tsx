/**
 * The project's decisions: how they depend on each other, and the list of them.
 *
 * The two want different reads. The list shows a page, searched and paged on
 * the server. The graph needs every decision key once — a dependency on a
 * decision past the page is not a missing one — but none of their tables, so
 * it reads the keys alone. Both used to share one read of every version of
 * every table, up to a thousand of them fetched one page after another, and
 * the page showed nothing until the last had arrived.
 */
import { Button, Stack } from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useNavigate } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { useState } from 'react';

import { CreationWizard } from '../components/CreationWizard';
import { DecisionGraphSection } from '../components/DecisionGraphView';
import { DecisionListCard } from '../components/decisions/DecisionListCard';
import { DecisionVersionsModal } from '../components/decisions/DecisionVersionsModal';
import { PageHeader } from '../components/PageHeader';
import { useDecisionSummaries, useDecisions } from '../hooks/useDecisions';
import type { ApiDecisionSummary } from '../services/types';
import { useTranslation } from '../i18n/context';

export const DECISIONS_PAGE_SIZE = 25;

/** How long typing pauses before the search is sent: one request per pause, not per key. */
const SEARCH_PAUSE_MS = 250;

export function DecisionList({ onEdit, hideHeader }: { onEdit: (id: string) => void; hideHeader?: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [sentSearch] = useDebouncedValue(search.trim(), SEARCH_PAUSE_MS);
  const [page, setPage] = useState(1);
  const listed = useDecisions(page, DECISIONS_PAGE_SIZE, sentSearch);
  const keys = useDecisionSummaries();
  const [wizardOpened, setWizardOpened] = useState(false);
  const [historyOf, setHistoryOf] = useState<ApiDecisionSummary | null>(null);

  const rows = listed.data?.summaries ?? [];
  const afterDelete = (remaining: number) => {
    if (remaining > 0) return;
    // Its last version is gone, and the decision with it.
    setHistoryOf(null);
    // The last row of a page is gone; the page before it is the one that exists.
    if (rows.length === 1 && page > 1) setPage(page - 1);
  };

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
        decisions={keys.data?.items ?? []}
        loading={keys.isLoading}
        failed={keys.isError}
        total={keys.data?.truncated ? keys.data.total : undefined}
        onOpen={(id) => navigate({ to: '/decision-editor', search: { id } })}
      />

      <DecisionListCard
        rows={rows}
        total={listed.data?.pageInfo?.total ?? rows.length}
        page={page}
        pageSize={DECISIONS_PAGE_SIZE}
        onPage={setPage}
        search={search}
        onSearch={(text) => {
          setSearch(text);
          setPage(1);
        }}
        loading={listed.isLoading}
        error={listed.error}
        onRetry={() => listed.refetch()}
        onEdit={onEdit}
        onHistory={setHistoryOf}
        onCreate={() => setWizardOpened(true)}
      />

      {historyOf !== null && (
        <DecisionVersionsModal
          decisionKey={historyOf.key}
          name={historyOf.name}
          onClose={() => setHistoryOf(null)}
          onOpen={(id) => {
            setHistoryOf(null);
            onEdit(id);
          }}
          onDeleted={(_, remaining) => afterDelete(remaining)}
        />
      )}

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
