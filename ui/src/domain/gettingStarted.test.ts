import { describe, expect, it } from 'bun:test';

import {
  gettingStartedFacts,
  gettingStartedSteps,
  nextStep,
  type GettingStartedFacts,
  type GettingStartedSources,
} from './gettingStarted';

const NOTHING_DONE: GettingStartedFacts = {
  processDeployed: false,
  instanceStarted: false,
  taskCompleted: false,
  connectionSetUp: false,
  peopleAdded: false,
};

const EVERYTHING_DONE: GettingStartedFacts = {
  processDeployed: true,
  instanceStarted: true,
  taskCompleted: true,
  connectionSetUp: true,
  peopleAdded: true,
};

/** What the hooks return for a project set up a moment ago: every list loaded, and empty. */
const FRESH_PROJECT: Required<GettingStartedSources> = {
  definitions: [],
  instances: [],
  tasks: [],
  connections: [],
  people: [],
};

describe('the steps', () => {
  /* Where each is done is the card's to link to; see GettingStartedCard.test.ts. */
  it('run in the order the product is first used', () => {
    expect(gettingStartedSteps(NOTHING_DONE).map((step) => step.id)).toEqual([
      'deploy-process',
      'start-instance',
      'complete-task',
      'connect-system',
      'add-people',
    ]);
  });

  /*
   * The timeline this replaces opened on "Create a project". Setup creates one,
   * so for everybody who could read it that step was already done, and it could
   * never be ticked.
   */
  it('never asks for a project, which setup has already made', () => {
    for (const step of gettingStartedSteps(NOTHING_DONE)) {
      expect(`${step.label} ${step.description}`.toLowerCase()).not.toContain('project');
    }
  });

  /*
   * The templates are on the Dashboard and nowhere else. The deploy step links
   * to Processes, so telling somebody there to "start from a template" sent
   * them looking for something that page does not have.
   */
  it('says the templates are on the Dashboard, since the deploy step links to Processes', () => {
    const deploy = gettingStartedSteps(NOTHING_DONE).find((step) => step.id === 'deploy-process');
    expect(deploy?.description).toMatch(/templates? on the Dashboard/);
  });

  it('says what each step is for, in a sentence', () => {
    for (const step of gettingStartedSteps(NOTHING_DONE)) {
      expect(step.label.length).toBeGreaterThan(0);
      expect(step.description).toMatch(/\.$/);
    }
  });

  it.each([
    ['processDeployed', 'deploy-process'],
    ['instanceStarted', 'start-instance'],
    ['taskCompleted', 'complete-task'],
    ['connectionSetUp', 'connect-system'],
    ['peopleAdded', 'add-people'],
  ] as const)('ticks one step when %s, and only that one', (fact, stepId) => {
    const done = gettingStartedSteps({ ...NOTHING_DONE, [fact]: true })
      .filter((step) => step.done)
      .map((step) => step.id);
    expect(done).toEqual([stepId]);
  });
});

describe('what to do next', () => {
  it('is the first step for somebody who has done nothing', () => {
    expect(nextStep(gettingStartedSteps(NOTHING_DONE))?.id).toBe('deploy-process');
  });

  /*
   * Progress can be made out of order: an administrator sets up the email
   * connection before anybody has drawn a process. The next step is still the
   * first one not done, because starting an instance needs a process to start.
   */
  it('is the earliest step not done, whatever was done out of order', () => {
    const steps = gettingStartedSteps({ ...NOTHING_DONE, connectionSetUp: true, peopleAdded: true });
    expect(nextStep(steps)?.id).toBe('deploy-process');
  });

  it('moves on as steps are done', () => {
    const steps = gettingStartedSteps({ ...NOTHING_DONE, processDeployed: true, instanceStarted: true });
    expect(nextStep(steps)?.id).toBe('complete-task');
  });

  it('is nothing once every step is done', () => {
    expect(nextStep(gettingStartedSteps(EVERYTHING_DONE))).toBeUndefined();
  });
});

describe('reading the facts from what the interface already fetches', () => {
  it('finds nothing done in a project set up a moment ago', () => {
    expect(gettingStartedFacts(FRESH_PROJECT)).toEqual(NOTHING_DONE);
  });

  /*
   * A list that has not arrived yet is not an empty list. Answering "nothing
   * done" while the requests are in flight would flash a checklist of undone
   * steps at somebody who has done them all.
   */
  it.each(Object.keys(FRESH_PROJECT) as (keyof GettingStartedSources)[])(
    'has no answer while %s is still loading',
    (source) => {
      expect(gettingStartedFacts({ ...FRESH_PROJECT, [source]: undefined })).toBeUndefined();
    },
  );

  it.each([
    ['definitions', 'processDeployed'],
    ['instances', 'instanceStarted'],
    ['connections', 'connectionSetUp'],
    ['people', 'peopleAdded'],
  ] as const)('counts one of %s as %s', (source, fact) => {
    const facts = gettingStartedFacts({ ...FRESH_PROJECT, [source]: [{ id: 'one' }] });
    expect(facts).toEqual({ ...NOTHING_DONE, [fact]: true });
  });

  /* A task that is waiting, or claimed and still open, is not a task somebody completed. */
  it('counts only a completed task as a task completed', () => {
    const open = gettingStartedFacts({ ...FRESH_PROJECT, tasks: [{ status: 'unclaimed' }, { status: 'claimed' }] });
    expect(open?.taskCompleted).toBe(false);

    const finished = gettingStartedFacts({ ...FRESH_PROJECT, tasks: [{ status: 'claimed' }, { status: 'completed' }] });
    expect(finished?.taskCompleted).toBe(true);
  });
});
