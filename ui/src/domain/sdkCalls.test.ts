import { describe, expect, it } from 'bun:test';
import { describeCall, requestFor } from './sdkCalls';

describe('requestFor', () => {
  it('omits the version when the caller wants the live one', () => {
    const request = requestFor({
      kind: 'start',
      projectId: 'p1',
      definitionKey: 'refund',
      variables: {},
      version: 0,
    });

    expect(request).toEqual({
      method: 'POST',
      path: '/process/start',
      body: { project_id: 'p1', definition_key: 'refund' },
      headers: undefined,
    });
  });

  it('sends a named version when one was chosen, so a staged model can be tried', () => {
    const request = requestFor({
      kind: 'start',
      projectId: 'p1',
      definitionKey: 'refund',
      variables: { amount: 42.5 },
      version: 3,
    });

    expect(request.body).toEqual({
      project_id: 'p1',
      definition_key: 'refund',
      variables: { amount: 42.5 },
      version: 3,
    });
  });

  it('carries an idempotency key as a header, not in the body', () => {
    const request = requestFor({
      kind: 'start',
      projectId: 'p1',
      definitionKey: 'refund',
      variables: {},
      version: 0,
      idempotencyKey: 'order-4471-start',
    });

    expect(request.headers).toEqual({ 'Idempotency-Key': 'order-4471-start' });
    expect(request.body).not.toHaveProperty('idempotency_key');
  });

  it('names the lock duration with its unit, because a bare one was misread once', () => {
    const request = requestFor({
      kind: 'fetchAndLock',
      topic: 'reverse-charge',
      workerId: 'sandbox-1',
      maxTasks: 1,
      lockDurationMs: 60_000,
    });

    expect(request.body).toEqual({
      topic: 'reverse-charge',
      worker_id: 'sandbox-1',
      max_tasks: 1,
      lock_duration_ms: 60_000,
    });
  });

  it('completes a task at its own path, with no variables key when there are none', () => {
    const request = requestFor({
      kind: 'complete',
      taskId: 'abc-123',
      workerId: 'sandbox-1',
      topic: 't',
      variables: {},
    });

    expect(request.path).toBe('/external-tasks/abc-123/complete');
    expect(request.body).toEqual({ worker_id: 'sandbox-1' });
  });

  it('sends retries and the retry delay on a failure', () => {
    const request = requestFor({
      kind: 'fail',
      taskId: 'abc-123',
      workerId: 'sandbox-1',
      topic: 't',
      errorMessage: 'card declined',
      retries: 2,
      retryTimeoutMs: 10_000,
    });

    expect(request.path).toBe('/external-tasks/abc-123/failure');
    expect(request.body).toEqual({
      worker_id: 'sandbox-1',
      error_message: 'card declined',
      retries: 2,
      retry_timeout_ms: 10_000,
    });
  });

  it('omits an empty correlation key, which means every waiting instance', () => {
    const request = requestFor({
      kind: 'message',
      projectId: 'p1',
      messageName: 'payment.received',
      correlationKey: '',
      variables: {},
    });

    expect(request.body).toEqual({ project_id: 'p1', message_name: 'payment.received' });
  });

  it('keeps a correlation key when one was given, so one instance is addressed', () => {
    const request = requestFor({
      kind: 'message',
      projectId: 'p1',
      messageName: 'payment.received',
      correlationKey: 'order-4471',
      variables: { paid: true },
    });

    expect(request.body).toEqual({
      project_id: 'p1',
      message_name: 'payment.received',
      correlation_key: 'order-4471',
      variables: { paid: true },
    });
  });

  it('broadcasts a signal without a correlation key at all', () => {
    const request = requestFor({
      kind: 'signal',
      projectId: 'p1',
      signalName: 'quarter.closed',
      variables: {},
    });

    expect(request.path).toBe('/processes/signal');
    expect(request.body).toEqual({ project_id: 'p1', signal_name: 'quarter.closed' });
  });
});

describe('describeCall', () => {
  it('describes the intent, not the endpoint', () => {
    expect(
      describeCall({ kind: 'fetchAndLock', topic: 'reverse-charge', workerId: 'w', maxTasks: 1, lockDurationMs: 1 }),
    ).toBe('Ask for work on reverse-charge');
  });
});
