/**
 * Which process your code is going to drive.
 *
 * Picking one is also what turns the rest of the page from a blank form into
 * something specific: the topics, messages and signals below are read off the
 * chosen model, so nobody has to type a topic string from memory and find out
 * at runtime that they mistyped it.
 */
import { Alert, Loader, Select, Stack, Text } from '@mantine/core';
import { CircleAlert } from 'lucide-react';

import type { IntegrationSurface, SurfacePoint } from '../../domain/sdkSurface';
import { SdkAdvanced } from './SdkAdvanced';
import { SdkSurfacePanel } from './SdkSurfacePanel';

/** One deployable process, with every version of it that exists. */
export interface ProcessChoice {
  key: string;
  name: string;
  versions: number[];
}

interface SdkChooseStepProps {
  choices: ProcessChoice[];
  loading: boolean;
  definitionKey: string;
  version: number;
  onPickProcess: (key: string) => void;
  onPickVersion: (version: number) => void;
  surface: IntegrationSurface | null;
  surfaceLoading: boolean;
  selectedTopic: string;
  selectedNotify: string;
  onUseTopic: (topic: string) => void;
  onUseNotify: (point: SurfacePoint) => void;
}

export function SdkChooseStep({
  choices,
  loading,
  definitionKey,
  version,
  onPickProcess,
  onPickVersion,
  surface,
  surfaceLoading,
  selectedTopic,
  selectedNotify,
  onUseTopic,
  onUseNotify,
}: SdkChooseStepProps) {
  const chosen = choices.find((choice) => choice.key === definitionKey) ?? null;

  if (!loading && choices.length === 0) {
    return (
      <Alert variant="light" color="yellow" icon={<CircleAlert size={16} />}>
        <Text size="sm">
          This project has no deployed processes yet. Deploy one from the designer and it will
          appear here.
        </Text>
      </Alert>
    );
  }

  return (
    <Stack gap="md">
      <Select
        label="Process"
        description="The definition key is what your code passes to StartProcess."
        placeholder={loading ? 'Loading…' : 'Choose a process'}
        data={choices.map((choice) => ({
          value: choice.key,
          label: choice.name.trim() === '' ? choice.key : `${choice.name} — ${choice.key}`,
        }))}
        value={definitionKey === '' ? null : definitionKey}
        onChange={(value) => onPickProcess(value ?? '')}
        searchable
        nothingFoundMessage="No process by that name"
        disabled={loading}
        rightSection={loading ? <Loader size={14} /> : undefined}
      />

      {chosen !== null && chosen.versions.length > 1 && (
        <SdkAdvanced label="Run a specific version">
          <Select
            label="Version"
            description="Leave this on the live version unless you are trying a staged one."
            data={[
              { value: '0', label: `Live version (currently v${Math.max(...chosen.versions)})` },
              ...chosen.versions.map((number) => ({ value: String(number), label: `v${number}` })),
            ]}
            value={String(version)}
            onChange={(value) => onPickVersion(Number(value ?? 0))}
            allowDeselect={false}
          />
        </SdkAdvanced>
      )}

      {definitionKey !== '' && (
        <Stack gap="xs">
          <Text size="sm" fw={500}>
            What this process expects from your code
          </Text>
          {surfaceLoading ? (
            <Loader size="sm" />
          ) : surface === null ? (
            <Text size="sm" c="dimmed">
              Could not read the model. The rest of this page still works.
            </Text>
          ) : (
            <SdkSurfacePanel
              surface={surface}
              selectedTopic={selectedTopic}
              selectedNotify={selectedNotify}
              onUseTopic={onUseTopic}
              onUseNotify={onUseNotify}
            />
          )}
        </Stack>
      )}
    </Stack>
  );
}
