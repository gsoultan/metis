import { Box, Stack, Text, TextInput } from '@mantine/core';
import { Search } from 'lucide-react';
import { useState, useTransition } from 'react';

import { alsoKnownAs, glossarySearchSummary, searchGlossary, type GlossaryEntry } from '../domain/glossary';

/**
 * The glossary, found by either name.
 *
 * The input is left uncontrolled and the filter runs in a transition, so the
 * letters appear as they are typed and the list follows.
 */
export function GlossaryPanel() {
  const [query, setQuery] = useState('');
  const [, startTransition] = useTransition();
  const entries = searchGlossary(query);

  return (
    <Stack gap="sm">
      <TextInput
        aria-label="Search the glossary"
        placeholder="Search, for example gateway or live version"
        leftSection={<Search size={16} />}
        onChange={(event) => {
          const value = event.currentTarget.value;
          startTransition(() => setQuery(value));
        }}
      />
      <Text size="xs" c="dimmed" aria-live="polite">
        {glossarySearchSummary(entries.length)}
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
  const otherName = alsoKnownAs(entry);
  return (
    <Box mb="md">
      <Text component="dt" size="sm" fw={600}>
        {entry.term}
        {otherName && (
          <Text span size="xs" c="dimmed" fw={400}>
            {' '}· also called {otherName}
          </Text>
        )}
      </Text>
      <Text component="dd" size="sm" m={0}>
        {entry.definition}
      </Text>
      {entry.example && (
        <Text component="dd" size="xs" c="dimmed" m={0} mt={2}>
          For example: {entry.example}
        </Text>
      )}
    </Box>
  );
}
