/**
 * The state of one sandbox run.
 *
 * A run is a journey rather than a form: you start an instance, the engine
 * parks work on a topic, you pull it as a worker, you report back, and the
 * process moves on. Each step needs what the step before produced — the
 * instance id, the locked task's id — so the state is held in one place and
 * the panels read slices of it.
 *
 * Everything derivable is derived during render. The only stored state is what
 * a person typed and what the server said.
 */
import { useCallback, useMemo, useState } from 'react';
import { v4 as uuidv4 } from 'uuid';

import type { SdkCall } from '../domain/sdkCalls';
import { appendCall, type WireCall } from '../domain/sdkWireLog';
import { parseVariables } from '../domain/variablesInput';
import { executeSdkCall } from '../services/domains/sdkSandboxService';
import type { ProcessVariables } from '../services/types';

/** A task this sandbox currently holds the lock on. */
export interface LockedTask {
  id: string;
  topic: string;
  /** The step in the model, named as the modeller named it. */
  nodeName: string;
  instanceId: string | null;
  variables: Record<string, unknown>;
  /** ISO timestamp; null when the server did not say. */
  lockExpiration: string | null;
  retries: number;
}

export interface SandboxForm {
  definitionKey: string;
  version: number;
  startVariables: string;
  idempotencyKey: string;
  topic: string;
  workerId: string;
  maxTasks: number;
  lockDurationMs: number;
  completeVariables: string;
  failMessage: string;
  retries: number;
  retryTimeoutMs: number;
  /** 'message' addresses one instance; 'signal' is broadcast to all of them. */
  notifyKind: 'message' | 'signal';
  notifyName: string;
  correlationKey: string;
  notifyVariables: string;
}

const DEFAULTS: Omit<SandboxForm, 'workerId'> = {
  definitionKey: '',
  version: 0,
  startVariables: '',
  idempotencyKey: '',
  topic: '',
  maxTasks: 1,
  // A minute: long enough for real work, short enough that a crashed worker's
  // tasks come back while somebody is still watching this screen. It is the
  // server's own default for a request that does not say.
  lockDurationMs: 60_000,
  completeVariables: '',
  failMessage: '',
  retries: 2,
  retryTimeoutMs: 10_000,
  notifyKind: 'message',
  notifyName: '',
  correlationKey: '',
  notifyVariables: '',
};

export function useSdkSandbox() {
  // Minted once per mount and shown, not hidden: the worker id is what the
  // engine records against the lock, so seeing it here is what lets somebody
  // recognise their own sandbox in an incident later.
  const [form, setForm] = useState<SandboxForm>(() => ({
    ...DEFAULTS,
    workerId: `sandbox-${uuidv4().slice(0, 8)}`,
  }));
  const [log, setLog] = useState<WireCall[]>([]);
  const [instanceId, setInstanceId] = useState<string | null>(null);
  const [lockedTask, setLockedTask] = useState<LockedTask | null>(null);
  const [noWorkOn, setNoWorkOn] = useState<string | null>(null);
  const [pending, setPending] = useState<SdkCall['kind'] | null>(null);

  const patch = useCallback((change: Partial<SandboxForm>) => {
    setForm((current) => ({ ...current, ...change }));
  }, []);

  /** Runs a call, records it, and hands back what the server said. */
  const run = useCallback(async (call: SdkCall) => {
    setPending(call.kind);
    try {
      const outcome = await executeSdkCall(call);
      setLog((current) => appendCall(current, outcome.record));
      return outcome;
    } finally {
      setPending(null);
    }
  }, []);

  const startInstance = useCallback(
    async (projectId: string) => {
      const variables = parseVariables(form.startVariables);
      if (!variables.ok) return;

      const outcome = await run({
        kind: 'start',
        projectId,
        definitionKey: form.definitionKey,
        variables: variables.value,
        version: form.version,
        idempotencyKey: form.idempotencyKey.trim() === '' ? undefined : form.idempotencyKey.trim(),
      });

      const id = typeof outcome.body?.instance_id === 'string' ? outcome.body.instance_id : null;
      if (outcome.ok && id !== null) {
        setInstanceId(id);
        setLockedTask(null);
        setNoWorkOn(null);
      }
    },
    [form.definitionKey, form.idempotencyKey, form.startVariables, form.version, run],
  );

  const fetchAndLock = useCallback(async () => {
    const outcome = await run({
      kind: 'fetchAndLock',
      topic: form.topic,
      workerId: form.workerId,
      maxTasks: form.maxTasks,
      lockDurationMs: form.lockDurationMs,
    });

    const task = firstTask(outcome.body);
    if (task === null) {
      setLockedTask(null);
      // An empty list is a legitimate, common answer — the process has not
      // reached that step yet — so it is reported as a state of the run rather
      // than as a failed call.
      setNoWorkOn(outcome.ok ? form.topic : null);
      return;
    }
    setNoWorkOn(null);
    setLockedTask(task);
  }, [form.lockDurationMs, form.maxTasks, form.topic, form.workerId, run]);

  const completeTask = useCallback(async () => {
    if (lockedTask === null) return;
    const variables = parseVariables(form.completeVariables);
    if (!variables.ok) return;

    const outcome = await run({
      kind: 'complete',
      taskId: lockedTask.id,
      workerId: form.workerId,
      topic: lockedTask.topic,
      variables: variables.value,
    });
    if (outcome.ok) {
      setLockedTask(null);
    }
  }, [form.completeVariables, form.workerId, lockedTask, run]);

  const failTask = useCallback(async () => {
    if (lockedTask === null) return;
    const outcome = await run({
      kind: 'fail',
      taskId: lockedTask.id,
      workerId: form.workerId,
      topic: lockedTask.topic,
      errorMessage: form.failMessage.trim() === '' ? 'Reported failed from the SDK sandbox' : form.failMessage.trim(),
      retries: form.retries,
      retryTimeoutMs: form.retryTimeoutMs,
    });
    if (outcome.ok) {
      setLockedTask(null);
    }
  }, [form.failMessage, form.retries, form.retryTimeoutMs, form.workerId, lockedTask, run]);

  const notify = useCallback(
    async (projectId: string) => {
      const variables = parseVariables(form.notifyVariables);
      if (!variables.ok) return;

      await run(
        form.notifyKind === 'message'
          ? {
              kind: 'message',
              projectId,
              messageName: form.notifyName,
              correlationKey: form.correlationKey.trim(),
              variables: variables.value,
            }
          : {
              kind: 'signal',
              projectId,
              signalName: form.notifyName,
              variables: variables.value,
            },
      );
    },
    [form.correlationKey, form.notifyKind, form.notifyName, form.notifyVariables, run],
  );

  const reset = useCallback(() => {
    setForm((current) => ({ ...DEFAULTS, workerId: current.workerId }));
    setInstanceId(null);
    setLockedTask(null);
    setNoWorkOn(null);
    setLog([]);
  }, []);

  const clearLog = useCallback(() => setLog([]), []);

  /** Parsed once per render and shared, so three panels do not each re-parse. */
  const variableErrors = useMemo(
    () => ({
      start: errorIn(form.startVariables),
      complete: errorIn(form.completeVariables),
      notify: errorIn(form.notifyVariables),
    }),
    [form.completeVariables, form.notifyVariables, form.startVariables],
  );

  return {
    form,
    patch,
    log,
    clearLog,
    instanceId,
    lockedTask,
    noWorkOn,
    pending,
    variableErrors,
    startInstance,
    fetchAndLock,
    completeTask,
    failTask,
    notify,
    reset,
  };
}

function errorIn(text: string): string | null {
  const parsed = parseVariables(text);
  return parsed.ok ? null : parsed.message;
}

/**
 * The first task out of a fetch-and-lock reply.
 *
 * `max_tasks` is 1 by default here on purpose: a sandbox that locked five tasks
 * and showed one would leave four invisible and locked until they expired,
 * which looks exactly like a stuck process to whoever is watching.
 */
function firstTask(body: Record<string, unknown> | null): LockedTask | null {
  const tasks = body?.tasks;
  if (!Array.isArray(tasks) || tasks.length === 0) {
    return null;
  }
  const task = tasks[0] as {
    id?: string;
    topic?: string;
    node?: { name?: string; id?: string };
    process_instance?: { id?: string };
    variables?: ProcessVariables;
    lock_expiration?: string;
    retries?: number;
  };
  if (typeof task.id !== 'string') {
    return null;
  }
  return {
    id: task.id,
    topic: typeof task.topic === 'string' ? task.topic : '',
    nodeName: task.node?.name?.trim() || task.node?.id || 'this step',
    instanceId: task.process_instance?.id ?? null,
    variables: task.variables ?? {},
    lockExpiration: typeof task.lock_expiration === 'string' ? task.lock_expiration : null,
    retries: typeof task.retries === 'number' ? task.retries : 0,
  };
}
