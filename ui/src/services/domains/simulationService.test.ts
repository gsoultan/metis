import { afterEach, describe, expect, it } from 'bun:test';

import { runSimulation } from './simulationService';
import { stubFetch } from '../shared/stubbedFetch';

/*
 * The server answers one run as `{ run }`. Reading the reply as the run itself
 * left every run without an outcome, so a run waiting for an answer was shown
 * as stopped on an incident.
 */
describe('running one simulation', () => {
  let restore = () => {};
  afterEach(() => restore());

  it('reads the run out of the reply', async () => {
    const stub = stubFetch({
      run: {
        run_id: 'run-1',
        definition: { key: 'expense-approval', version: 1, id: 'def-1' },
        outcome: 'needs_answer',
        awaiting_node: 'approve',
        awaiting_label: 'Approve the expense',
        virtual_duration_ms: 0,
        variables: { amount: 4200 },
        incidents: null,
        steps: [{ i: 0, clock: '2026-01-05T09:00:00Z', node: '', event: 'started', tokens: ['start'], note: 'Started' }],
      },
    });
    restore = stub.restore;

    const run = await runSimulation({
      project_id: 'project-1',
      definition_key: 'expense-approval',
      variables: { amount: 4200 },
      answers: [],
      clock_start: '2026-01-05T09:00:00Z',
      seed: 1,
      max_steps: 500,
    });

    expect(run.outcome).toBe('needs_answer');
    expect(run.awaitingNode).toBe('approve');
    expect(run.incidents).toEqual([]);
    expect(run.steps).toHaveLength(1);
  });

  it('reads what a replay read off the instance, with what people entered as rows', async () => {
    const stub = stubFetch({
      run: {
        run_id: 'run-2',
        definition: { key: 'refund', version: 2, id: 'def-2' },
        outcome: 'completed',
        ended_at_node: 'paid',
        virtual_duration_ms: 0,
        variables: {},
        incidents: [],
        steps: [],
        replay: {
          instance_id: 'inst-1',
          version: 1,
          status: 'completed',
          started_with: { amount: 120 },
          answers: [{ node: 'approve', kind: 'person', actor: 'sarah', after: 'PT1H', returns: { approved: true } }],
          path: ['start', 'approve'],
          only_in_run: null,
          only_in_instance: ['decide'],
          notes: null,
        },
      },
    });
    restore = stub.restore;

    const run = await runSimulation({
      project_id: 'project-1',
      replay_instance_id: 'inst-1',
      variables: {},
      answers: [],
      clock_start: '2026-01-05T09:00:00Z',
      seed: 1,
      max_steps: 500,
    });

    expect(run.replay?.startedWith).toEqual({ amount: 120 });
    expect(run.replay?.onlyInRun).toEqual([]);
    expect(run.replay?.notes).toEqual([]);
    const approve = run.replay?.answers[0];
    expect(approve?.kind).toBe('person');
    expect(approve?.kind === 'person' ? approve.returns?.map((r) => [r.name, r.value]) : null).toEqual([['approved', 'true']]);
  });
});

