/**
 * The fields a step fills in for a connector that asks for them — a database
 * lookup's query, the values for it, and the variable its answer goes in.
 *
 * Which fields, and which are required, comes from the connector catalogue
 * (node_schema), so none of this knows about databases: it knows about steps
 * that carry a request of their own.
 */

import { editorKeyFor } from '../mappers/definitionMapper';
import type { ApiConnector, ApiConnectorProperty } from '../services/types';

/** The step fields of each connector that has any, by connector id. */
export type StepSchemas = ReadonlyMap<string, readonly ApiConnectorProperty[]>;

/**
 * Every property name a step's own request is stored under. Mirrors
 * server/domains/services/contracts/connector_request_properties.go.
 */
export const STEP_REQUEST_KEYS = ['connector_statement', 'connector_params', 'result_variable'] as const;

const PARAMS_KEY = 'connector_params';
const STATEMENT_KEY = 'connector_statement';

export function stepSchemasOf(connectors: readonly ApiConnector[]): StepSchemas {
  const schemas = new Map<string, readonly ApiConnectorProperty[]>();
  for (const connector of connectors) {
    if (connector.node_schema && connector.node_schema.length > 0) {
      schemas.set(connector.id, connector.node_schema);
    }
  }
  return schemas;
}

/** What a step holds for a field, under its stored name or an editor's alias. */
export function stepFieldValue(data: Record<string, unknown>, key: string): unknown {
  const alias = editorKeyFor(key);
  return data[key] ?? (alias ? data[alias] : undefined);
}

/** The change that sets a field, clearing any alias that could outvote it on save. */
export function stepFieldPatch(key: string, value: unknown): Record<string, unknown> {
  const alias = editorKeyFor(key);
  return alias ? { [key]: value, [alias]: undefined } : { [key]: value };
}

/**
 * The change that clears every step-request field, for when a step changes or
 * drops its connector.
 *
 * A query left behind on a step that now posts to Slack would still make it a
 * database lookup to the server, which would refuse to deploy it for anybody
 * who may not author queries — over a query nobody can see any more.
 */
export function clearedStepFields(): Record<string, undefined> {
  const cleared: Record<string, undefined> = {};
  for (const key of STEP_REQUEST_KEYS) {
    cleared[key] = undefined;
    const alias = editorKeyFor(key);
    if (alias) cleared[alias] = undefined;
  }
  return cleared;
}

function isBlank(value: unknown): boolean {
  if (value === undefined || value === null) return true;
  if (typeof value === 'string') return value.trim() === '';
  if (typeof value === 'object') return Object.keys(value as object).length === 0;
  return false;
}

/** The required fields a step has not filled in. */
export function missingStepFields(
  data: Record<string, unknown>,
  schema: readonly ApiConnectorProperty[],
): ApiConnectorProperty[] {
  return schema.filter((field) => field.required === true && isBlank(stepFieldValue(data, field.key)));
}

/**
 * The :names a query uses, in order, each once.
 *
 * Quoted text is skipped, so ':name' inside a string is not a value, and :: is
 * a PostgreSQL cast, not one. The server reads the query properly; this only
 * has to be right about what the author meant to ask for.
 */
export function queryParameters(statement: string): string[] {
  const unquoted = statement.replace(/'(?:[^']|'')*'|"(?:[^"]|"")*"|`[^`]*`|\[[^\]]*\]/g, ' ');
  const names: string[] = [];
  for (const match of unquoted.matchAll(/(^|[^:]):([A-Za-z_][A-Za-z0-9_]*)/g)) {
    const name = match[2];
    if (!names.includes(name)) names.push(name);
  }
  return names;
}

/** The :names a step's query uses that it gives no value. */
export function unmappedParameters(data: Record<string, unknown>): string[] {
  const statement = stepFieldValue(data, STATEMENT_KEY);
  if (typeof statement !== 'string') return [];
  const params = stepFieldValue(data, PARAMS_KEY);
  const mapped = params && typeof params === 'object' ? (params as Record<string, unknown>) : {};
  return queryParameters(statement).filter((name) => isBlank(mapped[name]));
}
