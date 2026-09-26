/**
 * The case you just ran, as a test you can put in CI.
 *
 * Behind a menu item rather than a tab. It is the handoff — an analyst has
 * clicked a case until the diagram does the right thing, and now wants it to
 * keep being true after the next person edits the model. That is a real moment,
 * but it happens once per case, and giving it equal billing with the run itself
 * was the wrong weight.
 *
 * The snippet renders from the *same* request object the Run button posts. A
 * second hand-maintained description of the call drifts, and an example that
 * does not match what just ran is worse than no example at all.
 */
import { Button, CopyButton, Group, Modal, SegmentedControl, Stack, Text, Tooltip } from '@mantine/core';
import { Check, Copy } from 'lucide-react';
import { useState } from 'react';

import {
  curlSnippet,
  goSnippet,
  simulationRequest,
  type Scenario,
  type SimulationTarget,
} from '../../domain/simulationScenario';
import { simulationBaseUrl } from '../../services/domains/simulationService';

type Language = 'go' | 'curl';

export function SimulationTestModal({
  opened,
  onClose,
  scenario,
  target,
}: {
  opened: boolean;
  onClose: () => void;
  scenario: Scenario | null;
  target: SimulationTarget | null;
}) {
  const [language, setLanguage] = useState<Language>('go');

  const request = scenario !== null && target !== null ? simulationRequest(scenario, target) : null;
  const snippet =
    request === null
      ? ''
      : language === 'go'
        ? (goSnippet(request, scenario as Scenario) ?? '')
        : curlSnippet(request, simulationBaseUrl());

  return (
    <Modal opened={opened} onClose={onClose} title="Run this case in CI" size="lg" radius="md">
      {request === null || target?.kind === 'draft' ? (
        <Text size="sm" c="dimmed">
          Deploy this version and the case becomes a test a pipeline can run. A draft has no version
          for CI to name.
        </Text>
      ) : (
        <Stack gap="sm">
          <Text size="sm" c="dimmed">
            The same case, against {request.definition_key} v{request.version ?? 0}. The ids in it are
            the ones that just ran.
          </Text>

          <Group justify="space-between" wrap="nowrap">
            <SegmentedControl
              size="xs"
              value={language}
              onChange={(next) => setLanguage(next as Language)}
              data={[
                { value: 'go', label: 'Go SDK' },
                { value: 'curl', label: 'curl' },
              ]}
              aria-label="Language for the example"
            />
            <CopyButton value={snippet} timeout={1500}>
              {({ copied, copy }) => (
                <Tooltip label={copied ? 'Copied' : 'Copy'} withArrow>
                  <Button
                    size="compact-sm"
                    variant={copied ? 'light' : 'filled'}
                    color={copied ? 'teal' : undefined}
                    onClick={copy}
                    leftSection={copied ? <Check size={14} /> : <Copy size={14} />}
                  >
                    {copied ? 'Copied' : 'Copy'}
                  </Button>
                </Tooltip>
              )}
            </CopyButton>
          </Group>

          <pre
            style={{
              margin: 0,
              padding: 12,
              maxHeight: 420,
              overflow: 'auto',
              fontSize: 12,
              lineHeight: 1.55,
              borderRadius: 'var(--mantine-radius-sm)',
              border: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
              backgroundColor: 'light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-8))',
              fontFamily: 'var(--mantine-font-family-monospace)',
            }}
          >
            <code>{snippet}</code>
          </pre>
        </Stack>
      )}
    </Modal>
  );
}
