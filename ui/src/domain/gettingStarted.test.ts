import { describe, expect, it } from 'bun:test';

import {
  gettingStartedFacts,
  gettingStartedSteps,
  nextStep,
  type GettingStartedFacts,
  type GettingStartedQueries,
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

/** Somebody who may do every step. */
const ADMINISTRATOR = { roles: ['ADMIN'] };

/** A query that has answered this, for the project asked about. */
function answered<T>(data: T) {
  return { data, isPlaceholderData: false };
}

/** A query still on its way, with nothing to show. */
const LOADING = { data: undefined, isPlaceholderData: false };

/** What the hooks return for a project set up a moment ago: every query answered, and empty. */
const FRESH_PROJECT: GettingStartedQueries = {
  definitions: answered({ definitions: [] }),
  instances: answered({ instances: [] }),
  statistics: answered({ stats: { completedTasks: 0 } }),
  connections: answered({ instances: [] }),
  people: answered({ participants: [] }),
};

describe('the steps', () => {
  /* Where each is done is the card's to link to; see GettingStartedCard.test.ts. */
  it('run in the order the product is first used', () => {
    expect(gettingStartedSteps(NOTHING_DONE, ADMINISTRATOR).map((step) => step.id)).toEqual([
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
    for (const step of gettingStartedSteps(NOTHING_DONE, ADMINISTRATOR)) {
      expect(`${step.label} ${step.description}`.toLowerCase()).not.toContain('project');
    }
  });

  /*
   * The templates are on the Dashboard and nowhere else. The deploy step links
   * to Processes, so telling somebody there to "start from a template" sent
   * them looking for something that page does not have.
   */
  it('says the templates are on the Dashboard, since the deploy step links to Processes', () => {
    const deploy = gettingStartedSteps(NOTHING_DONE, ADMINISTRATOR).find((step) => step.id === 'deploy-process');
    expect(deploy?.description).toMatch(/templates? on the Dashboard/);
  });

  it('says what each step is for, in a sentence', () => {
    for (const step of gettingStartedSteps(NOTHING_DONE, ADMINISTRATOR)) {
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
    const done = gettingStartedSteps({ ...NOTHING_DONE, [fact]: true }, ADMINISTRATOR)
      .filter((step) => step.done)
      .map((step) => step.id);
    expect(done).toEqual([stepId]);
  });
});

/*
 * The server decides who may do each step (server/endpoints/endpoints.go):
 * deploying and importing people take the designer role or the
 * administrator's, setting up a connection the administrator's, and starting
 * an instance or completing a task only a sign-in. A step somebody cannot do
 * is not theirs to be told to do: its link leads to a page whose button the
 * server refuses them. Those steps are left out of their list.
 */
describe('the steps somebody is shown', () => {
  const ids = (viewer: Parameters<typeof gettingStartedSteps>[1]) =>
    gettingStartedSteps(NOTHING_DONE, viewer).map((step) => step.id);

  it('are all of them for an administrator', () => {
    expect(ids(ADMINISTRATOR)).toEqual(['deploy-process', 'start-instance', 'complete-task', 'connect-system', 'add-people']);
  });

  it('leave out setting up a connection for a designer, which only an administrator may do', () => {
    expect(ids({ roles: ['DESIGNER'] })).toEqual(['deploy-process', 'start-instance', 'complete-task', 'add-people']);
  });

  it('are starting an instance and completing a task for somebody with no role', () => {
    expect(ids({ roles: [] })).toEqual(['start-instance', 'complete-task']);
    expect(ids({ roles: ['OPERATOR'] })).toEqual(['start-instance', 'complete-task']);
  });

  it('read every role held, whatever its case', () => {
    expect(ids({ role: 'operator, admin' })).toEqual(ids(ADMINISTRATOR));
    expect(ids({ roles: ['designer'] })).toContain('deploy-process');
  });

  it('are only the ones anybody may do when nobody is known to be signed in', () => {
    expect(ids(null)).toEqual(['start-instance', 'complete-task']);
  });

  /* Done by somebody else still counts as done; being hidden does not change what is next among the rest. */
  it('put next the earliest of their own steps not done', () => {
    const steps = gettingStartedSteps({ ...NOTHING_DONE, instanceStarted: true }, { roles: [] });
    expect(nextStep(steps)?.id).toBe('complete-task');
  });
});

describe('what to do next', () => {
  it('is the first step for somebody who has done nothing', () => {
    expect(nextStep(gettingStartedSteps(NOTHING_DONE, ADMINISTRATOR))?.id).toBe('deploy-process');
  });

  /*
   * Progress can be made out of order: an administrator sets up the email
   * connection before anybody has drawn a process. The next step is still the
   * first one not done, because starting an instance needs a process to start.
   */
  it('is the earliest step not done, whatever was done out of order', () => {
    const steps = gettingStartedSteps({ ...NOTHING_DONE, connectionSetUp: true, peopleAdded: true }, ADMINISTRATOR);
    expect(nextStep(steps)?.id).toBe('deploy-process');
  });

  it('moves on as steps are done', () => {
    const steps = gettingStartedSteps({ ...NOTHING_DONE, processDeployed: true, instanceStarted: true }, ADMINISTRATOR);
    expect(nextStep(steps)?.id).toBe('complete-task');
  });

  it('is nothing once every step is done', () => {
    expect(nextStep(gettingStartedSteps(EVERYTHING_DONE, ADMINISTRATOR))).toBeUndefined();
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
  it.each(Object.keys(FRESH_PROJECT) as (keyof GettingStartedQueries)[])(
    'has no answer while %s is still loading',
    (source) => {
      expect(gettingStartedFacts({ ...FRESH_PROJECT, [source]: LOADING })).toBeUndefined();
    },
  );

  /*
   * The definitions and instances lists keep the previous rows on screen while
   * the next ones load, so a table does not collapse on every page. After a
   * project switch those rows are the last project's: counted as facts, they
   * ticked "Deploy a process" for a project that has deployed nothing, until
   * its own answer landed. A placeholder is not an answer.
   */
  it.each([
    ['definitions', { definitions: [{ id: 'from-the-last-project' }] }],
    ['instances', { instances: [{ id: 'from-the-last-project' }] }],
  ] as const)("has no answer while %s shows the last project's rows as a placeholder", (source, rows) => {
    const facts = gettingStartedFacts({ ...FRESH_PROJECT, [source]: { data: rows, isPlaceholderData: true } });
    expect(facts).toBeUndefined();
  });

  it.each([
    ['definitions', answered({ definitions: [{ id: 'one' }] }), 'processDeployed'],
    ['instances', answered({ instances: [{ id: 'one' }] }), 'instanceStarted'],
    ['connections', answered({ instances: [{ id: 'one' }] }), 'connectionSetUp'],
    ['people', answered({ participants: [{ id: 'one' }] }), 'peopleAdded'],
  ] as const)('counts one of %s', (source, query, fact) => {
    const facts = gettingStartedFacts({ ...FRESH_PROJECT, [source]: query });
    expect(facts).toEqual({ ...NOTHING_DONE, [fact]: true });
  });

  /*
   * "Complete a task" was read from the newest 200 tasks, so a project with
   * 200 open tasks newer than its one completed task was told nobody had
   * completed one. The project statistics count completed tasks across all of
   * it, and the dashboard loads them anyway.
   */
  it('counts completed tasks from the project statistics, however many tasks are newer', () => {
    const completed = (count: number) =>
      gettingStartedFacts({ ...FRESH_PROJECT, statistics: answered({ stats: { completedTasks: count } }) });
    expect(completed(1)?.taskCompleted).toBe(true);
    expect(completed(0)?.taskCompleted).toBe(false);
  });
});
