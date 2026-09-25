import {
  Anchor,
  Badge,
  Button,
  Card,
  CloseButton,
  Group,
  Progress,
  Stack,
  Text,
  Timeline,
  Title,
  Tooltip,
  VisuallyHidden,
} from '@mantine/core';
import { Link } from '@tanstack/react-router';
import { ArrowRight, Check } from 'lucide-react';
import { useId, useState } from 'react';

import { gettingStartedSteps, nextStep, type GettingStartedFacts } from '../domain/gettingStarted';

/**
 * Getting started, drawn two ways: as a card somebody can put away, and as the
 * timeline in the Help drawer. Both draw the same steps from the same facts,
 * so the two cannot disagree about what is done. The rules are in
 * domain/gettingStarted.ts.
 */

/** Where hiding the card is remembered. Per browser, like the chosen language. */
const DISMISSED_STORAGE_KEY = 'metis-getting-started-dismissed';

function readDismissed(): boolean {
  try {
    return localStorage.getItem(DISMISSED_STORAGE_KEY) === 'true';
  } catch {
    // Private browsing, or storage turned off. Showing the card is the safe
    // answer for somebody who has not said they are finished with it.
    return false;
  }
}

function rememberDismissed(): void {
  try {
    localStorage.setItem(DISMISSED_STORAGE_KEY, 'true');
  } catch {
    // Hidden for this visit all the same. It comes back next time, which is a
    // smaller failure than refusing to hide it.
  }
}

interface GettingStartedCardProps {
  /** From gettingStartedFacts(). Undefined while any of its lists is loading. */
  facts: GettingStartedFacts | undefined;
}

/**
 * The checklist for somebody new, until they have done it all or hidden it.
 *
 * Nothing is drawn until every fact is in. A card that appears with every step
 * unticked and then ticks them one by one tells somebody who has done it all
 * that they have done nothing.
 */
export function GettingStartedCard({ facts }: GettingStartedCardProps) {
  const titleId = useId();
  const [dismissed, setDismissed] = useState(readDismissed);
  if (dismissed || !facts) return null;

  const steps = gettingStartedSteps(facts);
  const next = nextStep(steps);
  if (!next) return null;
  const doneCount = steps.filter((step) => step.done).length;

  const dismiss = () => {
    rememberDismissed();
    setDismissed(true);
  };

  return (
    <Card withBorder radius="lg" p="lg" component="section" aria-labelledby={titleId}>
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Stack gap={2}>
          <Title order={4} id={titleId}>Getting started</Title>
          <Text size="sm" c="dimmed">{doneCount} of {steps.length} done</Text>
        </Stack>
        <Tooltip label="Your progress stays under Help, the question mark at the top." withArrow>
          <CloseButton aria-label="Hide getting started" onClick={dismiss} />
        </Tooltip>
      </Group>
      <Progress
        value={(doneCount / steps.length) * 100}
        mt="md"
        aria-label={`${doneCount} of ${steps.length} getting started steps done`}
      />
      <Stack gap="lg" mt="lg" align="flex-start">
        <GettingStartedTimeline facts={facts} />
        <Button component={Link} to={next.to} rightSection={<ArrowRight size={16} />}>
          {next.label}
        </Button>
      </Stack>
    </Card>
  );
}

/** The steps drawn before anything is known about them: nothing ticked. */
const NO_PROGRESS_YET: GettingStartedFacts = {
  processDeployed: false,
  instanceStarted: false,
  taskCompleted: false,
  connectionSetUp: false,
  peopleAdded: false,
};

interface GettingStartedTimelineProps {
  /** From gettingStartedFacts(). Undefined while any of its lists is loading. */
  facts: GettingStartedFacts | undefined;
  /** Called when a step's link is followed, so a drawer can close behind it. */
  onNavigate?: () => void;
}

/**
 * Every step, each linked to where it is done, with the ones done ticked.
 *
 * Until the facts are in, the steps are still worth reading as a guide. They
 * are drawn with nothing ticked and no step marked next, rather than calling
 * the first step next and then jumping once the answers land.
 */
export function GettingStartedTimeline({ facts, onNavigate }: GettingStartedTimelineProps) {
  const steps = gettingStartedSteps(facts ?? NO_PROGRESS_YET);
  const next = facts ? nextStep(steps) : undefined;
  // The line fills down to the first step not done. Steps get done out of
  // order, so each one's own tick is what says it is done.
  const firstOpen = steps.findIndex((step) => !step.done);
  const lastInUnbrokenRun = (firstOpen === -1 ? steps.length : firstOpen) - 1;

  return (
    <Timeline active={lastInUnbrokenRun} bulletSize={22} lineWidth={2}>
      {steps.map((step) => (
        <Timeline.Item
          key={step.id}
          bullet={step.done ? <Check size={12} aria-hidden /> : undefined}
          title={
            <Group gap="xs" wrap="nowrap">
              <Anchor component={Link} to={step.to} onClick={onNavigate} size="sm" fw={600}>
                {step.done && <VisuallyHidden>Done: </VisuallyHidden>}
                {step.label}
              </Anchor>
              {step.id === next?.id && <Badge size="xs" variant="light">Next</Badge>}
            </Group>
          }
        >
          <Text c="dimmed" size="xs">{step.description}</Text>
        </Timeline.Item>
      ))}
    </Timeline>
  );
}
