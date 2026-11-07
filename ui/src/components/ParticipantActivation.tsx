import { Alert, Button, Group, List, Modal, Radio, Select, Stack, Text } from '@mantine/core';
import { AlertTriangle } from 'lucide-react';
import dayjs from 'dayjs';
import { useMemo, useState } from 'react';

import { activationReady, heldWorkToSend, type HeldWorkChoice } from '../domain/participantActivation';
import type { Participant } from '../domain/participantImport';
import { useUsers } from '../hooks/useIdentityDirectory';
import { useHeldTasks, useSetParticipantActive } from '../hooks/useParticipants';
import { useAppStore } from '../store/useAppStore';
import { ReasonField } from './inbox/ReasonField';

const FRESH: HeldWorkChoice = { do: 'hand_over', to: null, reason: '' };

/**
 * Deactivates or reactivates a person.
 *
 * Deactivating asks what happens to the tasks they hold, because a task names
 * who holds it and nobody else can act on it while it does: left alone it
 * waits for somebody who can no longer sign in. Reactivating asks nothing and
 * moves nothing back.
 */
export function ParticipantActivation({ person, onClose }: { person: Participant | null; onClose: () => void }) {
  const deactivating = !!person?.active;
  const { data: held = [], isLoading } = useHeldTasks(deactivating ? person?.id ?? null : null);
  const organizationId = useAppStore((state) => state.currentOrganizationId);
  const { data: usersData } = useUsers(organizationId);
  const setActive = useSetParticipantActive();
  const [choice, setChoice] = useState<HeldWorkChoice>(FRESH);
  const users = useMemo(
    () => (usersData?.users ?? []).map((u) => ({ value: u.username, label: u.full_name || u.username })),
    [usersData],
  );

  const close = () => {
    setChoice(FRESH);
    onClose();
  };
  const name = person ? person.display_name || person.username : '';

  return (
    <Modal
      opened={!!person}
      onClose={close}
      radius="md"
      title={<Text fw={700}>{deactivating ? `Deactivate ${name}` : `Activate ${name}`}</Text>}
    >
      {person && (
        <Stack gap="md">
          {!deactivating && (
            <Text size="sm">
              {name} can sign in to their tasks and receives new work again. Tasks handed on while they were
              inactive stay where they are.
            </Text>
          )}
          {deactivating && (
            <Text size="sm">
              {name} can no longer sign in and receives no new work. Their history is kept, and they can be
              activated again.
            </Text>
          )}
          {deactivating && !isLoading && held.length === 0 && (
            <Text size="sm" c="dimmed">They hold no open tasks.</Text>
          )}
          {deactivating && held.length > 0 && (
            <>
              <Alert color="yellow" variant="light" icon={<AlertTriangle size={16} aria-hidden />}>
                They hold {held.length} open {held.length === 1 ? 'task' : 'tasks'}. Nobody else can act on
                {held.length === 1 ? ' it' : ' them'} while {held.length === 1 ? 'it is' : 'they are'} theirs.
                <List size="sm" mt="xs">
                  {held.slice(0, 5).map((task) => (
                    <List.Item key={task.task_id}>
                      {task.name}
                      {task.since && <Text span size="xs" c="dimmed"> · {dayjs(task.since).fromNow()}</Text>}
                    </List.Item>
                  ))}
                  {held.length > 5 && <List.Item>and {held.length - 5} more</List.Item>}
                </List>
              </Alert>
              <Radio.Group
                label="Their open tasks"
                value={choice.do}
                onChange={(value) => setChoice((c) => ({ ...c, do: value as HeldWorkChoice['do'] }))}
              >
                <Stack gap="xs" mt="xs">
                  <Radio value="hand_over" label="Hand them to someone" description="They land in that person's inbox." />
                  <Radio
                    value="release"
                    label="Put them back on offer"
                    description="Whoever the step offers them to can claim them."
                  />
                  <Radio
                    value="keep"
                    label="Leave them with them"
                    description="They wait, listed under Nobody can act on the tasks page, until somebody hands them over."
                  />
                </Stack>
              </Radio.Group>
              {choice.do === 'hand_over' && (
                <Select
                  label="Hand them to"
                  placeholder="Pick a person"
                  data={users.filter((u) => u.value !== person.username)}
                  value={choice.to}
                  onChange={(to) => setChoice((c) => ({ ...c, to }))}
                  searchable
                />
              )}
              {choice.do !== 'keep' && (
                <ReasonField value={choice.reason} onChange={(reason) => setChoice((c) => ({ ...c, reason }))} />
              )}
            </>
          )}
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={close}>Cancel</Button>
            <Button
              color={deactivating ? 'red' : 'indigo'}
              loading={setActive.isPending}
              disabled={deactivating && (isLoading || !activationReady(choice, held.length))}
              onClick={() =>
                setActive.mutate(
                  { id: person.id, active: !deactivating, work: deactivating ? heldWorkToSend(choice, held.length) : undefined },
                  { onSuccess: close },
                )
              }
            >
              {deactivating ? 'Deactivate' : 'Activate'}
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}
