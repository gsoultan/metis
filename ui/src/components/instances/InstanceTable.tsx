import { ActionIcon, Badge, Group, Stack, Table, Text, Tooltip, UnstyledButton } from '@mantine/core';
import dayjs from 'dayjs';
import { AlertTriangle, ChevronRight } from 'lucide-react';

import { StatusBadge } from '../StatusBadge';
import {
  definitionName,
  describeDuration,
  humanizeNodeId,
  instanceReference,
  isRunning,
  startedAtFromId,
  type ListedInstance,
  type NamedDefinition,
} from '../../domain/instanceList';

/** As much of an instance as a row needs, beyond what the domain layer names. */
export interface TableInstance extends ListedInstance {
  activeNodes?: Array<{ id: string }>;
}

interface InstanceTableProps {
  instances: TableInstance[];
  definitions: NamedDefinition[];
  /**
   * The moment these rows describe — when the data landed, not when this
   * happens to render.
   *
   * Passed in rather than read from the clock here, for two reasons. Reading it
   * during render is impure: every unrelated re-render would produce different
   * elapsed times from the same rows. And it makes the durations agree with the
   * rest of the page — while live updates are paused the whole table is a
   * snapshot, so its "running for 2h 14m" should hold still with everything
   * else rather than tick against figures that are no longer being refreshed.
   */
  asOf: number;
  /**
   * The instances on this page holding an unresolved incident.
   *
   * It has to come from the server because nothing on an instance says it. A
   * job that exhausts its retries raises an incident and stops; the instance
   * stays `active`. Nothing in the engine ever writes a `failed` status, so a
   * row that decided from `instance.status` would mark nothing, for ever — and
   * the failure drawer this marks would become unreachable.
   */
  needsAttention: ReadonlySet<string>;
  onOpen: (instance: TableInstance) => void;
  onInspect: (instanceId: string) => void;
}

/**
 * One page of instances.
 *
 * The row is the target. Opening an instance used to be a 26px icon at the far
 * right of a wide row — a small thing to hit, and a surprising place to look
 * for the primary action on a record whose name is at the far left. The whole
 * row now opens it, the icon stays as the visible affordance, and the keyboard
 * gets there the same way the mouse does.
 *
 * Making a row clickable and making it *accessible* are different jobs, and the
 * obvious way to do the first breaks the second. Putting `role="button"` and a
 * tabIndex on the `<tr>` reads well and is what this first did — but a row also
 * holds the "what went wrong" control, and a button inside a button is a
 * nested-interactive violation that leaves a screen-reader user unable to reach
 * the inner one. axe reports it as serious, and it is right to.
 *
 * So the accessible target is the process name: a real button, in the cell
 * somebody would look in, with the row's click kept as a mouse convenience on
 * top. Keyboard users tab to the name and press Enter; the whole row still
 * works for a pointer; the failure control is a sibling rather than a child.
 *
 * The action cell stops propagation. Without it, opening the failure drawer
 * also navigates away from the page it is drawn on.
 */
export function InstanceTable({
  instances,
  definitions,
  asOf,
  needsAttention,
  onOpen,
  onInspect,
}: InstanceTableProps) {
  return (
    // Below this width the columns cannot be read; the card used to simply
    // overflow on a narrow screen, cropping whichever column was last.
    <Table.ScrollContainer minWidth={860}>
      <Table verticalSpacing="sm" highlightOnHover>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Process</Table.Th>
            <Table.Th>Status</Table.Th>
            <Table.Th>Where it is</Table.Th>
            <Table.Th>Started</Table.Th>
            <Table.Th ta="right">Actions</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {instances.map((instance) => {
            const wants = needsAttention.has(instance.id);
            const open = () => onOpen(instance);
            return (
              <Table.Tr
                key={instance.id}
                // A pointer convenience only; the keyboard path is the button
                // in the first cell. Deliberately no role and no tabIndex — see
                // the note above.
                onClick={open}
                style={{ cursor: 'pointer' }}
              >
                <Table.Td>
                  {/* The process is what a person identifies an instance by;
                      the short reference is what tells two runs of the same
                      process apart. Both are inside the button, so the row's
                      accessible name is "Expense approval #69EAF9" rather than
                      the same word repeated down the column. */}
                  <UnstyledButton
                    onClick={(event) => {
                      // The row handles it too; without this it is handled twice.
                      event.stopPropagation();
                      open();
                    }}
                    style={{ display: 'block', textAlign: 'left' }}
                  >
                    <Stack gap={2}>
                      <Text size="sm" fw={600}>{definitionName(instance, definitions)}</Text>
                      <Text size="xs" c="dimmed" ff="monospace">{instanceReference(instance.id)}</Text>
                    </Stack>
                  </UnstyledButton>
                </Table.Td>

                <Table.Td><StatusBadge status={instance.status ?? ''} withIcon /></Table.Td>

                <Table.Td>
                  <WhereItIs instance={instance} />
                </Table.Td>

                <Table.Td>
                  <StartedAt id={instance.id} status={instance.status} asOf={asOf} />
                </Table.Td>

                <Table.Td ta="right">
                  <Group gap={6} justify="flex-end" wrap="nowrap">
                    {/*
                      Only where something actually failed. This sat on every
                      row, grey, and answered "nothing has failed on this
                      process" for the healthy majority who pressed it — a
                      control whose only possible reply was no.
                    */}
                    {wants && (
                      <Tooltip label="What went wrong, and try again">
                        <ActionIcon
                          aria-label="Show what failed"
                          variant="light"
                          color="red"
                          onClick={(event) => {
                            event.stopPropagation();
                            onInspect(instance.id);
                          }}
                        >
                          <AlertTriangle size={16} />
                        </ActionIcon>
                      </Tooltip>
                    )}
                    {/* The row is what opens the instance; this is the mark
                        that says so. Hidden from assistive technology because
                        the row already announces the same action. */}
                    <ChevronRight
                      size={16}
                      aria-hidden
                      style={{ color: 'var(--mantine-color-dimmed)', flexShrink: 0 }}
                    />
                  </Group>
                </Table.Td>
              </Table.Tr>
            );
          })}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

/** The steps an instance is sitting on right now. */
function WhereItIs({ instance }: { instance: TableInstance }) {
  const nodes = instance.activeNodes ?? [];
  if (nodes.length === 0) {
    return (
      <Text size="xs" c="dimmed">
        {isRunning(instance.status) ? 'Starting…' : 'Nothing in progress'}
      </Text>
    );
  }
  return (
    <Group gap={4}>
      {nodes.map((node) => (
        // tt="none" because Mantine uppercases a Badge by default and this is
        // somebody's step name, not a keyword. humanizeNodeId exists to turn
        // "Activity_AskDirector" into "Ask Director"; shouting it back as
        // "ASK DIRECTOR" throws that away and makes a calm table look urgent.
        <Badge key={node.id} size="sm" variant="light" color="blue" tt="none">
          {humanizeNodeId(node.id)}
        </Badge>
      ))}
    </Group>
  );
}

/**
 * When an instance started, and — while it is still going — how long it has
 * been going.
 *
 * The elapsed time is the operational number: an approval three minutes old is
 * working, the same approval four days old is stuck, and the two are otherwise
 * indistinguishable in this table. It is emphasised only while the instance is
 * unsettled, because for a finished run it is the age of a record rather than a
 * thing anyone is waiting on.
 *
 * There is no started-at field on a listed instance; the time is read out of the
 * UUIDv7 primary key. When the id is not a v7 this says so rather than inventing
 * a date.
 */
function StartedAt({ id, status, asOf }: { id: string; status?: string; asOf: number }) {
  const startedAt = startedAtFromId(id);
  if (!startedAt) return <Text size="xs" c="dimmed">—</Text>;

  const running = isRunning(status);
  const elapsed = describeDuration(startedAt.getTime(), asOf);

  // Both shapes put the number somebody reads first on top and the wall-clock
  // time underneath, so two rows line up whatever state they are in. The
  // alternative — "2 minutes ago" over the word "ago" — is what happens when
  // the second line is written for only one of them.
  const [headline, when] = running
    ? [elapsed ?? dayjs(startedAt).fromNow(), `since ${dayjs(startedAt).format('D MMM, HH:mm')}`]
    : [dayjs(startedAt).fromNow(), dayjs(startedAt).format('D MMM, HH:mm')];

  return (
    <Tooltip label={dayjs(startedAt).format('D MMM YYYY, HH:mm:ss')} withArrow>
      <Stack gap={2}>
        <Text size="sm" fw={running ? 600 : 400} c={running ? undefined : 'dimmed'}>
          {headline}
        </Text>
        <Text size="xs" c="dimmed">{when}</Text>
      </Stack>
    </Tooltip>
  );
}
