import {
  Group,
  MultiSelect,
  NumberInput,
  SegmentedControl,
  Select,
  Stack,
  TextInput,
} from '@mantine/core';
import { useState } from 'react';
import { useGroups, useUsers } from '../../hooks/useProcess';
import { useAppStore } from '../../store/useAppStore';
import { MultiInstanceConfig } from './CommonProperties';
import type { FormField } from '../FormBuilder';
import { FormBuilder } from '../FormBuilder';
import type { NodeConfigProps } from '../PropertyPanel';
import { PropertySection } from './PropertySection';
import { asText, asNumber, asTextList } from '../../types/bpmn';

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
  const { currentOrganizationId, expertMode } = useAppStore();
  const { data: usersData } = useUsers(currentOrganizationId);
  const { data: groupsData } = useGroups(currentOrganizationId);

  const availableUsers = (usersData?.users || []).map((u) => ({ value: u.username, label: u.full_name || u.username }));
  const availableGroups = (groupsData?.groups || []).map((g) => ({ value: g.name, label: g.name }));

  const hasDirectAssignment = !!data.assignee;
  const hasCandidates = asTextList(data.candidateUsers).length > 0 || asTextList(data.candidateGroups).length > 0;
  const initialMode = hasDirectAssignment ? 'direct' : hasCandidates ? 'pool' : asText(data.assignmentMode, 'direct');
  const [assignmentMode, setAssignmentMode] = useState(initialMode);

  const fields = asFormFields(data.formDefinition);

  return (
    <Stack gap="xl">
      <PropertySection
        title="Who does this"
        hint="Give it to one person, or offer it to several and let one take it."
      >
        <SegmentedControl
          fullWidth
          value={assignmentMode}
          onChange={(val) => {
            setAssignmentMode(val);
            if (val === 'direct') {
              onUpdate({ assignmentMode: 'direct', candidateUsers: [], candidateGroups: [] });
            } else {
              onUpdate({ assignmentMode: 'pool', assignee: '' });
            }
          }}
          data={[
            { label: 'One person', value: 'direct' },
            { label: 'Anyone from a group', value: 'pool' },
          ]}
        />

        {assignmentMode === 'direct' ? (
          <Select
            label="Assign to"
            placeholder="Choose a person"
            description="It appears in their list and nobody else's."
            data={availableUsers}
            value={asText(data.assignee)}
            onChange={(val) => onUpdate({ assignee: val || '' })}
            searchable
            clearable
          />
        ) : (
          <Stack gap="sm">
            <MultiSelect
              label="These people"
              placeholder="Anyone in particular"
              data={availableUsers}
              value={asTextList(data.candidateUsers)}
              onChange={(val) => onUpdate({ candidateUsers: val })}
              searchable
              clearable
            />
            <MultiSelect
              label="Or anyone in these teams"
              placeholder="e.g. finance"
              description="It waits in a shared list until one of them takes it."
              data={availableGroups}
              value={asTextList(data.candidateGroups)}
              onChange={(val) => onUpdate({ candidateGroups: val })}
              searchable
              clearable
            />
          </Stack>
        )}
      </PropertySection>

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

      {expertMode && (
        <>
          <PropertySection title="Form key" hint="Points at a form built outside this designer. Leave empty to use the fields above.">
            <TextInput
              placeholder="form_id"
              value={asText(data.formKey)}
              onChange={(e) => onUpdate({ formKey: e.target.value })}
            />
          </PropertySection>

          <MultiInstanceConfig data={data} onUpdate={onUpdate} />
        </>
      )}
    </Stack>
  );
}
