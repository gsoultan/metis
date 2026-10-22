/**
 * Trying a delivery against an incoming webhook before the partner sends one.
 *
 * Paste what the partner would post and see what Metis would do with it: the
 * same checks a real delivery meets, in the same order, read only. No process
 * moves and the delivery ID is not used up, so the real delivery that follows
 * is treated as the first.
 *
 * The signature is optional. Most setups go wrong in the body and the
 * correlation rule first, and those can be tried before anybody has written
 * the signing code.
 */
import {
  Alert,
  Badge,
  Button,
  Code,
  Collapse,
  Grid,
  Group,
  Modal,
  Stack,
  Text,
  TextInput,
  Textarea,
  ThemeIcon,
  UnstyledButton,
} from '@mantine/core';
import {
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  FlaskConical,
  MinusCircle,
  TriangleAlert,
  XCircle,
} from 'lucide-react';
import { useState } from 'react';

import { V2_SIGNING } from '../../domain/webhookSigning';
import { sampleBodyFor, simulationSteps, simulationVerdict, type StepTone } from '../../domain/webhookSimulation';
import { useSimulateWebhookDelivery } from '../../hooks/useWebhooks';
import type { ApiWebhook, ApiWebhookSimulation } from '../../services/domains/webhookService';
import { errorMessage } from '../../services/shared/errors';

interface Props {
  hook: ApiWebhook | null;
  onClose: () => void;
}

export function SimulateDelivery({ hook, onClose }: Props) {
  const who = hook ? hook.name || hook.message_name : '';
  return (
    <Modal opened={hook !== null} onClose={onClose} title={hook ? `Try a delivery to ${who}` : ''} size="xl">
      {/* Keyed so another webhook starts from its own sample and no result. */}
      {hook && <Simulator key={hook.id} hook={hook} />}
    </Modal>
  );
}

function Simulator({ hook }: { hook: ApiWebhook }) {
  const simulate = useSimulateWebhookDelivery(hook.id);
  const [body, setBody] = useState(() => sampleBodyFor(hook.correlation_expression));
  const [signing, setSigning] = useState(false);
  const [timestamp, setTimestamp] = useState('');
  const [deliveryId, setDeliveryId] = useState('');
  const [signature, setSignature] = useState('');

  const run = () =>
    simulate.mutate({
      body,
      ...(timestamp.trim() && { timestamp: timestamp.trim() }),
      ...(deliveryId.trim() && { delivery_id: deliveryId.trim() }),
      ...(signature.trim() && { signature: signature.trim() }),
    });

  return (
    <Stack gap="md">
      <Text size="sm" c="dimmed">
        Paste what your partner would send and see what Metis would do with it. This is a dry run: no process moves and
        no delivery is recorded.
      </Text>
      <Grid gap="lg">
        <Grid.Col span={{ base: 12, md: 6 }}>
          <Stack gap="sm">
            <Textarea
              label="What the partner sends"
              description="The request body, exactly as it is posted"
              value={body}
              onChange={(event) => setBody(event.currentTarget.value)}
              autosize
              minRows={8}
              maxRows={18}
              styles={{
                input: {
                  fontFamily: 'var(--mantine-font-family-monospace)',
                  fontSize: 12,
                },
              }}
            />
            <UnstyledButton onClick={() => setSigning((open) => !open)} aria-expanded={signing}>
              <Group gap={4}>
                {signing ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                <Text size="sm" fw={500}>
                  Also check a signature (optional)
                </Text>
              </Group>
            </UnstyledButton>
            <Collapse expanded={signing}>
              <Stack gap="xs">
                <TextInput
                  label={V2_SIGNING.timestampHeader}
                  placeholder="1767225600"
                  value={timestamp}
                  onChange={(event) => setTimestamp(event.currentTarget.value)}
                />
                <TextInput
                  label={V2_SIGNING.deliveryIdHeader}
                  placeholder="evt_0001"
                  value={deliveryId}
                  onChange={(event) => setDeliveryId(event.currentTarget.value)}
                />
                <TextInput
                  label={V2_SIGNING.signatureHeader}
                  placeholder="v2=…"
                  value={signature}
                  onChange={(event) => setSignature(event.currentTarget.value)}
                />
                <Text size="xs" c="dimmed">
                  Checked over the body exactly as pasted, so spaces and line breaks count. A timestamp more than{' '}
                  {V2_SIGNING.toleranceMinutes} minutes old is refused, as it would be for real. Metis never shows the
                  signature a body should have had.
                </Text>
              </Stack>
            </Collapse>
            <Group justify="flex-end">
              <Button leftSection={<FlaskConical size={14} />} loading={simulate.isPending} onClick={run}>
                Run simulation
              </Button>
            </Group>
          </Stack>
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 6 }}>
          {simulate.isError && (
            <Alert color="red" variant="light" title="The simulation did not run">
              {errorMessage(simulate.error)}
            </Alert>
          )}
          {simulate.data ? (
            <SimulationResult result={simulate.data} />
          ) : (
            !simulate.isError && (
              <Text size="sm" c="dimmed">
                Run it to see each check a real delivery meets, and which processes it would reach.
              </Text>
            )
          )}
        </Grid.Col>
      </Grid>
    </Stack>
  );
}

const TONE: Record<StepTone, { color: string; icon: typeof CheckCircle2 }> = {
  ok: { color: 'green', icon: CheckCircle2 },
  warn: { color: 'yellow', icon: TriangleAlert },
  bad: { color: 'red', icon: XCircle },
  skip: { color: 'gray', icon: MinusCircle },
};

export function SimulationResult({ result }: { result: ApiWebhookSimulation }) {
  const verdict = simulationVerdict(result);
  const steps = simulationSteps(result);
  const unnamed = result.waiting_total - result.waiting.length;

  return (
    <Stack gap="sm">
      <Group justify="space-between" align="center">
        <Text size="sm" fw={600}>
          What Metis would do
        </Text>
        <Badge color="gray" variant="light" styles={{ label: { textTransform: 'none' } }}>
          Dry run · nothing changed
        </Badge>
      </Group>
      {/* Said in words beside its icon, not as a coloured badge: a filled
          green or a tinted yellow does not carry text at AA in this theme. */}
      <Group gap="xs" wrap="nowrap">
        <ToneIcon tone={verdict.tone} size="md" />
        <Text size="md" fw={600}>
          {verdict.text}
        </Text>
      </Group>

      {steps.map((step) => {
        return (
          <Group key={step.key} gap="sm" align="flex-start" wrap="nowrap">
            <ToneIcon tone={step.tone} size="sm" />
            <div>
              <Text size="sm" fw={500}>
                {step.title}
              </Text>
              {step.detail && (
                <Text size="xs" c="dimmed">
                  {step.detail}
                </Text>
              )}
            </div>
          </Group>
        );
      })}

      {result.waiting.length > 0 && (
        <Stack gap={6} pl={36}>
          {result.waiting.map((waiting) => (
            <div key={`${waiting.instance_id}-${waiting.node_id}`}>
              <Text size="sm">
                {[waiting.process_name || waiting.process_key || 'A process', waiting.business_key]
                  .filter(Boolean)
                  .join(' · ')}
              </Text>
              <Text size="xs" c="dimmed">
                Waiting at &quot;{waiting.node_name || waiting.node_id}&quot; since{' '}
                {new Date(waiting.since).toLocaleString()} · <Code fz={10}>{waiting.instance_id}</Code>
              </Text>
            </div>
          ))}
          {unnamed > 0 && (
            <Text size="xs" c="dimmed">
              and {unnamed} more
            </Text>
          )}
        </Stack>
      )}
    </Stack>
  );
}

function ToneIcon({ tone, size }: { tone: StepTone; size: 'sm' | 'md' }) {
  const { color, icon: Icon } = TONE[tone];
  return (
    <ThemeIcon color={color} variant="light" size={size} radius="xl" aria-hidden>
      <Icon size={size === 'sm' ? 14 : 16} />
    </ThemeIcon>
  );
}
