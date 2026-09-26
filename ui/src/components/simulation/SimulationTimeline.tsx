/**
 * What happened, in the words somebody who does not read node ids would use.
 *
 * `BusinessTimeline` could not be reused as it stands — it takes an instance id
 * and fetches audit rows, and a simulation has neither — but its rule is kept,
 * and it is the rule that matters: "Marc submitted a £4,200 expense", never
 * `Task_Started node=Activity_1x2y`. The server writes each step's sentence;
 * this decides only how it is laid out.
 *
 * Entries are buttons. Clicking one scrubs to that step, because the question
 * people arrive with is "what happened at the bit that went wrong", and this
 * list is how they find the bit.
 */
import { Badge, Group, Text, Timeline, UnstyledButton } from '@mantine/core';
import { Check, Clock, GitBranch, OctagonAlert, Play, Square, User } from 'lucide-react';

import { dayNumber, type SimulationStep } from '../../domain/simulationTrace';
import type { SimulationController } from '../../hooks/useSimulation';

function iconFor(step: SimulationStep) {
  switch (step.event) {
    case 'started':
      return <Play size={11} />;
    case 'decided':
      return <GitBranch size={11} />;
    case 'waiting':
      return <User size={11} />;
    case 'incident':
      return <OctagonAlert size={11} />;
    case 'ended':
      return <Square size={11} />;
    default:
      return <Check size={11} />;
  }
}

function colorFor(step: SimulationStep): string {
  switch (step.event) {
    case 'incident':
      return 'red';
    case 'waiting':
      return 'blue';
    case 'decided':
      return 'grape';
    case 'ended':
      return 'teal';
    default:
      return 'gray';
  }
}

export function SimulationTimeline({ sim }: { sim: SimulationController }) {
  const run = sim.run;
  if (run === null || run.steps.length === 0) return null;

  return (
    <Timeline active={sim.cursor} bulletSize={18} lineWidth={1} styles={{ item: { paddingLeft: 22 } }}>
      {run.steps.map((step, index) => {
        const current = index === sim.cursor;
        const ahead = index > sim.cursor;
        const dayChanged = index === 0 || dayNumber(run, index) !== dayNumber(run, index - 1);

        return (
          <Timeline.Item
            key={step.i}
            bullet={iconFor(step)}
            color={colorFor(step)}
            lineVariant={ahead ? 'dashed' : 'solid'}
          >
            <UnstyledButton
              onClick={() => sim.moveTo(index)}
              aria-current={current}
              style={{
                display: 'block',
                width: '100%',
                padding: '3px 6px',
                marginLeft: -6,
                borderRadius: 'var(--mantine-radius-sm)',
                backgroundColor: current
                  ? 'light-dark(var(--mantine-color-blue-0), var(--mantine-color-dark-5))'
                  : undefined,
                opacity: ahead ? 0.4 : 1,
                transition: 'opacity 120ms ease',
              }}
            >
              {/*
                A day marker only where the day changes. Stamping every entry
                with a date turns a trace into a wall of timestamps and hides
                the one thing worth noticing — that a day passed.
              */}
              {dayChanged && (
                <Badge size="xs" variant="default" radius="sm" mb={3} fw={500}>
                  <Group gap={3} wrap="nowrap">
                    <Clock size={9} aria-hidden />
                    Day {dayNumber(run, index)}
                  </Group>
                </Badge>
              )}

              <Text size="sm" fw={current ? 600 : 400} lh={1.4}>
                {step.note}
              </Text>

              {/*
                The plan's acceptance criterion as one line: which gateway, and
                which rule. This is the thing a designer came to see.
              */}
              {step.decision && (
                <Text size="xs" c="grape.7" mt={2} ff="monospace">
                  {step.decision.expression} → {step.decision.result}
                  {step.decision.chose !== undefined && ` · took ${step.decision.chose}`}
                  {step.decision.rule !== undefined && ` · ${step.decision.rule}`}
                </Text>
              )}

              {step.incident && (
                <Text size="xs" c="red.7" mt={2}>
                  {step.incident.message}
                </Text>
              )}
            </UnstyledButton>
          </Timeline.Item>
        );
      })}
    </Timeline>
  );
}
