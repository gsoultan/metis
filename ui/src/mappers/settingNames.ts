/**
 * The names one setting of a step goes by, and which of them a step holds.
 *
 * The server stores a setting under one name, http_url say. The property panel
 * holds some under a camelCase name of its own, httpUrl, and older editors
 * wrote a few under names nothing writes now. Saving sends each under the
 * stored name.
 *
 * A step holds each setting once. Opening a step from the server used to put
 * it on the node up to three times: under the stored name, under the panel's
 * name, and in a nested copy of everything the server sent, which saving
 * started from. So a setting deleted or renamed in the raw editor was saved
 * all the same, and an edit made under one name could be outvoted by a stale
 * copy under another.
 */

/** A panel field → the name the server stores it under, where the two differ. */
const EDITOR_NAMES: Readonly<Record<string, string>> = {
  lockDuration: 'lock_duration',
  httpUrl: 'http_url',
  httpMethod: 'http_method',
  resultVariable: 'result_variable',
  eventType: 'event_type',
  timerType: 'timer_type',
  duration: 'timer_duration',
  signalName: 'signal_name',
  messageName: 'message_name',
  correlationKey: 'correlation_key',
  conditionExpression: 'condition_expression',
  escalationCode: 'escalation_code',
  activityRef: 'activity_ref',
  nonInterrupting: 'non_interrupting',
  formDefinition: 'form_definition',
};

/**
 * Names older editors wrote a setting under → the stored name. The panel now
 * writes these under the stored name itself, so a step saved by an older
 * designer is still read, and opening a step never produces them.
 */
const RETIRED_NAMES: Readonly<Record<string, string>> = {
  connectorInstanceId: 'connector_instance_id',
  inputMapping: 'input_mapping',
  outputMapping: 'output_mapping',
  decisionKey: 'decision_key',
  decisionVersion: 'decision_version',
  calledProcessKey: 'called_process_key',
  calledProcessVersion: 'called_process_version',
};

const PANEL_NAMES: ReadonlyMap<string, string> = new Map(
  Object.entries(EDITOR_NAMES).map(([panelName, storedName]) => [storedName, panelName]),
);

/**
 * The nested copy of the server's settings that steps used to carry. A draft
 * kept in the browser before the change can still hold one.
 */
const RETIRED_COPY = 'properties';

/** The name the server stores a setting under. */
export function storedName(key: string): string {
  return EDITOR_NAMES[key] ?? RETIRED_NAMES[key] ?? key;
}

/** The name a step holds a setting under: the one the property panel reads. */
export function heldName(key: string): string {
  const stored = storedName(key);
  return PANEL_NAMES.get(stored) ?? stored;
}

/**
 * A name other than the stored one that an editor may have left a setting
 * under, so a writer can clear it as it sets the setting.
 */
export function editorKeyFor(key: string): string | undefined {
  const stored = storedName(key);
  const other = [...Object.entries(EDITOR_NAMES), ...Object.entries(RETIRED_NAMES)]
    .find(([name, storedAs]) => storedAs === stored && name !== key);
  return other?.[0];
}

/**
 * The settings with each held once, under the name the panel reads.
 *
 * Where a setting is there under more than one name, the one the panel reads
 * wins: it is the one the person looking at the step saw and edited.
 */
export function heldOnce(settings: Readonly<Record<string, unknown>>): Record<string, unknown> {
  const held: Record<string, unknown> = {};
  const entries = Object.entries(settings).filter(([key]) => key !== RETIRED_COPY);
  for (const [key, value] of entries) {
    if (heldName(key) === key) held[key] = value;
  }
  for (const [key, value] of entries) {
    const name = heldName(key);
    if (name !== key && !Object.hasOwn(held, name)) held[name] = value;
  }
  return held;
}
