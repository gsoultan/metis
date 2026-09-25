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
  type AnchorProps,
  type ButtonProps,
} from '@mantine/core';
import { createLink, linkOptions } from '@tanstack/react-router';
import { ArrowRight, Check } from 'lucide-react';
import { useId, useState, type ComponentPropsWithRef } from 'react';

import {
  gettingStartedSteps,
  nextStep,
  type GettingStartedFacts,
  type GettingStartedStepId,
} from '../domain/gettingStarted';
import { useAppStore } from '../store/useAppStore';

/**
 * Getting started, drawn two ways: as a card somebody can put away, and as the
 * timeline in the Help drawer. Both draw the same steps from the same facts,
 * so the two cannot disagree about what is done. The rules are in
 * domain/gettingStarted.ts.
 */

/**
 * Where each step is done.
 *
 * Checked against the route tree: a route that does not exist, or one missing
 * the search it requires, fails the typecheck here. The links used to be
 * Mantine components given the router's Link as `component`, which types `to`
 * as any string, and the two that go to Processes compiled without the tab
 * that page requires.
 */
const STEP_LINKS = {
  'deploy-process': linkOptions({ to: '/models', search: { tab: 'processes' } }),
  'start-instance': linkOptions({ to: '/models', search: { tab: 'processes' } }),
  'complete-task': linkOptions({ to: '/inbox' }),
  'connect-system': linkOptions({ to: '/connectors' }),
  'add-people': linkOptions({ to: '/people' }),
} satisfies Record<GettingStartedStepId, unknown>;

type ButtonAnchorProps = ButtonProps & Omit<ComponentPropsWithRef<'a'>, keyof ButtonProps>;

/** A button drawn as the link it is, so it can be opened in a new tab and is announced as a link. */
function ButtonAnchor(props: ButtonAnchorProps) {
  return <Button component="a" {...props} />;
}

type TextAnchorProps = AnchorProps & Omit<ComponentPropsWithRef<'a'>, keyof AnchorProps>;

function TextAnchor(props: TextAnchorProps) {
  return <Anchor {...props} />;
}

const ButtonLink = createLink(ButtonAnchor);
const AnchorLink = createLink(TextAnchor);

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
  const viewer = useAppStore((state) => state.user);
  const [dismissed, setDismissed] = useState(readDismissed);
  if (dismissed || !facts) return null;

  const steps = gettingStartedSteps(facts, viewer);
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
        <ButtonLink {...STEP_LINKS[next.id]} rightSection={<ArrowRight size={16} />}>
          {next.label}
        </ButtonLink>
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
  const viewer = useAppStore((state) => state.user);
  const steps = gettingStartedSteps(facts ?? NO_PROGRESS_YET, viewer);
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
              <AnchorLink {...STEP_LINKS[step.id]} onClick={onNavigate} size="sm" fw={600}>
                {step.done && <VisuallyHidden>Done: </VisuallyHidden>}
                {step.label}
              </AnchorLink>
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
