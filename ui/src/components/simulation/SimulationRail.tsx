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
import { ActionIcon, Badge, Button, Divider, Group, Menu, ScrollArea, Select, Stack, Text, TextInput, Tooltip } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { CirclePlus, ListChecks, MoreHorizontal, Play, RotateCcw, Save, TerminalSquare, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { casePassed, checkExpectation, expectationVerdict, hasExpectation } from '../../domain/simulationCase';
import type { SimulationController } from '../../hooks/useSimulation';
import type { SimulationCasesController } from '../../hooks/useSimulationCases';
import { SimulationExpect } from './SimulationExpect';
import { SimulationReplay } from './SimulationReplay';
import { Section } from './SimulationSection';
import { VariablesAtCursor } from './SimulationVariablesNow';
import { SimulationInputs } from './SimulationInputs';
import { SimulationTestModal } from './SimulationTestModal';
import { SimulationTimeline } from './SimulationTimeline';
import { SimulationVerdict } from './SimulationVerdict';
import type { SimulationTarget } from '../../domain/simulationScenario';
import { errorMessage } from '../../services/shared/errors';

export function SimulationRail({
  sim,
  cases,
  target,
  labelOf,
  instanceId,
  onAnswerAwaiting,
}: {
  sim: SimulationController;
  /** Saving and deleting cases with the process. */
  cases: SimulationCasesController;
  target: SimulationTarget | null;
  /** What the diagram calls a node, for sentences that name one. */
  labelOf: (nodeId: string) => string;
  /** The real instance the designer was opened on, which can be replayed. */
  instanceId?: string;
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
  const [expectOpen, expectSection] = useDisclosure(false);
  const active = sim.active;
  // A case nobody has put anything into is not worth a warning.
  const worthSaving =
    active !== null && (active.savedId !== undefined || active.variables.length > 0 || active.answers.length > 0);
  const unsaved = cases.available && worthSaving && !cases.isSaved(active);
  const check =
    sim.run !== null && active !== null && hasExpectation(active.expect)
      ? checkExpectation(sim.run, active.expect, labelOf)
      : null;
  const caseError = cases.loadError ?? cases.saveError ?? cases.removeError ?? null;

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

        {unsaved && (
          <Tooltip label="Only in this tab. Save it with the process from the case menu." withArrow>
            <Badge size="xs" variant="light" color="gray" radius="sm" style={{ flexShrink: 0 }}>
              Not saved
            </Badge>
          </Tooltip>
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
              leftSection={<Save size={14} />}
              onClick={() => active !== null && cases.save(active)}
              disabled={!unsaved || cases.saving}
            >
              {active?.savedId === undefined ? 'Save with this process' : 'Save changes'}
            </Menu.Item>
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
              onClick={() => active !== null && cases.remove(active)}
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
                  color={casePassed(item, result, labelOf) ? 'teal' : 'red'}
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

          {check !== null && <SimulationVerdict verdict={expectationVerdict(check)} />}

          {caseError !== null && (
            <SimulationVerdict verdict={{ tone: 'warn', headline: 'Saved cases could not be read or written', detail: errorMessage(caseError) }} />
          )}

          {sim.error !== null && (
            <SimulationVerdict verdict={{ tone: 'bad', headline: 'The run could not start', detail: errorMessage(sim.error) }} />
          )}

          <SimulationReplay sim={sim} instanceId={instanceId} labelOf={labelOf} />

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
              Give the process a key, and every case here becomes runnable.
            </Text>
          )}

          {target?.kind === 'draft' && active?.replay?.on !== 'live' && (
            <Text size="xs" c="dimmed">
              Running the diagram as it is now. Nothing is deployed and nothing is kept — deploy it
              when you want this case to run from CI too.
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

          <Section
            label="It should end"
            count={active?.expect?.variables.length ?? 0}
            opened={expectOpen}
            collapsible
            onToggle={expectSection.toggle}
          >
            <SimulationExpect sim={sim} labelOf={labelOf} />
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
