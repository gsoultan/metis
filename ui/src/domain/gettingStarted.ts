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

export type GettingStartedStepId =
  | 'deploy-process'
  | 'start-instance'
  | 'complete-task'
  | 'connect-system'
  | 'add-people';

/** Where a step is done. Router paths, so a renamed route fails the typecheck where they are linked. */
export type GettingStartedPath = '/models' | '/inbox' | '/connectors' | '/people';

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
  label: string;
  /** What the step is for, in one sentence, for somebody who has not done it. */
  description: string;
  done: boolean;
  to: GettingStartedPath;
}

interface StepDefinition extends Omit<GettingStartedStep, 'done'> {
  isDone: (facts: GettingStartedFacts) => boolean;
}

const STEPS: StepDefinition[] = [
  {
    id: 'deploy-process',
    label: 'Deploy a process',
    description: 'Draw one in the designer, or start from a template, then deploy it so it can run.',
    to: '/models',
    isDone: (facts) => facts.processDeployed,
  },
  {
    id: 'start-instance',
    label: 'Start an instance',
    description: 'Run your process once. Each run is an instance you can follow step by step.',
    to: '/models',
    isDone: (facts) => facts.instanceStarted,
  },
  {
    id: 'complete-task',
    label: 'Complete a task',
    description: 'When a process needs a person, the task waits in the inbox until somebody completes it.',
    to: '/inbox',
    isDone: (facts) => facts.taskCompleted,
  },
  {
    id: 'connect-system',
    label: 'Connect another system',
    description: 'Set up a connection, such as email or Slack, so your steps can call it.',
    to: '/connectors',
    isDone: (facts) => facts.connectionSetUp,
  },
  {
    id: 'add-people',
    label: 'Add the people who do the work',
    description: 'Import the people your processes can assign tasks to.',
    to: '/people',
    isDone: (facts) => facts.peopleAdded,
  },
];

export function gettingStartedSteps(facts: GettingStartedFacts): GettingStartedStep[] {
  return STEPS.map(({ isDone, ...step }) => ({ ...step, done: isDone(facts) }));
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

/**
 * The lists the facts are read from, each exactly as its hook returns it.
 *
 * Undefined means not loaded yet, which is different from an empty list.
 */
export interface GettingStartedSources {
  /** `useDefinitions()` → `data.definitions`. */
  definitions?: readonly unknown[];
  /** `useInstances()` → `data.instances`, with no filter. */
  instances?: readonly unknown[];
  /**
   * `useTasks(1, 200)` → `data.tasks`: the newest tasks, in every state. A
   * project whose only completed tasks are older than that page is missed. A
   * project with that much open work is well past getting started.
   */
  tasks?: readonly { status?: string }[];
  /** `useConnectorInstances()` → `data.instances`. */
  connections?: readonly unknown[];
  /** `useParticipants()` → `data.participants`. */
  people?: readonly unknown[];
}

/** A task's status once somebody has completed it, as the server writes it. */
const COMPLETED_TASK_STATUS = 'completed';

/**
 * The facts, or undefined while any list is still on its way.
 *
 * All or nothing, because a checklist drawn from half its answers shows steps
 * as not done when they are only not loaded, and ticks them one by one as the
 * requests land.
 */
export function gettingStartedFacts(sources: GettingStartedSources): GettingStartedFacts | undefined {
  const { definitions, instances, tasks, connections, people } = sources;
  if (!definitions || !instances || !tasks || !connections || !people) return undefined;
  return {
    processDeployed: definitions.length > 0,
    instanceStarted: instances.length > 0,
    taskCompleted: tasks.some((task) => task.status === COMPLETED_TASK_STATUS),
    connectionSetUp: connections.length > 0,
    peopleAdded: people.length > 0,
  };
}
