import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  SegmentedControl, 
  Select, 
  Grid, 
  MultiSelect, 
  Divider, 
  NumberInput, 
  TextInput, 
  Box, 
  Badge 
} from '@mantine/core';
import { User, Settings } from 'lucide-react';
import { useState } from 'react';
import { useAppStore } from '../../store/useAppStore';
import { useUsers, useGroups } from '../../hooks/useProcess';
import { MultiInstanceConfig } from './CommonProperties';
import type { FormField } from '../FormBuilder';
import { FormBuilder } from '../FormBuilder';
import type { NodeConfigProps } from '../PropertyPanel';
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

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="blue" radius="md">
            <User size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Assignment Strategy</Text>
        </Group>

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
            { label: 'Individual (Assignee)', value: 'direct' },
            { label: 'Candidate Pool', value: 'pool' },
          ]}
          mb="md"
        />
        
        {assignmentMode === 'direct' ? (
          <Select
            label="Assignee"
            placeholder="Select user"
            description="The specific user responsible for this task"
            data={availableUsers}
            value={asText(data.assignee)}
            onChange={(val) => onUpdate({ assignee: val || '' })}
            searchable
            clearable
          />
        ) : (
          <Grid gap="md">
            <Grid.Col span={{ base: 12, sm: 6 }}>
              <MultiSelect
                label="Candidate Users"
                placeholder="Select users"
                description="Users who can claim this task"
                data={availableUsers}
                value={asTextList(data.candidateUsers)}
                onChange={(val) => onUpdate({ candidateUsers: val })}
                searchable
                clearable
              />
            </Grid.Col>
            <Grid.Col span={{ base: 12, sm: 6 }}>
              <MultiSelect
                label="Candidate Groups"
                placeholder="Select groups"
                description="Groups whose members can claim this task"
                data={availableGroups}
                value={asTextList(data.candidateGroups)}
                onChange={(val) => onUpdate({ candidateGroups: val })}
                searchable
                clearable
              />
            </Grid.Col>
          </Grid>
        )}
      </Stack>

      <Divider variant="dashed" />

      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="orange" radius="md">
            <Settings size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Execution Details</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12, sm: 4 }}>
            <NumberInput
              label="Priority"
              description="Priority level (e.g. 0-100)"
              value={asNumber(data.priority)}
              onChange={(val) => onUpdate({ priority: Number(val) || 0 })}
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 8 }}>
            <TextInput
              label="Due Date"
              placeholder="PT24H or Date"
              description="ISO 8601 or date"
              value={asText(data.dueDate)}
              onChange={(e) => onUpdate({ dueDate: e.target.value })}
            />
          </Grid.Col>
          {expertMode && (
            <Grid.Col span={{ base: 12 }}>
              <TextInput
                label="Form Key"
                placeholder="form_id"
                description="Reference to an external or internal form"
                value={asText(data.formKey)}
                onChange={(e) => onUpdate({ formKey: e.target.value })}
              />
            </Grid.Col>
          )}
        </Grid>
      </Stack>

      {expertMode && (
        <>
          <Divider variant="dashed" />
          <MultiInstanceConfig data={data} onUpdate={onUpdate} />
        </>
      )}

      <Divider variant="dashed" />
      
      <Box>
         <Group justify="space-between" mb="sm">
            <Text fw={700} size="md">Form Designer</Text>
            <Badge variant="dot" color="blue">Dynamic</Badge>
         </Group>
         <FormBuilder 
            fields={asFormFields(data.formDefinition)}
            onChange={(formDefinition) => onUpdate({ formDefinition })}
         />
      </Box>
    </Stack>
  );
}
