/**
 * One step of the journey, with the rail that ties it to the next.
 *
 * The three states are deliberate and mean different things: *waiting* is
 * quietened because its turn has not come, *active* carries the accent, and
 * *done* keeps a tick so you can see what you already proved without scrolling
 * back through the wire log.
 */
import { Paper, Stack, Text, ThemeIcon, Title } from '@mantine/core';
import { Check } from 'lucide-react';
import type { ReactNode } from 'react';

import type { StepState } from '../../domain/sdkRun';
import classes from './SdkStep.module.css';

interface SdkStepProps {
  number: number;
  title: string;
  description: string;
  state: StepState;
  /** False on the last step, which has nothing to connect to. */
  connected?: boolean;
  /** Shown to the right of the title — a status badge, a count. */
  meta?: ReactNode;
  children: ReactNode;
}

export function SdkStep({
  number,
  title,
  description,
  state,
  connected = true,
  meta,
  children,
}: SdkStepProps) {
  const headingId = `sdk-step-${number}`;

  return (
    <section className={classes.step} aria-labelledby={headingId}>
      <div className={classes.rail}>
        <ThemeIcon
          size={32}
          radius="xl"
          variant={state === 'waiting' ? 'light' : 'filled'}
          color={state === 'waiting' ? 'gray' : 'blue'}
          aria-hidden
        >
          {state === 'done' ? <Check size={17} strokeWidth={2.5} /> : <Text fw={600} size="sm">{number}</Text>}
        </ThemeIcon>
        {connected && <div className={`${classes.line} ${state === 'done' ? classes.lineDone : ''}`} />}
      </div>

      <div className={`${classes.body} ${state === 'waiting' ? classes.waiting : ''}`}>
        <Stack gap="xs">
          <div>
            <Title order={3} size="h5" id={headingId}>
              {title}
              {meta && <span style={{ marginLeft: 'var(--mantine-spacing-sm)' }}>{meta}</span>}
            </Title>
            <Text size="sm" c="dimmed" mt={2}>
              {description}
            </Text>
          </div>
          <Paper withBorder radius="md" p="md">
            {children}
          </Paper>
        </Stack>
      </div>
    </section>
  );
}
