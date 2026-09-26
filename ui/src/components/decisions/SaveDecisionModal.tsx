/**
 * Asks whether an edit goes into force now, or waits beside the live version.
 *
 * The question deploying a process asks (DeployVersionModal), for a decision.
 * Saving used to overwrite the version in force, so a threshold typed in for
 * review was deciding real instances the moment it was saved. Now every save
 * is a new version, and this is where it is decided whether steps start using
 * it: now, or once somebody has looked at it and made it live from Versions.
 *
 * Only asked when there is a live version to keep. A decision's first version
 * is the only one there is, and a question with one answer is not a question.
 */
import { Alert, Badge, Button, Group, Modal, Radio, Stack, Text } from '@mantine/core';
import { CircleDot, Info, Save } from 'lucide-react';
import { useState } from 'react';

export interface SaveDecisionModalProps {
  opened: boolean;
  name: string;
  /** The version this save will create. */
  nextVersion: number;
  /** The version in force now. */
  liveVersion: number;
  saving: boolean;
  onClose: () => void;
  /** Saves, staged or live. */
  onSave: (stage: boolean) => void;
}

export function SaveDecisionModal({ opened, name, nextVersion, liveVersion, saving, onClose, onSave }: SaveDecisionModalProps) {
  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          <Save size={20} />
          <Text fw={800}>
            Save {name} v{nextVersion}
          </Text>
        </Group>
      }
      size="lg"
      radius="lg"
    >
      {/*
        Mounted only while open, so the answer resets by unmounting: staging
        is a deliberate choice, and a remembered one would quietly stage a save
        somebody meant to put into force.
      */}
      {opened && (
        <SaveDecisionForm
          nextVersion={nextVersion}
          liveVersion={liveVersion}
          saving={saving}
          onClose={onClose}
          onSave={onSave}
        />
      )}
    </Modal>
  );
}

function SaveDecisionForm({
  nextVersion,
  liveVersion,
  saving,
  onClose,
  onSave,
}: Omit<SaveDecisionModalProps, 'opened' | 'name'>) {
  const [stage, setStage] = useState(false);
  return (
    <Stack gap="md">
      <Radio.Group value={stage ? 'stage' : 'live'} onChange={(value) => setStage(value === 'stage')} aria-label="When the new version is used">
        <Stack gap="sm">
          <Radio.Card p="md" radius="md" value="live">
            <Group align="flex-start" gap="sm" wrap="nowrap">
              <Radio.Indicator />
              <Stack gap={4}>
                <Group gap="xs">
                  <Text fw={700} size="sm">
                    Make v{nextVersion} live
                  </Text>
                  <Badge size="xs" color="green" variant="light">
                    Recommended
                  </Badge>
                </Group>
                <Text size="xs" c="dimmed">
                  Steps that name no version use v{nextVersion} from now on, in instances already running as well, the
                  next time they reach one. v{liveVersion} is kept in the history.
                </Text>
              </Stack>
            </Group>
          </Radio.Card>

          <Radio.Card p="md" radius="md" value="stage">
            <Group align="flex-start" gap="sm" wrap="nowrap">
              <Radio.Indicator />
              <Stack gap={4}>
                <Text fw={700} size="sm">
                  Stage v{nextVersion} for later
                </Text>
                <Text size="xs" c="dimmed">
                  v{nextVersion} is saved but not used: steps that name no version keep using v{liveVersion}. Make it
                  live from Versions when it is ready.
                </Text>
              </Stack>
            </Group>
          </Radio.Card>
        </Stack>
      </Radio.Group>

      <Alert variant="light" color="blue" icon={<Info size={16} />} radius="md">
        <Text size="xs">
          Either way, v{liveVersion} is not changed, and what instances have already decided stays as it was: each keeps
          the answer it got and the version that gave it.
        </Text>
      </Alert>

      <Group justify="flex-end">
        <Button variant="default" onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button
          color={stage ? 'gray' : 'indigo'}
          leftSection={stage ? <Save size={16} /> : <CircleDot size={16} />}
          loading={saving}
          onClick={() => onSave(stage)}
        >
          {stage ? `Save v${nextVersion} as staged` : `Save v${nextVersion} and make it live`}
        </Button>
      </Group>
    </Stack>
  );
}
