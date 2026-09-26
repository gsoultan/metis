/**
 * The one panel simulation gets.
 *
 * It replaced a 300px sidebar and a 360px inspector — 660px of chrome around a
 * diagram people opened in order to look at the diagram. There is one rail now,
 * and what is in it changes with the state of the run rather than with a tab
 * somebody has to choose:
 *
 *   nothing run yet  →  the case, what it starts with, and a Run button
 *   run finished     →  the verdict, then what happened
 *
 * Setup does not disappear once a run exists — it collapses. The common reason
 * to reopen it is "try it with a bigger amount", which is one click away, and
 * the uncommon reasons stay out of the way.
 */
import { ActionIcon, Badge, Button, Collapse, Divider, Group, Menu, ScrollArea, Select, Stack, Text, TextInput, Tooltip } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { ChevronDown, ChevronRight, CirclePlus, ListChecks, MoreHorizontal, Play, RotateCcw, TerminalSquare, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { formatValue } from '../../domain/simulationScenario';
import { passed } from '../../domain/simulationTrace';
import type { SimulationController } from '../../hooks/useSimulation';
import { SimulationInputs } from './SimulationInputs';
import { SimulationTestModal } from './SimulationTestModal';
import { SimulationTimeline } from './SimulationTimeline';
import { SimulationVerdict } from './SimulationVerdict';
import type { SimulationTarget } from '../../domain/simulationScenario';
import { errorMessage } from '../../services/shared/errors';

export function SimulationRail({
  sim,
  target,
  onAnswerAwaiting,
}: {
  sim: SimulationController;
  target: SimulationTarget | null;
  /**
   * Open the answer card for the step the run is waiting on.
   *
   * Supplied by the designer rather than resolved here: which question a step
   * asks depends on what kind of step it is, and the node types live on the
   * canvas. Guessing "a person's task" would put the wrong two fields in front
   * of somebody whose process is waiting on a service call.
   */
  onAnswerAwaiting?: (nodeId: string) => void;
}) {
  const [testOpen, setTestOpen] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const hasRun = sim.run !== null;
  // Setup is open until a run exists, then folds itself away.
  const [setupOpen, setup] = useDisclosure(true);

  return (
    <Stack gap={0} h="100%" style={{ minHeight: 0 }}>
      {/* ── Which case ─────────────────────────────────────────────────── */}
      <Group gap={6} wrap="nowrap" p="sm" pb="xs">
        {renaming ? (
          <TextInput
            size="xs"
            value={sim.active?.name ?? ''}
            onChange={(event) => sim.active && sim.patchCase(sim.active.id, { name: event.currentTarget.value })}
            onBlur={() => setRenaming(false)}
            onKeyDown={(event) => event.key === 'Enter' && setRenaming(false)}
            autoFocus
            style={{ flex: 1 }}
            aria-label="Case name"
          />
        ) : (
          <Select
            size="xs"
            style={{ flex: 1, minWidth: 0 }}
            value={sim.activeId}
            onChange={(id) => id !== null && sim.selectCase(id)}
            data={sim.cases.map((item) => ({ value: item.id, label: item.name || 'Untitled case' }))}
            allowDeselect={false}
            aria-label="Which case"
            comboboxProps={{ withinPortal: true }}
          />
        )}

        <Menu position="bottom-end" withinPortal>
          <Menu.Target>
            <ActionIcon variant="subtle" color="gray" aria-label="Case options">
              <MoreHorizontal size={16} />
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Item leftSection={<CirclePlus size={14} />} onClick={sim.addCase}>
              New case
            </Menu.Item>
            <Menu.Item onClick={() => setRenaming(true)}>Rename</Menu.Item>
            <Menu.Item
              leftSection={<ListChecks size={14} />}
              onClick={sim.startAll}
              disabled={!sim.runnable || sim.cases.length < 2}
            >
              Run every case
            </Menu.Item>
            <Menu.Item leftSection={<TerminalSquare size={14} />} onClick={() => setTestOpen(true)}>
              Run this in CI…
            </Menu.Item>
            <Menu.Divider />
            <Menu.Item
              leftSection={<Trash2 size={14} />}
              color="red"
              onClick={() => sim.active && sim.removeCase(sim.active.id)}
              disabled={sim.cases.length === 1}
            >
              Delete case
            </Menu.Item>
          </Menu.Dropdown>
        </Menu>
      </Group>

      {/* Pass/fail across the suite, only once there is a suite to report on. */}
      {Object.keys(sim.suite).length > 1 && (
        <Group gap={4} px="sm" pb="xs" wrap="wrap">
          {sim.cases.map((item) => {
            const result = sim.suite[item.id];
            if (result === undefined) return null;
            return (
              <Tooltip key={item.id} label={item.name || 'Untitled case'} withArrow>
                <Badge
                  size="xs"
                  radius="sm"
                  variant={item.id === sim.activeId ? 'filled' : 'light'}
                  color={passed(result) ? 'teal' : 'red'}
                  style={{ cursor: 'pointer' }}
                  onClick={() => sim.selectCase(item.id)}
                >
                  {item.name || 'Untitled'}
                </Badge>
              </Tooltip>
            );
          })}
        </Group>
      )}

      <Divider />

      <ScrollArea style={{ flex: 1, minHeight: 0 }} type="hover">
        <Stack gap="sm" p="sm">
          {/* ── The verdict, when there is one ───────────────────────────── */}
          {sim.verdict !== null && (
            <SimulationVerdict
              verdict={sim.verdict}
              onAnswer={
                sim.verdict.tone === 'asking' && sim.verdict.node !== undefined && onAnswerAwaiting !== undefined
                  ? () => onAnswerAwaiting(sim.verdict!.node!)
                  : undefined
              }
            />
          )}

          {sim.error !== null && (
            <SimulationVerdict verdict={{ tone: 'bad', headline: 'The run could not start', detail: errorMessage(sim.error) }} />
          )}

          {/* ── Run ──────────────────────────────────────────────────────── */}
          <Group gap="xs" wrap="nowrap">
            <Button
              flex={1}
              onClick={sim.start}
              loading={sim.running}
              disabled={!sim.runnable}
              leftSection={hasRun ? <RotateCcw size={15} /> : <Play size={15} />}
              variant={hasRun ? 'default' : 'filled'}
            >
              {hasRun ? 'Run again' : 'Run'}
            </Button>
          </Group>

          {target === null && (
            <Text size="xs" c="dimmed">
              Deploy this process once, and every case here becomes runnable — from this screen and
              from CI.
            </Text>
          )}

          {sim.problems.map((problem) => (
            <Text key={problem} size="xs" c="orange.7">
              {problem}
            </Text>
          ))}

          {/* ── Setup, folded away once a run exists ─────────────────────── */}
          <Section
            label="Start it with"
            count={sim.active?.variables.length ?? 0}
            opened={hasRun ? setupOpen : true}
            collapsible={hasRun}
            onToggle={setup.toggle}
          >
            <SimulationInputs sim={sim} />
          </Section>

          {/* ── What happened ────────────────────────────────────────────── */}
          {hasRun && (
            <>
              <Divider />
              <Text size="sm" fw={600}>
                What happened
              </Text>
              <SimulationTimeline sim={sim} />
              <VariablesAtCursor sim={sim} />
            </>
          )}
        </Stack>
      </ScrollArea>

      <SimulationTestModal
        opened={testOpen}
        onClose={() => setTestOpen(false)}
        scenario={sim.active}
        target={target}
      />
    </Stack>
  );
}

/** A heading that folds, used only where folding earns its keep. */
function Section({
  label,
  count,
  opened,
  collapsible,
  onToggle,
  children,
}: {
  label: string;
  count: number;
  opened: boolean;
  collapsible: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  const heading = (
    <Group gap={6} wrap="nowrap">
      {collapsible && (opened ? <ChevronDown size={14} /> : <ChevronRight size={14} />)}
      <Text size="sm" fw={600}>
        {label}
      </Text>
      {count > 0 && (
        <Badge size="xs" variant="default" radius="sm" fw={500}>
          {count}
        </Badge>
      )}
    </Group>
  );

  return (
    <Stack gap={6}>
      {collapsible ? (
        <Group
          gap={6}
          onClick={onToggle}
          style={{ cursor: 'pointer', userSelect: 'none' }}
          role="button"
          tabIndex={0}
          aria-expanded={opened}
          onKeyDown={(event) => (event.key === 'Enter' || event.key === ' ') && onToggle()}
        >
          {heading}
        </Group>
      ) : (
        heading
      )}
      <Collapse expanded={opened}>{children}</Collapse>
    </Stack>
  );
}

/**
 * The variables as they stand at the cursor.
 *
 * Folded by default: it matters when a value is wrong, and until then it is
 * four lines of noise under the thing people are actually reading.
 */
function VariablesAtCursor({ sim }: { sim: SimulationController }) {
  const [opened, { toggle }] = useDisclosure(false);
  const names = Object.keys(sim.variables);
  if (names.length === 0) return null;

  return (
    <Section label="Variables now" count={names.length} opened={opened} collapsible onToggle={toggle}>
      <Stack gap={2}>
        {names.map((name) => {
          const changed = sim.changedKeys.includes(name);
          return (
            <Group key={name} justify="space-between" gap="sm" wrap="nowrap">
              <Group gap={4} wrap="nowrap" style={{ minWidth: 0 }}>
                <Text size="xs" c="dimmed" truncate>
                  {name}
                </Text>
                {/* A glyph, not a colour: a change marked only by colour is a
                    change some people never see. */}
                {changed && (
                  <Text size="xs" c="blue.7" aria-label="changed at this step">
                    ✎
                  </Text>
                )}
              </Group>
              <Text size="xs" fw={changed ? 600 : 400} truncate ff="monospace">
                {formatValue(sim.variables[name])}
              </Text>
            </Group>
          );
        })}
      </Stack>
    </Section>
  );
}
