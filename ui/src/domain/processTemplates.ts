/**
 * Processes somebody can start from instead of an empty canvas.
 *
 * The designer opened on a blank grid, and `roadmap.md` §7 has had "template
 * gallery" as a high-priority gap for as long as it has existed. The dashboard
 * advertised one — three cards badged "Recommended" whose buttons were all
 * disabled — which was removed rather than built, because a control that
 * cannot act is worse than an absent one.
 *
 * This is the building. A template is a starting diagram: nodes and edges the
 * designer loads so the first thing an author sees is a process that already
 * makes sense, which they then edit. It is not a wizard and does not generate
 * anything — the shapes are written out here, so what lands is exactly what
 * somebody reviewed.
 *
 * What a template deliberately does NOT do
 * ----------------------------------------
 * It leaves every *decision* to the author. Gateway conditions are empty, not
 * guessed; a business rule task names no table. A template that arrives
 * looking configured, and is not, is the failure mode the decide-group builder
 * already avoids for the same reason — a condition that looks set but is empty
 * routes nothing, and the author has no reason to look at it.
 *
 * Nor does it point a step at another system. A step that calls one names a
 * topic for the author's own worker instead, so nothing is called until
 * something the author wrote asks for the work; see the invoice template.
 *
 * Layout is on a 180px grid so the shapes do not overlap on arrival. The
 * designer's auto-layout can rearrange them afterwards.
 */

import type { BuiltEdge, BuiltNode } from './decideGroup';

export interface BuiltTemplate {
  nodes: BuiltNode[];
  edges: BuiltEdge[];
  /** The step the author most likely has to configure first. */
  focusId: string;
}

export interface ProcessTemplate {
  id: string;
  /** What it is called in the gallery. */
  name: string;
  /** What it does, in the words somebody choosing would use. */
  description: string;
  /** The default process key, which the author can change. */
  suggestedKey: string;
  build: (makeId: () => string) => BuiltTemplate;
}

const COL = 200;
const ROW = 140;

/**
 * A node carries its kind twice: `type` is what React Flow renders with, and
 * `data.nodeType` is what the property panel, the vocabulary and the BPMN
 * export read. Setting only the first is invisible on the canvas until you
 * notice every task calling itself "Call another system" — the fallback — so
 * the two are set together here rather than at each call site.
 */
function node(id: string, type: string, label: string, x: number, y: number, extra: Record<string, unknown> = {}): BuiltNode {
  return { id, type, position: { x, y }, data: { label, nodeType: type, documentation: '', ...extra } };
}

function edge(id: string, source: string, target: string, label?: string): BuiltEdge {
  return label === undefined ? { id, source, target } : { id, source, target, label };
}

/** The settings of a step a worker picks up by topic, as the property panel writes them. */
function workerStep(topic: string): Record<string, unknown> {
  return { implementation: 'external', externalTopic: topic };
}

/**
 * Two-step approval with a decision point.
 *
 * The smallest process that is still a process: something arrives, a person
 * decides, and the two answers go different ways. Most first processes are a
 * variation on it.
 */
const simpleApproval: ProcessTemplate = {
  id: 'simple-approval',
  name: 'Simple approval',
  description: 'Someone reviews a request and it is either approved or rejected.',
  suggestedKey: 'simple_approval',
  build: (makeId) => {
    const start = makeId();
    const review = makeId();
    const decide = makeId();
    const approved = makeId();
    const rejected = makeId();

    return {
      nodes: [
        node(start, 'startEvent', 'Request received', 0, ROW),
        node(review, 'userTask', 'Review the request', COL, ROW),
        node(decide, 'exclusiveGateway', 'Approved?', COL * 2, ROW),
        node(approved, 'endEvent', 'Approved', COL * 3, ROW - ROW / 2),
        node(rejected, 'endEvent', 'Rejected', COL * 3, ROW + ROW / 2),
      ],
      edges: [
        edge(makeId(), start, review),
        edge(makeId(), review, decide),
        // Labelled, not conditioned: the author decides what "approved" means
        // in their own variables, and a condition that looks set but is empty
        // routes nothing.
        edge(makeId(), decide, approved, 'Approved'),
        edge(makeId(), decide, rejected, 'Rejected'),
      ],
      focusId: decide,
    };
  },
};

/**
 * Triage, then two paths of different urgency.
 *
 * The shape behind most support and incident processes: classify first, then
 * treat differently.
 */
const supportTicket: ProcessTemplate = {
  id: 'support-ticket',
  name: 'Support ticket',
  description: 'A ticket is triaged, then either escalated or handled normally.',
  suggestedKey: 'support_ticket',
  build: (makeId) => {
    const start = makeId();
    const triage = makeId();
    const urgent = makeId();
    const escalate = makeId();
    const handle = makeId();
    const closed = makeId();

    return {
      nodes: [
        node(start, 'startEvent', 'Ticket raised', 0, ROW),
        node(triage, 'userTask', 'Triage the ticket', COL, ROW),
        node(urgent, 'exclusiveGateway', 'Urgent?', COL * 2, ROW),
        node(escalate, 'userTask', 'Escalate to a specialist', COL * 3, ROW - ROW),
        node(handle, 'userTask', 'Handle the ticket', COL * 3, ROW + ROW),
        node(closed, 'endEvent', 'Ticket closed', COL * 4, ROW),
      ],
      edges: [
        edge(makeId(), start, triage),
        edge(makeId(), triage, urgent),
        edge(makeId(), urgent, escalate, 'Urgent'),
        edge(makeId(), urgent, handle, 'Normal'),
        // Straight to the end from both branches. A joining gateway with one
        // way out decides nothing, and the designer's validator says so.
        edge(makeId(), escalate, closed),
        edge(makeId(), handle, closed),
      ],
      focusId: urgent,
    };
  },
};

/**
 * A process that calls out to something, then asks a person.
 *
 * Included because the service task is the step most first-time authors have
 * not met, and meeting it inside a working process is easier than meeting it
 * on an empty canvas.
 */
const invoiceProcessing: ProcessTemplate = {
  id: 'invoice-processing',
  name: 'Invoice processing',
  description: 'An invoice is checked, approved by a person, then paid. The checking and the paying wait for a worker, a program of yours that picks up the work.',
  suggestedKey: 'invoice_processing',
  build: (makeId) => {
    const start = makeId();
    const verify = makeId();
    const approve = makeId();
    const pay = makeId();
    const done = makeId();

    return {
      nodes: [
        node(start, 'startEvent', 'Invoice received', 0, ROW),
        /*
         * Handed to a worker by topic rather than pointed at a web address. A
         * template that arrives pointing somewhere calls somewhere by accident.
         * Leaving the step empty was worse: with nothing to call it finishes at
         * once, so every invoice went through unchecked and unpaid while the
         * instance reported success. With a topic the instance waits at the
         * step, where anyone can see it, until a worker the author writes asks
         * for the work. The SDK sandbox can play that worker.
         */
        node(verify, 'serviceTask', 'Check the invoice', COL, ROW, workerStep('check-invoice')),
        node(approve, 'userTask', 'Approve for payment', COL * 2, ROW),
        node(pay, 'serviceTask', 'Send for payment', COL * 3, ROW, workerStep('pay-invoice')),
        node(done, 'endEvent', 'Invoice paid', COL * 4, ROW),
      ],
      edges: [
        edge(makeId(), start, verify),
        edge(makeId(), verify, approve),
        edge(makeId(), approve, pay),
        edge(makeId(), pay, done),
      ],
      focusId: verify,
    };
  },
};

export const PROCESS_TEMPLATES: ProcessTemplate[] = [
  simpleApproval,
  supportTicket,
  invoiceProcessing,
];

/** The template with this id, or null. An unknown id opens a blank canvas. */
export function templateById(id: string | undefined | null): ProcessTemplate | null {
  if (!id) return null;
  return PROCESS_TEMPLATES.find((t) => t.id === id) ?? null;
}
