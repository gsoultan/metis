import { Alert, Stack, Text, TextInput } from '@mantine/core';

import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';
import { AssignmentSettings } from './AssignmentSettings';
import { LoopSettings } from './LoopSettings';

/**
 * Something a person does away from the system, then confirms here.
 *
 * Who does it is named the way it is for a user step, with the same fields.
 * This panel used to offer only a line of free text, which the engine never
 * read, and say that leaving it empty meant anybody could pick the step up.
 * A step nobody is named for is only an administrator's or an operator's to
 * take, whichever kind of step it is. The free text stays, for the steps that
 * already carry it, as what it always was: a note.
 */
export function ManualTaskConfig({ data, onUpdate }: NodeConfigProps) {
  return (
    <Stack gap="xl">
      <AssignmentSettings data={data} onUpdate={onUpdate}>
        <TextInput
          label="Person or role"
          placeholder="e.g. Warehouse manager"
          description="A note for whoever reads the diagram; it does not decide who can take the step."
          value={asText(data.actor)}
          onChange={(e) => onUpdate({ actor: e.target.value })}
        />
      </AssignmentSettings>

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          The process waits here until someone confirms the work is done. Nothing is
          asked of them beyond that — use a form step if you need them to record something.
        </Text>
      </Alert>

      <LoopSettings data={data} onUpdate={onUpdate} />
    </Stack>
  );
}
