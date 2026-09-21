import { describe, expect, it } from 'bun:test';
import { defaultFocus, previewCall, stepStates } from './sdkRun';
import type { SandboxForm, LockedTask } from '../hooks/useSdkSandbox';

const form: SandboxForm = {
  definitionKey: 'refund',
  version: 0,
  startVariables: '{"amount": 42.5}',
  idempotencyKey: '',
  topic: 'reverse-charge',
  workerId: 'sandbox-1',
  maxTasks: 1,
  lockDurationMs: 60_000,
  completeVariables: '{"reversed": true}',
  failMessage: '',
  retries: 2,
  retryTimeoutMs: 10_000,
  notifyKind: 'message',
  notifyName: 'payment.received',
  correlationKey: 'order-4471',
  notifyVariables: '',
};

const locked: LockedTask = {
  id: 'task-1',
  topic: 'reverse-charge',
  nodeName: 'Reverse the charge',
  instanceId: 'inst-1',
  variables: {},
  lockExpiration: null,
  retries: 0,
};

describe('stepStates', () => {
  it('starts with nothing chosen and only the first step live', () => {
    expect(stepStates(null, '')).toEqual({
      choose: 'active',
      start: 'waiting',
      work: 'waiting',
      notify: 'waiting',
      watch: 'waiting',
    });
  });

  it('opens starting once a process is chosen, and nothing after it', () => {
    const states = stepStates(null, 'refund');
    expect(states.choose).toBe('done');
    expect(states.start).toBe('active');
    expect(states.work).toBe('waiting');
  });

  it('opens every later step once an instance exists', () => {
    const states = stepStates('inst-1', 'refund');
    expect(states.start).toBe('done');
    expect(states.work).toBe('active');
    expect(states.notify).toBe('active');
    expect(states.watch).toBe('active');
  });
});

describe('defaultFocus', () => {
  it('follows starting until there is an instance', () => {
    expect(defaultFocus({ started: false, holdingTask: false, hasTopics: true })).toBe('start');
  });

  it('follows the worker once an instance is running', () => {
    expect(defaultFocus({ started: true, holdingTask: false, hasTopics: true })).toBe('work');
  });

  it('skips the worker for a process with no topics, which would show nothing', () => {
    expect(defaultFocus({ started: true, holdingTask: false, hasTopics: false })).toBe('notify');
  });

  it('follows a held task above everything, because a lock is running out', () => {
    expect(defaultFocus({ started: true, holdingTask: true, hasTopics: false })).toBe('work');
  });
});

describe('previewCall', () => {
  it('shows nothing to start until a process is chosen', () => {
    expect(previewCall('start', 'p1', { ...form, definitionKey: '' }, null)).toBeNull();
  });

  it('builds the start call from what is typed, before anything is sent', () => {
    expect(previewCall('start', 'p1', form, null)).toEqual({
      kind: 'start',
      projectId: 'p1',
      definitionKey: 'refund',
      variables: { amount: 42.5 },
      version: 0,
      idempotencyKey: undefined,
    });
  });

  it('treats half-typed JSON as no variables rather than making the snippet vanish', () => {
    const call = previewCall('start', 'p1', { ...form, startVariables: '{"amount":' }, null);
    expect(call).toMatchObject({ kind: 'start', variables: {} });
  });

  it('shows the fetch while no task is held, and the completion once one is', () => {
    expect(previewCall('work', 'p1', form, null)).toMatchObject({ kind: 'fetchAndLock', topic: 'reverse-charge' });
    expect(previewCall('work', 'p1', form, locked)).toMatchObject({
      kind: 'complete',
      taskId: 'task-1',
      variables: { reversed: true },
    });
  });

  it('shows nothing to fetch when no topic is selected', () => {
    expect(previewCall('work', 'p1', { ...form, topic: '' }, null)).toBeNull();
  });

  it('switches between a message and a signal with the toggle', () => {
    expect(previewCall('notify', 'p1', form, null)).toMatchObject({
      kind: 'message',
      messageName: 'payment.received',
      correlationKey: 'order-4471',
    });
    expect(previewCall('notify', 'p1', { ...form, notifyKind: 'signal' }, null)).toMatchObject({
      kind: 'signal',
      signalName: 'payment.received',
    });
  });

  it('trims the name, so a pasted value with a trailing space is not a second event', () => {
    expect(previewCall('notify', 'p1', { ...form, notifyName: ' payment.received ' }, null)).toMatchObject({
      messageName: 'payment.received',
    });
  });

  it('has nothing to copy for watching, which is a read the page does for you', () => {
    expect(previewCall('watch', 'p1', form, locked)).toBeNull();
  });
});
