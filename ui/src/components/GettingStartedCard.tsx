import { Badge, Button, Card, CloseButton, Group, Progress, Stack, Text, Timeline, Title, Tooltip, VisuallyHidden } from '@mantine/core';
import { linkOptions } from '@tanstack/react-router';
import { ArrowRight, Check, RefreshCw } from 'lucide-react';
import { useId, useState } from 'react';

import {
  dismissalKey,
  gettingStartedSteps,
  nextStep,
  type GettingStartedFacts,
  type GettingStartedProgress,
  type GettingStartedStepId,
} from '../domain/gettingStarted';
import { useTranslation } from '../i18n/context';
import { useAppStore } from '../store/useAppStore';
import { AnchorLink, ButtonLink } from './RouterLinks';

/**
 * Getting started, drawn two ways: as a card somebody can put away, and as the
 * timeline in the Help drawer. Both draw the same steps from the same facts,
 * so the two cannot disagree about what is done. The rules are in
 * domain/gettingStarted.ts, and the words in src/i18n/catalogues.
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

/** Whether this person hid the card in this project, per dismissalKey(). */
function readDismissed(key: string | undefined): boolean {
  if (key === undefined) return false;
  try {
    return localStorage.getItem(key) === 'true';
  } catch {
    // Private browsing, or storage turned off. Showing the card is the safe
    // answer for somebody who has not said they are finished with it.
    return false;
  }
}

function rememberDismissed(key: string | undefined): void {
  if (key === undefined) return;
  try {
    localStorage.setItem(key, 'true');
  } catch {
    // Hidden for this visit all the same. It comes back next time, which is a
    // smaller failure than refusing to hide it.
  }
}

interface GettingStartedCardProps {
  /** From useGettingStartedProgress(). */
  progress: GettingStartedProgress;
  /** Asks again for whatever failed. */
  onRetry: () => void;
}

/**
 * The checklist for somebody new, until they have done it all or hidden it.
 *
 * Nothing is drawn while the facts are on their way. A card that appears with
 * every step unticked and then ticks them one by one tells somebody who has
 * done it all that they have done nothing. If they cannot be had, the card
 * says so rather than drawing a checklist it cannot vouch for.
 */
export function GettingStartedCard({ progress, onRetry }: GettingStartedCardProps) {
  const { t } = useTranslation();
  const titleId = useId();
  const viewer = useAppStore((state) => state.user);
  const projectId = useAppStore((state) => state.currentProjectId);
  // Read for whoever and wherever this is now: switching projects keeps the
  // card mounted, and what was hidden in one project is not hidden in the next.
  const key = dismissalKey(viewer?.id, projectId);
  const [hiddenThisVisit, setHiddenThisVisit] = useState<ReadonlySet<string | undefined>>(new Set());
  const dismissed = hiddenThisVisit.has(key) || readDismissed(key);
  if (dismissed || progress.state === 'loading') return null;

  // Failed, there are no steps to draw, so there is no next one either.
  const steps = progress.state === 'known' ? gettingStartedSteps(progress.facts, viewer) : [];
  const next = nextStep(steps);
  if (progress.state === 'known' && !next) return null;
  const counts = { done: steps.filter((step) => step.done).length, total: steps.length };

  const dismiss = () => {
    rememberDismissed(key);
    setHiddenThisVisit(new Set(hiddenThisVisit).add(key));
  };

  return (
    <Card withBorder radius="lg" p="lg" component="section" aria-labelledby={titleId}>
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Stack gap={2}>
          <Title order={4} id={titleId}>{t('start.title')}</Title>
          {next && <Text size="sm" c="dimmed">{t('start.progress', counts)}</Text>}
        </Stack>
        <Tooltip label={t('start.hideHint')} withArrow>
          <CloseButton aria-label={t('start.hide')} onClick={dismiss} />
        </Tooltip>
      </Group>
      {!next ? (
        <ProgressUnknown onRetry={onRetry} />
      ) : (
        <>
          <Progress value={(counts.done / counts.total) * 100} mt="md" aria-label={t('start.progressLabel', counts)} />
          <Stack gap="lg" mt="lg" align="flex-start">
            <GettingStartedTimeline progress={progress} onRetry={onRetry} />
            <ButtonLink {...STEP_LINKS[next.id]} rightSection={<ArrowRight size={16} />}>
              {t(next.labelKey)}
            </ButtonLink>
          </Stack>
        </>
      )}
    </Card>
  );
}

/** Said instead of a checklist when what has been done could not be found out. */
function ProgressUnknown({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <Stack gap="xs" mt="md" align="flex-start" role="alert">
      <Text size="sm" fw={600}>{t('start.unknownTitle')}</Text>
      <Text size="sm" c="dimmed">{t('start.unknownHint')}</Text>
      <Button variant="light" size="xs" leftSection={<RefreshCw size={14} />} onClick={onRetry}>
        {t('common.retry')}
      </Button>
    </Stack>
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
  /** From useGettingStartedProgress(). */
  progress: GettingStartedProgress;
  /** Asks again for whatever failed. */
  onRetry: () => void;
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
export function GettingStartedTimeline({ progress, onRetry, onNavigate }: GettingStartedTimelineProps) {
  const { t } = useTranslation();
  const viewer = useAppStore((state) => state.user);
  if (progress.state === 'failed') return <ProgressUnknown onRetry={onRetry} />;
  const facts = progress.state === 'known' ? progress.facts : undefined;
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
                {step.done && <VisuallyHidden>{`${t('start.done')} `}</VisuallyHidden>}
                {t(step.labelKey)}
              </AnchorLink>
              {step.id === next?.id && <Badge size="xs" variant="light">{t('start.next')}</Badge>}
            </Group>
          }
        >
          <Text c="dimmed" size="xs">{t(step.descriptionKey)}</Text>
        </Timeline.Item>
      ))}
    </Timeline>
  );
}
