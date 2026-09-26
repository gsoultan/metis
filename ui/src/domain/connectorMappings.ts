/**
 * Carrying a connector step saved by an older designer over to the mappings the
 * engine reads. Kept apart from connectorStep.ts because the definition loader
 * needs it, and connectorStep.ts needs the loader's alias table — one module
 * each way would be an import cycle.
 */

const LEGACY_INPUTS = 'inputs';
const LEGACY_OUTPUTS = 'outputs';

function isMap(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/** Each source → target pair turned into target → source. */
function turnedRound(pairs: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [source, target] of Object.entries(pairs)) {
    if (typeof target === 'string' && target.trim() !== '') out[target] = source;
  }
  return out;
}

/**
 * A connector step saved by an older designer, with its mappings moved to where
 * the engine reads them.
 *
 * The SENDING and RECEIVING tables used to write `inputs` and `outputs`, which
 * nothing on the server ever read: a step configured to rename a value did not.
 * The engine reads `input_mapping` and `output_mapping` — the same
 * target → source maps a decision uses — so an older step is carried over when
 * it is opened, and takes effect when it is next deployed. A definition already
 * deployed runs exactly as it did until then.
 *
 * The old tables were written the other way round (your variable → their
 * field; their field → store as), so each pair is turned round. A step that
 * already has the new maps keeps them, and the old keys are dropped either way.
 */
export function migrateConnectorMappings(properties: Record<string, unknown>): Record<string, unknown> {
  const inputs = properties[LEGACY_INPUTS];
  const outputs = properties[LEGACY_OUTPUTS];
  if (!isMap(inputs) && !isMap(outputs)) return properties;

  const migrated = { ...properties };
  delete migrated[LEGACY_INPUTS];
  delete migrated[LEGACY_OUTPUTS];
  if (isMap(inputs) && !isMap(properties.input_mapping)) migrated.input_mapping = turnedRound(inputs);
  if (isMap(outputs) && !isMap(properties.output_mapping)) migrated.output_mapping = turnedRound(outputs);
  return migrated;
}
