/**
 * The calls an integration makes, as data.
 *
 * Every call the sandbox can make is described here once, and both the code
 * that *executes* it and the code that *prints it as a snippet* read the same
 * description. That is the whole point: a snippet generated from a second,
 * hand-maintained table would drift from the request the button actually sent,
 * and a copy-pasteable example that does not match what just worked is worse
 * than no example at all.
 *
 * These are the public REST endpoints — the ones the Go SDK and any other
 * client speak — not the Connect API the first-party UI uses for everything
 * else. Exercising the same surface an integrator will is the reason this
 * screen exists; testing the private one would prove nothing about theirs.
 */
import type { ProcessVariables } from '../services/types';

export type SdkCall =
  | {
      kind: 'start';
      projectId: string;
      definitionKey: string;
      variables: ProcessVariables;
      /** 0 means the live version, which is what a caller who does not care sends. */
      version: number;
      /** Optional; a retry must carry the same one. */
      idempotencyKey?: string;
    }
  | {
      kind: 'fetchAndLock';
      topic: string;
      workerId: string;
      maxTasks: number;
      lockDurationMs: number;
    }
  | {
      kind: 'complete';
      taskId: string;
      workerId: string;
      topic: string;
      variables: ProcessVariables;
    }
  | {
      kind: 'fail';
      taskId: string;
      workerId: string;
      topic: string;
      errorMessage: string;
      /** Attempts remaining after this failure. Zero means give up. */
      retries: number;
      retryTimeoutMs: number;
    }
  | {
      kind: 'message';
      projectId: string;
      messageName: string;
      correlationKey: string;
      variables: ProcessVariables;
    }
  | {
      kind: 'signal';
      projectId: string;
      signalName: string;
      variables: ProcessVariables;
    };

export type SdkCallKind = SdkCall['kind'];

export interface SdkRequest {
  method: 'GET' | 'POST';
  /** Relative to the API base — `/process/start`, not the full URL. */
  path: string;
  body?: Record<string, unknown>;
  /** Headers beyond Content-Type and Authorization, which every call carries. */
  headers?: Record<string, string>;
}

/**
 * The HTTP request one call becomes.
 *
 * Optional fields are omitted rather than sent empty: the server reads an
 * absent `version` as "the live one" and an absent `correlation_key` as
 * "every waiting instance", and sending `""` for either is a different
 * request that happens to look the same in a form.
 */
export function requestFor(call: SdkCall): SdkRequest {
  switch (call.kind) {
    case 'start': {
      const body: Record<string, unknown> = {
        project_id: call.projectId,
        definition_key: call.definitionKey,
      };
      if (hasVariables(call.variables)) body.variables = call.variables;
      if (call.version > 0) body.version = call.version;
      const headers = call.idempotencyKey ? { 'Idempotency-Key': call.idempotencyKey } : undefined;
      return { method: 'POST', path: '/process/start', body, headers };
    }
    case 'fetchAndLock':
      return {
        method: 'POST',
        path: '/external-tasks/fetch-and-lock',
        body: {
          topic: call.topic,
          worker_id: call.workerId,
          max_tasks: call.maxTasks,
          // Suffixed with its unit on purpose — a bare `lock_duration` has
          // been misread as seconds inside this codebase before.
          lock_duration_ms: call.lockDurationMs,
        },
      };
    case 'complete': {
      const body: Record<string, unknown> = { worker_id: call.workerId };
      if (hasVariables(call.variables)) body.variables = call.variables;
      return { method: 'POST', path: `/external-tasks/${call.taskId}/complete`, body };
    }
    case 'fail':
      return {
        method: 'POST',
        path: `/external-tasks/${call.taskId}/failure`,
        body: {
          worker_id: call.workerId,
          error_message: call.errorMessage,
          retries: call.retries,
          retry_timeout_ms: call.retryTimeoutMs,
        },
      };
    case 'message': {
      const body: Record<string, unknown> = {
        project_id: call.projectId,
        message_name: call.messageName,
      };
      if (call.correlationKey !== '') body.correlation_key = call.correlationKey;
      if (hasVariables(call.variables)) body.variables = call.variables;
      return { method: 'POST', path: '/processes/message', body };
    }
    case 'signal': {
      const body: Record<string, unknown> = {
        project_id: call.projectId,
        signal_name: call.signalName,
      };
      if (hasVariables(call.variables)) body.variables = call.variables;
      return { method: 'POST', path: '/processes/signal', body };
    }
  }
}

function hasVariables(variables: ProcessVariables): boolean {
  return Object.keys(variables).length > 0;
}

/**
 * What the call did, in the words of the person who pressed the button.
 *
 * The wire log shows `POST /external-tasks/{id}/complete` beside this, so the
 * two together read as "what I meant" and "what went over the wire" — which is
 * the pairing somebody debugging an integration needs.
 */
export function describeCall(call: SdkCall): string {
  switch (call.kind) {
    case 'start':
      return `Start ${call.definitionKey}`;
    case 'fetchAndLock':
      return `Ask for work on ${call.topic}`;
    case 'complete':
      return `Report ${call.topic} done`;
    case 'fail':
      return `Report ${call.topic} failed`;
    case 'message':
      return `Send ${call.messageName}`;
    case 'signal':
      return `Broadcast ${call.signalName}`;
  }
}
