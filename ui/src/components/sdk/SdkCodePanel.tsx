/**
 * The call you are about to make, written as code you can paste.
 *
 * It updates as you fill the step in, before you press anything — so the loop
 * is read the code, run it here, watch it work, then take the code away. An
 * example that appears only *after* a successful call teaches nothing about the
 * call that failed.
 */
import { Alert, CopyButton, Group, Paper, SegmentedControl, Stack, Text, Tooltip, Button } from '@mantine/core';
import { Check, Copy, Code2 } from 'lucide-react';

import type { SdkCall } from '../../domain/sdkCalls';
import { describeCall } from '../../domain/sdkCalls';
import { snippetFor, SNIPPET_LANGUAGES, type SnippetLanguage } from '../../domain/sdkSnippets';
import classes from './SdkStep.module.css';

interface SdkCodePanelProps {
  call: SdkCall | null;
  language: SnippetLanguage;
  onLanguageChange: (language: SnippetLanguage) => void;
  baseUrl: string;
}

export function SdkCodePanel({ call, language, onLanguageChange, baseUrl }: SdkCodePanelProps) {
  const hint = SNIPPET_LANGUAGES.find((entry) => entry.id === language)?.hint ?? '';

  if (call === null) {
    return (
      <Stack gap="sm" align="center" py="xl">
        <Code2 size={28} aria-hidden style={{ opacity: 0.4 }} />
        <Text size="sm" c="dimmed" ta="center" maw={280}>
          Pick a process and fill in a step. The code for whatever you are about to do appears
          here, with your real ids in it.
        </Text>
      </Stack>
    );
  }

  const snippet = snippetFor(language, call, { baseUrl });

  return (
    <Stack gap="sm">
      <SegmentedControl
        fullWidth
        size="xs"
        value={language}
        onChange={(next) => onLanguageChange(next as SnippetLanguage)}
        data={SNIPPET_LANGUAGES.map((entry) => ({ value: entry.id, label: entry.label }))}
        aria-label="Language for the example"
      />

      <Group justify="space-between" wrap="nowrap" gap="xs">
        <Text size="xs" c="dimmed" truncate>
          {describeCall(call)} · {hint}
        </Text>
        <CopyButton value={snippet} timeout={1500}>
          {({ copied, copy }) => (
            <Tooltip label={copied ? 'Copied' : 'Copy this snippet'} withArrow>
              <Button
                size="compact-xs"
                variant={copied ? 'light' : 'subtle'}
                color={copied ? 'teal' : 'gray'}
                leftSection={copied ? <Check size={12} /> : <Copy size={12} />}
                onClick={copy}
              >
                {copied ? 'Copied' : 'Copy'}
              </Button>
            </Tooltip>
          )}
        </CopyButton>
      </Group>

      <Paper withBorder radius="md" p="sm" bg="light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-8))">
        <pre className={classes.code}>{snippet}</pre>
      </Paper>

      {/* Said once, here, rather than left for somebody to discover by pasting
          a snippet into a service and wondering why it is unauthorised. */}
      <Alert variant="light" color="gray" p="xs" radius="md">
        <Text size="xs">
          The token is read from the environment, never printed. Give your service its own
          account — this page runs as you.
        </Text>
      </Alert>
    </Stack>
  );
}
