/**
 * What a simulated delivery says, step by step, in the order a real delivery
 * meets each check.
 *
 * The server does the checking; this only decides how each answer reads. It is
 * kept apart from the panel so every wording can be tested without drawing it.
 */
import type { ApiWebhookSimulation } from '../services/domains/webhookService';
import { V2_SIGNING } from './webhookSigning';

export type StepTone = 'ok' | 'warn' | 'bad' | 'skip';

export interface SimulationStep {
  key: string;
  tone: StepTone;
  title: string;
  detail: string;
}

export interface SimulationVerdict {
  tone: 'ok' | 'warn' | 'bad';
  text: string;
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function simulationSteps(result: ApiWebhookSimulation): SimulationStep[] {
  const steps: SimulationStep[] = [];

  if (!result.enabled) {
    steps.push({
      key: 'enabled',
      tone: 'bad',
      title: 'This webhook is switched off',
      detail: 'A real delivery would be refused whatever it carried. Switch it on when the sender is ready.',
    });
  }

  const { signature } = result;
  if (!signature.checked) {
    steps.push({
      key: 'signature',
      tone: 'skip',
      title: 'Signature not checked',
      detail: `None was given. A real delivery must carry ${V2_SIGNING.timestampHeader}, ${V2_SIGNING.deliveryIdHeader} and ${V2_SIGNING.signatureHeader}.`,
    });
  } else if (signature.valid) {
    steps.push({
      key: 'signature',
      tone: 'ok',
      title: 'Signature checks out',
      detail: "Signed with this webhook's secret, and recently enough.",
    });
  } else {
    steps.push({
      key: 'signature',
      tone: 'bad',
      title: 'The signature is not accepted',
      detail: signature.problem ?? '',
    });
  }

  if (result.body_problem) {
    steps.push({
      key: 'body',
      tone: 'bad',
      title: 'Could not tell which process this is about',
      detail: result.body_problem,
    });
    return steps;
  }
  if (result.correlation_expression) {
    steps.push({
      key: 'body',
      tone: 'ok',
      title: 'Found which process this is about',
      detail: `Your rule ${result.correlation_expression} gives "${result.correlation_key ?? ''}".`,
    });
  } else {
    steps.push({
      key: 'body',
      tone: 'ok',
      title: 'Read the delivery',
      detail: `This webhook has no rule picking one process, so a delivery starts the processes listening for "${result.message_name}".`,
    });
  }

  if (result.already_received) {
    steps.push({
      key: 'duplicate',
      tone: 'warn',
      title: 'This delivery ID was already received',
      detail:
        'A delivery with this ID was acted on in the last two days. A real one would be answered as a duplicate and change nothing.',
    });
  }

  const starts = result.starts ?? [];
  if (result.waiting_total > 0) {
    steps.push({
      key: 'recipients',
      tone: 'ok',
      title: `${plural(result.waiting_total, 'process is', 'processes are')} waiting for it`,
      detail: 'A real delivery would move each of them on from the step it is waiting at.',
    });
  }
  if (starts.length > 0) {
    steps.push({
      key: 'starts',
      tone: 'ok',
      title: `It would start ${plural(starts.length, 'process', 'processes')}`,
      detail: starts.map((start) => start.process_name || start.process_key).join(', '),
    });
  }
  if (result.waiting_total === 0 && starts.length === 0) {
    const forKey = result.correlation_key ? ` for "${result.correlation_key}"` : '';
    steps.push({
      key: 'recipients',
      tone: 'warn',
      title: result.correlation_key ? `Nothing is waiting for "${result.correlation_key}"` : 'Nothing is waiting for it',
      detail: `No process is waiting for the message "${result.message_name}"${forKey}. A real delivery would be accepted and change nothing.`,
    });
  }
  return steps;
}

export function simulationVerdict(result: ApiWebhookSimulation): SimulationVerdict {
  if (result.refusal) return { tone: 'bad', text: 'A real delivery would be refused' };
  if (result.would_act) {
    return result.signature.checked
      ? { tone: 'ok', text: 'A real delivery would be acted on' }
      : {
          tone: 'ok',
          text: 'A real delivery would be acted on, once it is signed',
        };
  }
  return { tone: 'warn', text: 'A real delivery would change nothing' };
}

/**
 * A body to start from: the correlation rule's field filled in, so the first
 * run says something useful. A rule that is not a plain dotted path gets an
 * empty object, because guessing at FEEL would only mislead.
 */
export function sampleBodyFor(expression?: string): string {
  const path = expression?.trim() ?? '';
  if (!/^[A-Za-z_]\w*(\.[A-Za-z_]\w*)*$/.test(path)) return '{}';
  const sample = path
    .split('.')
    .reverse()
    .reduce<unknown>((inner, field) => ({ [field]: inner }), 'ORD-1');
  return JSON.stringify(sample, null, 2);
}
