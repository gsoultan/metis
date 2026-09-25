import { asText } from '../types/bpmn';

/**
 * How a service task is carried out, decided the way the engine decides it
 * (entities.Node.Implementation): 'push' calls a web address, 'connector' uses
 * a connection, 'external' waits for a worker, 'script' runs a script.
 *
 * The panel records the modeller's choice as `implementation` and keeps what
 * was typed under every choice, so a topic entered before switching to a web
 * address is still on the node. The recorded choice decides. A file imported
 * from another tool records none, and then the settings do: a topic means a
 * worker, a connection a connector, anything else a web address. The panel
 * used to show "Call a web address" for an imported worker step, which is not
 * the step the engine runs.
 */
export function serviceImplementation(data: Readonly<Record<string, unknown>>): string {
  const chosen = asText(data.implementation);
  if (chosen !== '') return chosen;
  if (storedWorkerTopic(data) !== '') return 'external';
  if (firstText(data.connector_id, data.connector_instance_id, data.connectorInstanceId) !== '') return 'connector';
  return 'push';
}

/** The topic a worker asks under, or '' when the step does not wait for one. */
export function workerTopic(data: Readonly<Record<string, unknown>>): string {
  return serviceImplementation(data) === 'external' ? storedWorkerTopic(data) : '';
}

/** The web address the step calls, or '' when it calls none. */
export function webAddress(data: Readonly<Record<string, unknown>>): string {
  return serviceImplementation(data) === 'push' ? storedWebAddress(data) : '';
}

/**
 * The topic as stored, whatever the step does. Definitions saved from August
 * to September 2026 hold it as `topic`; the engine reads that too.
 */
export function storedWorkerTopic(data: Readonly<Record<string, unknown>>): string {
  return firstText(data.externalTopic, data.external_topic, data.topic);
}

/**
 * The web address as stored, whatever the step does. Definitions saved from
 * August to September 2026 hold it as `url`; the engine reads that too.
 */
export function storedWebAddress(data: Readonly<Record<string, unknown>>): string {
  return firstText(data.httpUrl, data.http_url, data.url);
}

/**
 * The first of several spellings that holds something. Not `??`: a field the
 * modeller cleared is '' rather than missing, and the engine reads past it.
 */
function firstText(...values: unknown[]): string {
  for (const value of values) {
    const text = asText(value);
    if (text !== '') return text;
  }
  return '';
}
