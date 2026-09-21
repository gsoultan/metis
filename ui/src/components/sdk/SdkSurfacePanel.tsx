/**
 * The contract the model implies, read off the model.
 *
 * This is the part nobody can get from the diagram without clicking every node:
 * the exact topic string a worker must subscribe to, the exact message name the
 * process is waiting for, and which side does the work in each case. The names
 * are matched exactly by the engine, so `reverse-charge` and `reverseCharge`
 * are two different integrations and nothing reports the mismatch — which is
 * why they are shown as copyable text rather than described in a sentence.
 */
import { Badge, Button, CopyButton, Group, Stack, Text, ThemeIcon, Tooltip } from '@mantine/core';
import { Bell, Check, Copy, Inbox, Radio, UserRound } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import { describeRole, type IntegrationSurface, type SurfacePoint } from '../../domain/sdkSurface';

interface SdkSurfacePanelProps {
  surface: IntegrationSurface;
  selectedTopic: string;
  selectedNotify: string;
  onUseTopic: (topic: string) => void;
  onUseNotify: (point: SurfacePoint) => void;
}

export function SdkSurfacePanel({
  surface,
  selectedTopic,
  selectedNotify,
  onUseTopic,
  onUseNotify,
}: SdkSurfacePanelProps) {
  if (surface.isEmpty) {
    return (
      <Text size="sm" c="dimmed">
        Nothing in this model reaches outside the engine — no external topics, no messages, no
        signals and no human steps. You can still start it here and watch it run.
      </Text>
    );
  }

  return (
    <Stack gap="lg">
      <SurfaceGroup
        icon={Radio}
        colour="blue"
        title="Your worker pulls these"
        intent="you-act"
        points={surface.topics}
        selected={selectedTopic}
        actionLabel="Use this topic"
        onUse={(point) => onUseTopic(point.name)}
      />
      <SurfaceGroup
        icon={Inbox}
        colour="grape"
        title="The process waits for these"
        intent="you-notify"
        points={surface.inbound}
        selected={selectedNotify}
        actionLabel="Send this"
        onUse={onUseNotify}
      />
      <SurfaceGroup
        icon={Bell}
        colour="teal"
        title="The process announces these"
        intent="you-listen"
        points={surface.outbound}
        selected=""
      />

      {surface.humanSteps.length > 0 && (
        <Group gap="sm" align="flex-start" wrap="nowrap">
          <ThemeIcon size={26} radius="md" variant="light" color="gray" aria-hidden>
            <UserRound size={14} />
          </ThemeIcon>
          <div>
            <Text size="sm" fw={500}>
              {surface.humanSteps.length === 1
                ? '1 step is completed by a person'
                : `${surface.humanSteps.length} steps are completed by a person`}
            </Text>
            <Text size="xs" c="dimmed">
              {describeRole('a-person-acts')}
            </Text>
          </div>
        </Group>
      )}
    </Stack>
  );
}

interface SurfaceGroupProps {
  icon: LucideIcon;
  colour: string;
  title: string;
  intent: Parameters<typeof describeRole>[0];
  points: SurfacePoint[];
  selected: string;
  actionLabel?: string;
  onUse?: (point: SurfacePoint) => void;
}

function SurfaceGroup({ icon: Icon, colour, title, intent, points, selected, actionLabel, onUse }: SurfaceGroupProps) {
  if (points.length === 0) {
    return null;
  }

  return (
    <Stack gap={8}>
      <Group gap="sm" wrap="nowrap" align="flex-start">
        <ThemeIcon size={26} radius="md" variant="light" color={colour} aria-hidden>
          <Icon size={14} />
        </ThemeIcon>
        <div style={{ minWidth: 0 }}>
          <Text size="sm" fw={500}>
            {title}
          </Text>
          <Text size="xs" c="dimmed">
            {describeRole(intent)}
          </Text>
        </div>
      </Group>

      <Stack gap={4} pl={38}>
        {points.map((point) => (
          <Group key={`${point.nodeId}-${point.name}`} gap="xs" wrap="nowrap" justify="space-between">
            <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
              <Badge
                variant={point.name === selected && selected !== '' ? 'filled' : 'light'}
                color={colour}
                radius="sm"
                styles={{ label: { fontFamily: 'var(--mantine-font-family-monospace)', textTransform: 'none' } }}
              >
                {point.name}
              </Badge>
              <Text size="xs" c="dimmed" truncate>
                {point.nodeName}
              </Text>
              {point.correlationKey !== undefined && (
                <Tooltip
                  withArrow
                  label={`Addressed by ${point.correlationKey} — send that value as the correlation key so the right instance hears it`}
                >
                  <Text size="xs" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                    · keyed by {point.correlationKey}
                  </Text>
                </Tooltip>
              )}
            </Group>

            <Group gap={2} wrap="nowrap">
              <CopyButton value={point.name} timeout={1200}>
                {({ copied, copy }) => (
                  <Tooltip label={copied ? 'Copied' : `Copy “${point.name}”`} withArrow>
                    <Button
                      size="compact-xs"
                      variant="subtle"
                      color="gray"
                      onClick={copy}
                      aria-label={`Copy the name ${point.name}`}
                    >
                      {copied ? <Check size={12} /> : <Copy size={12} />}
                    </Button>
                  </Tooltip>
                )}
              </CopyButton>
              {onUse && actionLabel && (
                <Button
                  size="compact-xs"
                  variant="light"
                  onClick={() => onUse(point)}
                  aria-label={`${actionLabel}: ${point.name}`}
                >
                  {actionLabel}
                </Button>
              )}
            </Group>
          </Group>
        ))}
      </Stack>
    </Stack>
  );
}
