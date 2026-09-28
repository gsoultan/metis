import {
  Code,
  Group,
  NumberInput,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { advancedVisibility, CHANGE_IN_EXPERT_MODE } from '../../domain/disclosure';
import { useAppStore } from '../../store/useAppStore';
import type { FormField } from '../FormBuilder';
import { FormBuilder } from '../FormBuilder';
import type { NodeConfigProps } from '../PropertyPanel';
import { AssignmentSettings } from './AssignmentSettings';
import { LoopSettings } from './LoopSettings';
import { PropertySection } from './PropertySection';
import { asText, asNumber } from '../../types/bpmn';

/**
 * The saved form, which is stored as free-form JSON and may be a string when it
 * came back from the server as one. Anything unrecognisable becomes an empty
 * form rather than reaching the builder as something it cannot render.
 */
function asFormFields(value: unknown): FormField[] {
  const parsed = typeof value === 'string' ? safeParse(value) : value;
  return Array.isArray(parsed) ? (parsed as FormField[]) : [];
}

function safeParse(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

/**
 * Work for a person: who is asked, when it is due, and what they fill in.
 *
 * The three questions are separated because they are decided at different
 * times — who does it is a policy decision, the form is design work, and the
 * timing is usually left alone. They used to be one column of fields under
 * "Assignment Strategy" and "Execution Details".
 */
export function UserTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const expertMode = useAppStore((state) => state.expertMode);

  const fields = asFormFields(data.formDefinition);
  const formKey = advancedVisibility(expertMode, data.formKey);

  return (
    <Stack gap="xl">
      <AssignmentSettings data={data} onUpdate={onUpdate} />

      <PropertySection
        title="What they fill in"
        hint={
          fields.length === 0
            ? 'With no fields, the person is only asked to confirm the work is done.'
            : 'Each field becomes a value the rest of the process can read, under the name you give it.'
        }
      >
        <FormBuilder
          fields={fields}
          onChange={(formDefinition) => onUpdate({ formDefinition })}
        />
      </PropertySection>

      <PropertySection title="Timing" hint="Optional. Both are for sorting and chasing; neither stops the process.">
        <Group grow align="flex-start">
          <NumberInput
            label="Priority"
            description="Higher comes first"
            min={0}
            value={asNumber(data.priority)}
            onChange={(val) => onUpdate({ priority: Number(val) || 0 })}
          />
          <TextInput
            label="Due"
            placeholder="e.g. PT24H, or 2026-03-01"
            description="A period from now, or a date"
            value={asText(data.dueDate)}
            onChange={(e) => onUpdate({ dueDate: e.target.value })}
          />
        </Group>
      </PropertySection>

      {formKey === 'summary' && (
        <PropertySection title="Form key" hint={CHANGE_IN_EXPERT_MODE}>
          <Text size="sm">
            Points at a form built outside this designer: <Code>{asText(data.formKey)}</Code>
          </Text>
        </PropertySection>
      )}

      {formKey === 'edit' && (
        <PropertySection title="Form key" hint="Points at a form built outside this designer. Leave empty to use the fields above.">
          <TextInput
            aria-label="Form key"
            placeholder="form_id"
            value={asText(data.formKey)}
            onChange={(e) => onUpdate({ formKey: e.target.value })}
          />
        </PropertySection>
      )}

      <LoopSettings data={data} onUpdate={onUpdate} />
    </Stack>
  );
}
