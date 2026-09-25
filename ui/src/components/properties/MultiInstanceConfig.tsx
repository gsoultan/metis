import { Checkbox, NumberInput, Select, Stack, TextInput } from '@mantine/core';

import { asNumber, asText } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';

const RUNS_ONCE = new Set(['', 'none']);

const KNOWN_EXECUTIONS = [
  { value: 'parallel', label: 'Parallel: all at the same time' },
  { value: 'sequential', label: 'Sequential: one after another' },
];

/**
 * The ways a loop can run, with the step's own among them.
 *
 * A step from an imported file can name a kind this editor does not offer.
 * It is listed under its own name, so the select shows what the step holds
 * rather than an empty box.
 */
function executionOptions(current: string): Array<{ value: string; label: string }> {
  if (KNOWN_EXECUTIONS.some((option) => option.value === current)) return KNOWN_EXECUTIONS;
  return [...KNOWN_EXECUTIONS, { value: current, label: `${current} (not recognised)` }];
}

/**
 * "Do this once for each item in a list", or a set number of times.
 *
 * The settings are flat on a node — multiInstanceType, loopCardinality,
 * collection, elementVariable, completionCondition — which is how the domain,
 * the mapper and the engine all name them. This editor used to keep them
 * nested under a `loopCharacteristics` object of its own invention, with a
 * boolean `isSequential` in place of the type, so nothing it wrote was ever
 * read: a task set to run once per item ran exactly once.
 *
 * Any type but an empty one and "none" counts as a loop here, as it does to
 * the engine. The editor used to count only the two it offers, so a step
 * set to repeat some other way showed an unticked box, as if it ran once.
 */
export function MultiInstanceConfig({ data, onUpdate }: Pick<NodeConfigProps, 'data' | 'onUpdate'>) {
  const type = asText(data.multiInstanceType);
  const repeats = !RUNS_ONCE.has(type);
  const count = asNumber(data.loopCardinality);

  return (
    <Stack gap="sm">
      <Checkbox
        label="Multi-instance"
        checked={repeats}
        onChange={(e) => {
          if (e.currentTarget.checked) {
            onUpdate({ multiInstanceType: 'parallel', collection: 'items', elementVariable: 'item' });
          } else {
            onUpdate({
              multiInstanceType: 'none',
              loopCardinality: undefined,
              collection: '',
              elementVariable: '',
              completionCondition: '',
            });
          }
        }}
      />

      {repeats && (
        <Stack gap="sm" pl="xl">
          <Select
            label="Execution"
            data={executionOptions(type)}
            value={type}
            onChange={(value) => value && onUpdate({ multiInstanceType: value as 'parallel' | 'sequential' })}
            allowDeselect={false}
            size="sm"
          />
          <TextInput
            label="Collection"
            placeholder="e.g. users"
            description="Process variable containing a list"
            size="sm"
            value={asText(data.collection)}
            onChange={(e) => onUpdate({ collection: e.target.value })}
          />
          <NumberInput
            label="Loop Cardinality"
            placeholder="e.g. 3"
            description="How many times it runs when there is no collection"
            size="sm"
            min={0}
            value={count > 0 ? count : ''}
            onChange={(value) => onUpdate({ loopCardinality: typeof value === 'number' && value > 0 ? value : undefined })}
          />
          <TextInput
            label="Element Variable"
            placeholder="e.g. user"
            description="Variable name for current item"
            size="sm"
            value={asText(data.elementVariable)}
            onChange={(e) => onUpdate({ elementVariable: e.target.value })}
          />
          <TextInput
            label="Completion Condition"
            placeholder="e.g. nrOfCompletedInstances == nrOfInstances"
            size="sm"
            value={asText(data.completionCondition)}
            onChange={(e) => onUpdate({ completionCondition: e.target.value })}
          />
        </Stack>
      )}
    </Stack>
  );
}
