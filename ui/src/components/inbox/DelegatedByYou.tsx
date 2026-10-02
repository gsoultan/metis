import { Box, Card, Text, ThemeIcon } from '@mantine/core';
import { UserCheck } from 'lucide-react';

import { useTranslation } from '../../i18n/context';
import type { DelegatedTask } from '../../services/types';

interface DelegatedByYouProps {
  /** The page of them the inbox read. */
  tasks: DelegatedTask[];
  /** How many there are in all, which can be more than the page. */
  total: number;
}

/**
 * The tasks the reader delegated that have not come back.
 *
 * They are the reader's to complete and are not in their list — the delegate
 * holds them — so without this a delegated task simply vanished from its
 * owner's inbox until it was handed back. Nothing to do here but see where
 * each one is; it is not rendered at all when there is none.
 */
export function DelegatedByYou({ tasks, total }: DelegatedByYouProps) {
  const { t } = useTranslation();
  if (tasks.length === 0) return null;
  const more = total - tasks.length;

  return (
    <Card component="section" withBorder radius="lg" padding="md" aria-label={t('handover.delegatedByYou')}>
      <Text fw={700} size="sm">{t('handover.delegatedByYou')}</Text>
      <Text size="xs" c="dimmed" mb="sm">{t('handover.delegatedByYouHelp')}</Text>
      <Box component="ul" m={0} p={0} style={{ listStyle: 'none' }}>
        {tasks.map((task) => (
          <Box component="li" key={task.id} py={4} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <ThemeIcon size="sm" variant="light" color="indigo" aria-hidden>
              <UserCheck size={12} />
            </ThemeIcon>
            <Text size="sm" fw={600}>{task.name}</Text>
            <Text size="sm" c="dimmed">{t('handover.withDelegate', { delegate: task.assignee?.username ?? '' })}</Text>
          </Box>
        ))}
      </Box>
      {more > 0 && (
        <Text size="xs" c="dimmed" mt="xs">{t('handover.delegatedByYouMore', { count: more })}</Text>
      )}
    </Card>
  );
}
