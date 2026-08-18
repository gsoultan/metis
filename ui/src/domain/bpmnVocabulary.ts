/**
 * Plain-language vocabulary for BPMN.
 *
 * BPMN names describe the *notation*, not the intent. "Exclusive Gateway",
 * "Intermediate Catch Event" and "Call Activity" are precise to someone who has
 * read the spec and opaque to everyone else — which is most of the people who
 * need to read a process diagram: the manager approving it, the analyst who
 * owns the policy, the operator working out why an instance is stuck.
 *
 * Each entry carries four things:
 *
 *   plainName   what it does, as a verb phrase someone can act on
 *   bpmnName    the spec term, shown in expert mode and in tooltips, because
 *               the vocabulary has to be learnable — hiding it forever would
 *               leave people unable to talk to the wider BPMN world
 *   whatItDoes  one sentence, in business terms, no notation words
 *   example     a concrete instance of it, which is what actually makes an
 *               abstract construct click
 *
 * The rule for writing these: if the sentence needs a BPMN word to make sense,
 * it is not finished.
 */

export type NodeKind =
  | 'startEvent'
  | 'endEvent'
  | 'terminateEndEvent'
  | 'errorEndEvent'
  | 'intermediateCatchEvent'
  | 'intermediateThrowEvent'
  | 'boundaryEvent'
  | 'userTask'
  | 'serviceTask'
  | 'scriptTask'
  | 'manualTask'
  | 'businessRuleTask'
  | 'decideGroup'
  | 'callActivity'
  | 'exclusiveGateway'
  | 'parallelGateway'
  | 'inclusiveGateway'
  | 'eventBasedGateway'
  | 'subProcess'
  | 'pool'
  | 'lane';

export interface NodeVocabulary {
  plainName: string;
  bpmnName: string;
  whatItDoes: string;
  example: string;
  /** Business-oriented grouping for the palette. */
  group: 'Start and finish' | 'Steps' | 'Decisions and branching' | 'Waiting' | 'Structure';
}

export const NODE_VOCABULARY: Record<NodeKind, NodeVocabulary> = {
  startEvent: {
    plainName: 'Start',
    bpmnName: 'Start Event',
    whatItDoes: 'Where the process begins.',
    example: 'Someone submits an expense claim.',
    group: 'Start and finish',
  },
  endEvent: {
    plainName: 'Finish',
    bpmnName: 'End Event',
    whatItDoes: 'Marks this path as finished. Other paths keep running.',
    example: 'The claim has been paid.',
    group: 'Start and finish',
  },
  terminateEndEvent: {
    plainName: 'Stop everything',
    bpmnName: 'Terminate End Event',
    whatItDoes: 'Ends the whole process immediately, including any other paths still running.',
    example: 'The customer cancels, so nothing else should happen.',
    group: 'Start and finish',
  },
  errorEndEvent: {
    plainName: 'Finish with a problem',
    bpmnName: 'Error End Event',
    whatItDoes: 'Ends this path and reports a named problem the surrounding process can react to.',
    example: 'The payment was declined.',
    group: 'Start and finish',
  },

  userTask: {
    plainName: 'Ask a person',
    bpmnName: 'User Task',
    whatItDoes: 'Puts a task in someone’s inbox and waits until they complete it.',
    example: 'A manager reviews and approves the claim.',
    group: 'Steps',
  },
  serviceTask: {
    plainName: 'Call another system',
    bpmnName: 'Service Task',
    whatItDoes: 'Sends or fetches information from another system automatically.',
    example: 'Create the invoice in the accounting system.',
    group: 'Steps',
  },
  decideGroup: {
    plainName: 'Decide, then take the right path',
    bpmnName: 'Business Rule Task + Exclusive Gateway',
    whatItDoes:
      'Looks up the answer in a decision table and branches on it. This is the recommended shape: the policy lives in a table somebody can version and test, and each path just compares against what it returned.',
    example: 'Decide the approval level, then send it to whoever that level names.',
    group: 'Decisions and branching',
  },
  businessRuleTask: {
    plainName: 'Apply a business rule',
    bpmnName: 'Business Rule Task',
    whatItDoes: 'Looks up an answer in a decision table, so the policy can change without changing the process.',
    example: 'Decide the approval level from the claim amount.',
    group: 'Steps',
  },
  scriptTask: {
    plainName: 'Work something out',
    bpmnName: 'Script Task',
    whatItDoes: 'Calculates or reshapes information without involving a person or another system.',
    example: 'Add up the line items to get a total.',
    group: 'Steps',
  },
  manualTask: {
    plainName: 'Something happens offline',
    bpmnName: 'Manual Task',
    whatItDoes: 'Records a step that happens outside any system. Nothing is tracked until someone says it is done.',
    example: 'File the signed paperwork.',
    group: 'Steps',
  },
  callActivity: {
    plainName: 'Run another process',
    bpmnName: 'Call Activity',
    whatItDoes: 'Runs a separate process and waits for it to finish before carrying on.',
    example: 'Run the standard onboarding process for the new supplier.',
    group: 'Steps',
  },

  exclusiveGateway: {
    plainName: 'Choose one path',
    bpmnName: 'Exclusive Gateway',
    whatItDoes: 'Sends the process down exactly one path, based on a condition you set.',
    example: 'Over £1,000 goes to the finance director; anything less is auto-approved.',
    group: 'Decisions and branching',
  },
  parallelGateway: {
    plainName: 'Do several things at once',
    bpmnName: 'Parallel Gateway',
    whatItDoes: 'Starts every path at the same time, and can later wait for all of them to finish.',
    example: 'Run the credit check and the identity check together.',
    group: 'Decisions and branching',
  },
  inclusiveGateway: {
    plainName: 'Choose one or more paths',
    bpmnName: 'Inclusive Gateway',
    whatItDoes: 'Takes every path whose condition is true — one, several, or all of them.',
    example: 'Notify the manager, and also legal if the contract is involved.',
    group: 'Decisions and branching',
  },
  eventBasedGateway: {
    plainName: 'Whichever happens first',
    bpmnName: 'Event-Based Gateway',
    whatItDoes: 'Waits for several possible things and continues down whichever one happens first.',
    example: 'The customer replies, or two days pass — whichever comes first.',
    group: 'Decisions and branching',
  },

  intermediateCatchEvent: {
    plainName: 'Wait for something',
    bpmnName: 'Intermediate Catch Event',
    whatItDoes: 'Pauses here until a set time passes or a message arrives.',
    example: 'Wait three days for the customer to respond.',
    group: 'Waiting',
  },
  intermediateThrowEvent: {
    plainName: 'Announce something',
    bpmnName: 'Intermediate Throw Event',
    whatItDoes: 'Sends a message or signal that other processes can be waiting for.',
    example: 'Tell the shipping process that payment has cleared.',
    group: 'Waiting',
  },
  boundaryEvent: {
    plainName: 'If this goes wrong',
    bpmnName: 'Boundary Event',
    whatItDoes: 'Attaches to a step and provides a way out if it fails or takes too long.',
    example: 'If nobody approves within five days, escalate it.',
    group: 'Waiting',
  },

  subProcess: {
    plainName: 'Group of steps',
    bpmnName: 'Sub-Process',
    whatItDoes: 'Collapses several steps into one box, to keep a large diagram readable.',
    example: 'All the checks needed before approval, shown as one step.',
    group: 'Structure',
  },
  pool: {
    plainName: 'Who is involved',
    bpmnName: 'Pool',
    whatItDoes: 'Represents an organization or participant in the process.',
    example: 'Your company, and separately the supplier.',
    group: 'Structure',
  },
  lane: {
    plainName: 'Team or role',
    bpmnName: 'Lane',
    whatItDoes: 'Divides a participant into the teams or roles that do each step.',
    example: 'Finance, Legal, and Procurement within your company.',
    group: 'Structure',
  },
};

/** Vocabulary for a node kind, or undefined for an unknown one. */
export function vocabularyFor(kind: string): NodeVocabulary | undefined {
  return NODE_VOCABULARY[kind as NodeKind];
}

/**
 * The label to show for a node kind.
 *
 * Expert mode swaps to the BPMN term for people who think in the notation and
 * need to match a diagram against the specification.
 */
export function nodeLabel(kind: string, expertMode: boolean): string {
  const entry = vocabularyFor(kind);
  if (!entry) return kind;
  return expertMode ? entry.bpmnName : entry.plainName;
}

/** The secondary line under a node label: the other name, for learnability. */
export function nodeSubLabel(kind: string, expertMode: boolean): string | undefined {
  const entry = vocabularyFor(kind);
  if (!entry) return undefined;
  return expertMode ? entry.plainName : entry.bpmnName;
}
