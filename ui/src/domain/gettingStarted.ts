/**
 * What somebody new to Metis has done so far, and what to do next.
 *
 * The Help drawer had a "Getting started" timeline that never moved. Nothing on
 * it was ever ticked, nothing linked anywhere, and its first step was "Create a
 * project", which setup has already done for everybody who can read it. A
 * checklist that cannot say where you are is a paragraph.
 *
 * Every step here is answered from something the interface already fetches:
 * the project's processes, instances, tasks, connections and people. Progress
 * is read, not remembered, so a step a colleague did, or one done in another
 * browser, is ticked all the same, and there is nothing to keep in step.
 *
 * The order is the first loop through the product: build a process, run it, do
 * the work it hands out. Then the two things that make it real: another system
 * for it to call, and people to give its work to.
 */

import { hasRole, type RoleHolder } from './access';
import { DESIGNER_ROLE, PRIVILEGED_ROLE } from './roles';

export type GettingStartedStepId =
  | 'deploy-process'
  | 'start-instance'
  | 'complete-task'
  | 'connect-system'
  | 'add-people';

export interface GettingStartedFacts {
  /** Some version of some process has been deployed in the project. */
  processDeployed: boolean;
  /** An instance has been started, whatever became of it since. */
  instanceStarted: boolean;
  /** Somebody has completed a task. */
  taskCompleted: boolean;
  /** The project has a connection to another system. */
  connectionSetUp: boolean;
  /** The project has people its processes can assign work to. */
  peopleAdded: boolean;
}

export interface GettingStartedStep {
  id: GettingStartedStepId;
  /** The message key of what it is called (src/i18n/catalogues). */
  labelKey: string;
  /** The message key of what it is for, in one sentence, for somebody who has not done it. */
  descriptionKey: string;
  done: boolean;
}

interface StepDefinition extends Omit<GettingStartedStep, 'done'> {
  /**
   * The roles the server lets do it (server/endpoints/endpoints.go), any one
   * of which will do. Empty means anybody signed in.
   */
  doneBy: readonly string[];
  isDone: (facts: GettingStartedFacts) => boolean;
}

/** CreateDefinition and ImportParticipants are designer() endpoints. */
const DESIGNERS = [PRIVILEGED_ROLE, DESIGNER_ROLE];
/** CreateConnectorInstance is adminOnly(). */
const ADMINISTRATORS = [PRIVILEGED_ROLE];
/** StartProcess and completing a task are protected(): a sign-in is enough. */
const ANYBODY: readonly string[] = [];

const STEPS: StepDefinition[] = [
  {
    id: 'deploy-process',
    doneBy: DESIGNERS,
    labelKey: 'start.deployProcess.label',
    // It links to Processes, where a new process is drawn (see the card's
    // STEP_LINKS). The templates are on the Dashboard only, so it says so.
    descriptionKey: 'start.deployProcess.description',
    isDone: (facts) => facts.processDeployed,
  },
  {
    id: 'start-instance',
    doneBy: ANYBODY,
    labelKey: 'start.startInstance.label',
    descriptionKey: 'start.startInstance.description',
    isDone: (facts) => facts.instanceStarted,
  },
  {
    id: 'complete-task',
    doneBy: ANYBODY,
    labelKey: 'start.completeTask.label',
    descriptionKey: 'start.completeTask.description',
    isDone: (facts) => facts.taskCompleted,
  },
  {
    id: 'connect-system',
    doneBy: ADMINISTRATORS,
    labelKey: 'start.connectSystem.label',
    descriptionKey: 'start.connectSystem.description',
    isDone: (facts) => facts.connectionSetUp,
  },
  {
    id: 'add-people',
    doneBy: DESIGNERS,
    labelKey: 'start.addPeople.label',
    descriptionKey: 'start.addPeople.description',
    isDone: (facts) => facts.peopleAdded,
  },
];

/**
 * The steps this viewer can do, each ticked if it has been done, by anybody.
 *
 * A step the server would refuse them is left out rather than shown with a
 * link to the page where they would be refused. Somebody with no role is left
 * with starting an instance and completing a task, and the card counts and
 * finishes on those. This decides what is shown, never what is allowed.
 */
export function gettingStartedSteps(facts: GettingStartedFacts, viewer: RoleHolder | null | undefined): GettingStartedStep[] {
  return STEPS
    .filter((step) => mayDo(viewer, step))
    .map(({ isDone, doneBy: _doneBy, ...step }) => ({ ...step, done: isDone(facts) }));
}

function mayDo(viewer: RoleHolder | null | undefined, step: StepDefinition): boolean {
  return step.doneBy.length === 0 || step.doneBy.some((role) => hasRole(viewer, role));
}

/**
 * The earliest step not done yet, or undefined when every step is.
 *
 * The earliest, not the one after the last done: steps get done out of order
 * (a connection set up before any process exists), and each depends on the
 * ones before it, since there is no instance to start without a process.
 */
export function nextStep(steps: readonly GettingStartedStep[]): GettingStartedStep | undefined {
  return steps.find((step) => !step.done);
}

/** The parts of a query's result the facts are read from. TanStack Query's result has them. */
export interface QueryLike<T> {
  data: T | undefined;
  /**
   * True while `data` is the previous request's answer, kept on screen while
   * this one loads: after a project switch, another project's rows.
   */
  isPlaceholderData: boolean;
  /** The request threw. */
  isError: boolean;
}

/** A reply that may carry the server's refusal inside it rather than as a failed request. */
interface Reply {
  err?: string;
}

/** The queries the facts are read from, each exactly as its hook returns it. */
export interface GettingStartedQueries {
  /** `useDefinitions()`. */
  definitions: QueryLike<Reply & { definitions: readonly unknown[] }>;
  /** `useInstances()`, with no filter. */
  instances: QueryLike<Reply & { instances: readonly unknown[] }>;
  /**
   * `useProcessStatistics()`: completed tasks, counted across the whole
   * project. They used to be read from the newest 200 tasks, which missed a
   * completed task behind 200 open ones.
   */
  statistics: QueryLike<Reply & { stats?: { completedTasks?: number } }>;
  /** `useConnectorInstances()`. */
  connections: QueryLike<Reply & { instances: readonly unknown[] }>;
  /** `useParticipants()`. */
  people: QueryLike<Reply & { participants: readonly unknown[] }>;
}

/** What is known of the project's progress. */
export type GettingStartedProgress =
  | { state: 'loading' }
  | { state: 'failed' }
  | { state: 'known'; facts: GettingStartedFacts };

const LOADING: GettingStartedProgress = { state: 'loading' };
const FAILED: GettingStartedProgress = { state: 'failed' };

/** One query's answer: its data, or why there is none. */
type Answer<T> = { state: 'loading' } | { state: 'failed' } | { state: 'answered'; data: T };

/**
 * A query's data once it answers what was asked, or why it does not.
 *
 * A placeholder is not an answer: the definitions and instances lists keep the
 * previous rows on screen while the next ones load, and after a project switch
 * those are the last project's. Counted as facts, they ticked "Deploy a
 * process" for a project that had deployed nothing.
 *
 * A failure is not an answer either, and it comes two ways. The connections
 * and people calls throw when the server refuses, which read as loading for
 * good. Definitions, instances and the statistics carry the refusal inside a
 * reply that otherwise reads as an empty list, which read as "not done". A
 * refresh that fails after an answer leaves that answer standing, and it is
 * still true, so it is kept.
 */
function answer<T extends Reply>(query: QueryLike<T>): Answer<T> {
  if (query.isPlaceholderData) return { state: 'loading' };
  const data = query.data;
  if (data !== undefined) return data.err ? { state: 'failed' } : { state: 'answered', data };
  return query.isError ? { state: 'failed' } : { state: 'loading' };
}

/**
 * The facts, or why there are none.
 *
 * All or nothing, because a checklist drawn from half its answers shows steps
 * as not done when they are only not loaded, and ticks them one by one as the
 * requests land. One failure fails it straight away: waiting for the rest
 * cannot make the answer whole.
 */
export function gettingStartedProgress(queries: GettingStartedQueries): GettingStartedProgress {
  const definitions = answer(queries.definitions);
  const instances = answer(queries.instances);
  const statistics = answer(queries.statistics);
  const connections = answer(queries.connections);
  const people = answer(queries.people);
  if ([definitions, instances, statistics, connections, people].some((each) => each.state === 'failed')) return FAILED;
  if (
    definitions.state !== 'answered' ||
    instances.state !== 'answered' ||
    statistics.state !== 'answered' ||
    connections.state !== 'answered' ||
    people.state !== 'answered'
  ) {
    return LOADING;
  }
  return {
    state: 'known',
    facts: {
      processDeployed: definitions.data.definitions.length > 0,
      instanceStarted: instances.data.instances.length > 0,
      taskCompleted: (statistics.data.stats?.completedTasks ?? 0) > 0,
      connectionSetUp: connections.data.instances.length > 0,
      peopleAdded: people.data.participants.length > 0,
    },
  };
}

/**
 * Where hiding the card is remembered: for one person, in one project.
 *
 * It was one key for the whole browser, so on a shared machine, or for
 * somebody working in two projects, hiding it once hid it for everybody and
 * everywhere, including projects nobody had started on. Undefined when either
 * is unknown, since there is then nobody and nowhere to remember it for.
 */
export function dismissalKey(userId: string | undefined, projectId: string | null | undefined): string | undefined {
  if (!userId || !projectId) return undefined;
  return `metis-getting-started-dismissed:${userId}:${projectId}`;
}
