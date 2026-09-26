import type { ApiDefinition, ApiFlow, ApiNode } from '../services/types';
import { vocabularyFor } from './bpmnVocabulary';
import { isSensitiveKey } from './connectorSecrets';
import { parseFormDefinition } from './formDefinition';
import { humanizeIdentifier } from './wording';

/**
 * What a step is set up to do, read out as labelled settings.
 *
 * The version comparison looked at seven fields — name, type, assignee,
 * candidates, due date and form key — and nothing else. What makes most steps
 * do anything lives elsewhere: a service task's web address and a timer's wait
 * are in the node's settings bag, a script is a field of its own, a gateway's
 * fallback is a flow id. Two versions that called a different address, or
 * waited a day instead of five minutes, compared as identical.
 *
 * So every setting the engine or a connector can act on is read here, in the
 * property panel's words where it has any, and the comparison pairs them up by
 * key. Only what the designer alone reads is left out.
 */

/** One version's steps and paths by id, so a setting can name what it points at. */
export interface VersionIndex {
  nodes: ReadonlyMap<string, ApiNode>;
  flows: ReadonlyMap<string, ApiFlow>;
}

export function indexVersion(definition: ApiDefinition | null): VersionIndex {
  return {
    nodes: new Map((definition?.nodes ?? []).map((node) => [node.id, node])),
    flows: new Map((definition?.flows ?? []).map((flow) => [flow.id, flow])),
  };
}

/** One setting of a step, ready to pair with the same setting in another version. */
export interface StepSetting {
  /** Pairs it across versions. Labels repeat: the designer stores a timer's wait twice. */
  key: string;
  /** What it is called in a sentence. */
  label: string;
  /** What is compared; '' when unset. */
  value: string;
  /**
   * What is shown, or null when the value is code, layout or a credential:
   * those are said to have changed, never printed. A script does not read as
   * a sentence, and a secret must not be put in one.
   */
  shown: string | null;
  /** What an unset value reads as. */
  unset: string;
}

const NONE = '(none)';

/** Longer than this and a value is said to have changed rather than printed. */
const MAX_PRINTED = 120;

/** How a repeating step reads; "none" is the designer's word for not repeating. */
const REPEATS = { parallel: 'all at once', sequential: 'one at a time', none: '' };

/** A step as a sentence names it: by its name, or by its id when it has none. */
export function stepTitle(id: string, version: VersionIndex): string {
  const name = version.nodes.get(id)?.name?.trim();
  return `"${name || id}"`;
}

/** Every setting of one step, fields first and then its settings bag by key. */
export function stepSettings(node: ApiNode, version: VersionIndex): StepSetting[] {
  return [...fieldSettings(node, version), ...propertySettings(node, version)];
}

/**
 * What differs between two readings of the same step, one line per setting.
 *
 * A Set rather than a list because the designer stores some settings twice —
 * a timer's wait is both the node's condition and a property — and a change to
 * one is a change to both. Said once, it reads as the one change it is.
 */
export function settingDifferences(before: StepSetting[], after: StepSetting[]): string[] {
  // Nearly every step reads the same settings in the same order in both
  // versions, and nearly every one is unchanged. Walking the two lists side by
  // side answers that without building anything, which is what keeps a
  // comparison of thousands of steps cheap; the keyed pairing below is only
  // for a step whose settings are not the same list.
  if (sameValues(before, after)) return [];
  const was = new Map(before.map((setting) => [setting.key, setting]));
  const lines = new Set<string>();
  for (const now of after) {
    addDifference(lines, was.get(now.key), now);
    was.delete(now.key);
  }
  for (const gone of was.values()) addDifference(lines, gone, undefined);
  return [...lines];
}

function sameValues(before: StepSetting[], after: StepSetting[]): boolean {
  if (before.length !== after.length) return false;
  return before.every((setting, index) => setting.key === after[index].key && setting.value === after[index].value);
}

function addDifference(lines: Set<string>, was: StepSetting | undefined, now: StepSetting | undefined): void {
  const before = was?.value ?? '';
  const after = now?.value ?? '';
  const setting = now ?? was;
  if (before === after || !setting) return;
  if (was?.shown === null || now?.shown === null) {
    lines.add(`${setting.label} ${before === '' ? 'added' : after === '' ? 'removed' : 'changed'}`);
    return;
  }
  const read = (side: StepSetting | undefined) => (side && side.value !== '' ? side.shown : setting.unset);
  lines.add(`${setting.label}: ${read(was)} → ${read(now)}`);
}

function fieldSettings(node: ApiNode, version: VersionIndex): StepSetting[] {
  return [
    shown('name', 'name', plain(node.name)),
    shown('type', 'kind of step', plain(node.type), vocabularyFor(node.type)?.plainName),
    shown('assignee', 'assignee', plain(node.assignee)),
    shown('candidate_users', 'candidate users', list((node.candidate_users ?? []).map((u) => u.username))),
    shown('candidate_groups', 'candidate groups', list((node.candidate_groups ?? []).map((g) => g.name))),
    timed('due_date', 'due date', plain(node.due_date)),
    shown('priority', 'priority', positive(node.priority)),
    shown('form_key', 'form', plain(node.form_key)),
    hidden('script', 'script', plain(node.script)),
    shown('script_format', 'script language', plain(node.script_format)),
    conditionSetting(node),
    fallbackSetting(node, version),
    shown('external_topic', 'worker topic', plain(node.external_topic)),
    shown('error_code', 'failure code', plain(node.error_code)),
    choice('multi_instance_type', 'runs once per item', node.multi_instance_type, REPEATS, 'no'),
    shown('loop_cardinality', 'times it runs', positive(node.loop_cardinality)),
    shown('collection', 'list it runs over', plain(node.collection)),
    shown('element_variable', 'each item is called', plain(node.element_variable)),
    shown('completion_condition', 'finishes early when', plain(node.completion_condition)),
    stepRef('attached_to_ref', 'attached to', plain(node.attached_to_ref), version),
    flag('cancel_activity', 'stops the step it is attached to', node.cancel_activity),
    stepRef('parent_id', 'inside', plain(node.parent_id), version),
    flag('is_event_sub_process', 'starts when an event happens', node.is_event_sub_process),
  ];
}

/**
 * The node's own `condition` field, which means different things by type: the
 * designer stores a timer's wait in it, and a script task's code as well.
 * Labelled for what it is, so the copy it mirrors reads the same and is said
 * once.
 */
function conditionSetting(node: ApiNode): StepSetting {
  const value = plain(node.condition);
  const type = node.type ?? '';
  if (type === 'scriptTask') return hidden('condition', 'script', value);
  if (type.endsWith('Event')) return timed('condition', 'wait', value);
  return shown('condition', 'condition', value);
}

/**
 * A gateway's fallback, as the step it leads to. The flow id is the designer's
 * business: an arrow drawn again gets a new one and still goes to the same place.
 */
function fallbackSetting(node: ApiNode, version: VersionIndex): StepSetting {
  const flowId = plain(node.default_flow);
  const flow = version.flows.get(flowId);
  if (flowId === '' || !flow) return shown('default_flow', 'fall back to', flowId);
  return shown('default_flow', 'fall back to', flow.target_ref, stepTitle(flow.target_ref, version));
}

/**
 * What the designer alone reads: test data, and which editor a panel shows.
 * `implementation` and `timer_type` are the second kind — the engine goes by
 * the topic, connector or address that is set, and by the wait as written.
 */
const DESIGNER_ONLY = new Set([
  'sampleData', 'startedBy', 'assignmentMode', 'implementation', 'timer_type',
  'label', 'nodeType', 'status', 'heatmapValue', 'documentation', 'width', 'height', 'isExpanded',
]);

type Read = (key: string, raw: unknown, version: VersionIndex) => StepSetting;

const said = (label: string): Read => (key, raw) => shown(key, label, plain(raw));
const secret = (label: string): Read => (key, raw) => hidden(key, label, plain(raw));
const waited = (label: string): Read => (key, raw) => timed(key, label, plain(raw));
const flagged = (label: string): Read => (key, raw) => flag(key, label, raw);
const chosen = (label: string, words: Record<string, string>): Read => (key, raw) => choice(key, label, raw, words);
const pointed = (label: string): Read => (key, raw, version) => stepRef(key, label, plain(raw), version);

/**
 * The settings bag, in the property panel's words. A key not listed here is
 * still compared, under its own name made readable — an unknown setting that
 * changed is worth a line, and hiding it is how two versions that behave
 * differently come to read as the same.
 */
const PROPERTY_WORDS: Record<string, Read> = {
  http_url: said('web address'),
  url: said('web address'),
  http_method: said('request method'),
  headers: secret('request headers'),
  auth_type: said('sign-in method'),
  auth_username: secret('credentials'),
  auth_password: secret('credentials'),
  auth_token: secret('credentials'),
  auth_api_key: secret('credentials'),
  auth_header_name: secret('credentials'),
  connector_id: said('connector'),
  connector_instance_id: said('connection'),
  connector_statement: secret('query'),
  topic: said('worker topic'),
  lock_duration: waited('how long a worker may hold it'),
  result_variable: said('store the answer as'),
  input_mapping: secret('data passed in'),
  in_mapping: secret('data passed in'),
  output_mapping: secret('data passed back'),
  out_mapping: secret('data passed back'),
  script: secret('script'),
  decision_key: said('decision table'),
  decision_version: said('decision table version'),
  assignment_decision_key: said('approver chosen by decision table'),
  called_process_key: said('process it runs'),
  called_element: said('process it runs'),
  calledElement: said('process it runs'),
  called_process_version: said('version of the process it runs'),
  called_element_version: said('version of the process it runs'),
  calledElementVersion: said('version of the process it runs'),
  event_type: chosen('waits for', {
    timer: 'time', message: 'a message', signal: 'a signal', conditional: 'something to become true',
    error: 'a failure', escalation: 'something to raise', compensation: 'undoing the work',
  }),
  timer_duration: waited('wait'),
  signal_name: said('signal name'),
  message_name: said('message name'),
  correlation_key: said('message belonging to'),
  condition_expression: said('carry on when'),
  escalation_code: said('reason code'),
  error_code: said('failure code'),
  non_interrupting: flagged('lets the step carry on'),
  compensation: flagged('undoes earlier steps'),
  activity_ref: pointed('step it undoes'),
  multi_instance_completion_condition: said('finishes early when'),
  compliance_relevant: flagged('carries a control obligation'),
  compliance_note: said('control note'),
  separation_of_duties: (key, raw, version) => stepList(key, 'not by whoever did', plain(raw), version),
  actor: said('done by'),
};

function propertySettings(node: ApiNode, version: VersionIndex): StepSetting[] {
  const properties = node.properties ?? {};
  // Sorted: the bag arrives in whatever order the server's map produced, and
  // the same difference should read the same way twice.
  return Object.keys(properties).sort().filter((key) => !DESIGNER_ONLY.has(key)).flatMap((key) => {
    const raw = properties[key];
    if (key === 'form_definition') return formFieldSettings(raw);
    const read = PROPERTY_WORDS[key];
    return [read ? read(`prop:${key}`, raw, version) : unknownSetting(key, raw)];
  });
}

function unknownSetting(key: string, raw: unknown): StepSetting {
  const label = humanizeIdentifier(key).toLowerCase();
  const value = plain(raw);
  const printable = !isSensitiveKey(key) && (typeof raw !== 'object' || raw === null) && value.length <= MAX_PRINTED;
  return printable ? shown(`prop:${key}`, label, value) : hidden(`prop:${key}`, label, value);
}

/**
 * A form, one setting per field. "The form changed" says nothing a process
 * owner can use; a field that is gone is a value nobody is asked for any more,
 * and the gateways after it may still read it.
 */
function formFieldSettings(raw: unknown): StepSetting[] {
  const { fields, error } = parseFormDefinition<{ id?: string; label?: string }>(
    typeof raw === 'string' ? raw : JSON.stringify(raw ?? null),
  );
  if (error !== null) return [hidden('prop:form_definition', 'form', plain(raw))];
  return fields.map((field, index) => {
    const id = plain(field.id) || plain(field.label) || String(index);
    return hidden(`form:${id}`, `form field "${plain(field.label) || id}"`, stableJson(field));
  });
}

function shown(key: string, label: string, value: string, display?: string): StepSetting {
  return { key, label, value, shown: display ?? value, unset: NONE };
}

function hidden(key: string, label: string, value: string): StepSetting {
  return { key, label, value, shown: null, unset: NONE };
}

function timed(key: string, label: string, value: string): StepSetting {
  return shown(key, label, value, describeTimer(value));
}

function flag(key: string, label: string, raw: unknown): StepSetting {
  const on = raw === true || raw === 'true';
  return { key, label, value: on ? 'yes' : '', shown: on ? 'yes' : '', unset: 'no' };
}

function choice(key: string, label: string, raw: unknown, words: Record<string, string>, unset = NONE): StepSetting {
  const value = plain(raw);
  const reading = words[value] ?? value;
  // A choice whose words are empty is the "none" option: unset, not a value.
  return { key, label, value: reading === '' ? '' : value, shown: reading, unset };
}

function stepRef(key: string, label: string, id: string, version: VersionIndex): StepSetting {
  return shown(key, label, id, id === '' ? '' : stepTitle(id, version));
}

function stepList(key: string, label: string, ids: string, version: VersionIndex): StepSetting {
  const names = ids.split(',').map((id) => id.trim()).filter((id) => id !== '').sort();
  return shown(key, label, names.join(','), names.map((id) => stepTitle(id, version)).join(', '));
}

/** A value as compared: trimmed, with unset, empty and false all reading as ''. */
function plain(value: unknown): string {
  if (value === undefined || value === null || value === false) return '';
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'object') return stableJson(value);
  return String(value);
}

function positive(value: number | undefined): string {
  return value !== undefined && value > 0 ? String(value) : '';
}

function list(values: string[]): string {
  return [...values].sort().join(', ');
}

/** JSON with object keys sorted, so the same value always compares equal. */
function stableJson(value: unknown): string {
  return JSON.stringify(value, (_key, inner: unknown) =>
    inner && typeof inner === 'object' && !Array.isArray(inner)
      ? Object.fromEntries(Object.entries(inner as Record<string, unknown>).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)))
      : inner,
  );
}

const ISO_DURATION = /^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$/;
const REPEATING = /^R(\d*)\/(P\S+)$/;
const UNITS = ['year', 'month', 'week', 'day', 'hour', 'minute', 'second'];

/**
 * An ISO-8601 wait as somebody would say it: "PT5M" is "5 minutes", "R3/PT1H"
 * is "every 1 hour, 3 times". Anything else — a date, an expression — is shown
 * as written, because guessing at it would be worse than quoting it.
 */
export function describeTimer(expression: string): string {
  const repeating = REPEATING.exec(expression);
  if (repeating) {
    const every = describeDuration(repeating[2]);
    if (every === null) return expression;
    const times = repeating[1];
    if (times === '') return `every ${every}, with no end`;
    return `every ${every}, ${times} ${times === '1' ? 'time' : 'times'}`;
  }
  return describeDuration(expression) ?? expression;
}

function describeDuration(expression: string): string | null {
  const match = ISO_DURATION.exec(expression);
  if (!match || expression.endsWith('T')) return null;
  const parts = match.slice(1).flatMap((amount, index) =>
    amount === undefined || Number(amount) === 0 ? [] : [`${amount} ${UNITS[index]}${Number(amount) === 1 ? '' : 's'}`],
  );
  return parts.length > 0 ? parts.join(', ') : null;
}
