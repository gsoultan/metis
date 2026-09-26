import { Box, Stack, Text, TextInput } from '@mantine/core';
import { Search } from 'lucide-react';
import { useMemo, useState, useTransition } from 'react';

import {
  alsoKnownAs,
  glossaryEntries,
  glossaryIndex,
  glossarySearchSummary,
  searchGlossary,
  type GlossaryEntry,
} from '../domain/glossary';
import { useTranslation } from '../i18n/context';

/**
 * The glossary, found by either name, in the language the interface is in.
 *
 * The input is left uncontrolled and the filter runs in a transition, so the
 * letters appear as they are typed and the list follows. The entries are
 * indexed once per language rather than on every keystroke.
 */
export function GlossaryPanel() {
  const { t, locale } = useTranslation();
  const [query, setQuery] = useState('');
  const [, startTransition] = useTransition();
  const index = useMemo(() => glossaryIndex(glossaryEntries(t, locale)), [t, locale]);
  const entries = searchGlossary(index, query);

  return (
    <Stack gap="sm">
      <TextInput
        aria-label={t('glossary.search')}
        placeholder={t('glossary.searchPlaceholder')}
        leftSection={<Search size={16} />}
        onChange={(event) => {
          const value = event.currentTarget.value;
          startTransition(() => setQuery(value));
        }}
      />
      <Text size="xs" c="dimmed" aria-live="polite">
        {glossarySearchSummary(t, entries.length, index.entries.length)}
      </Text>
      <Box component="dl" m={0}>
        {entries.map((entry) => (
          <GlossaryItem key={entry.term} entry={entry} />
        ))}
      </Box>
    </Stack>
  );
}

function GlossaryItem({ entry }: { entry: GlossaryEntry }) {
  const { t } = useTranslation();
  const otherName = alsoKnownAs(entry);
  return (
    <Box mb="md">
      <Text component="dt" size="sm" fw={600}>
        {entry.term}
        {otherName && (
          <Text span size="xs" c="dimmed" fw={400}>
            {' · '}
            {t('glossary.alsoCalled', { name: otherName })}
          </Text>
        )}
      </Text>
      <Text component="dd" size="sm" m={0}>
        {entry.definition}
      </Text>
      {entry.example && (
        <Text component="dd" size="xs" c="dimmed" m={0} mt={2}>
          {t('glossary.example', { example: entry.example })}
        </Text>
      )}
    </Box>
  );
}
