import { MultiSelect, SegmentedControl, Select, Stack, type ComboboxItem } from '@mantine/core';
import type { ReactNode } from 'react';

import { useGroups, useUsers } from '../../hooks/useProcess';
import { useAppStore } from '../../store/useAppStore';
import { asText, asTextList, type BPMNNodeData } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { PropertySection } from './PropertySection';

/**
 * Who does a step that asks a person: one person, or people and teams one of
 * whom takes it.
 *
 * Shared by the user step and the manual step, because the server holds both to
 * one rule — a step that names nobody is only an administrator's or an
 * operator's to take — so both need the same way to name somebody. The manual
 * step had a line of free text instead, which the engine never read.
 *
 * Split in two: this part reads the organization's people and teams, and
 * AssignmentFields draws the fields from what it is given.
 */
export function AssignmentSettings({ data, onUpdate, children }: Pick<NodeConfigProps, 'data' | 'onUpdate'> & { children?: ReactNode }) {
  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const { data: usersData } = useUsers(currentOrganizationId);
  const { data: groupsData } = useGroups(currentOrganizationId);

  const people = (usersData?.users || []).map((u) => ({ value: u.username, label: u.full_name || u.username }));
  const teams = (groupsData?.groups || []).map((g) => ({ value: g.name, label: g.name }));

  return (
    <PropertySection
      title="Who does this"
      hint="Give it to one person, or offer it to several and let one take it."
    >
      <AssignmentFields data={data} onUpdate={onUpdate} people={people} teams={teams} />
      {children}
    </PropertySection>
  );
}

type AssignmentMode = 'direct' | 'pool';

/**
 * How the step names who does it, read from what it names.
 *
 * Derived on every render rather than held in state, so the fields show what
 * the step names even when that changed somewhere else — in expert mode's raw
 * settings, say, where a held choice stayed on "One person" and hid the teams
 * just given to the step.
 */
function assignmentModeOf(data: BPMNNodeData): AssignmentMode {
  if (asText(data.assignee)) return 'direct';
  if (asTextList(data.candidateUsers).length > 0 || asTextList(data.candidateGroups).length > 0) return 'pool';
  return asText(data.assignmentMode) === 'pool' ? 'pool' : 'direct';
}

interface AssignmentFieldsProps extends Pick<NodeConfigProps, 'data' | 'onUpdate'> {
  /** Who the step can be given to, as the pickers list them. */
  people: ComboboxItem[];
  teams: ComboboxItem[];
}

/**
 * The fields themselves. Choosing one way forgets the other, so a step never
 * names a person and a team at once without its author seeing both. Each list
 * says which way it belongs to, so emptying it does not flip the choice back.
 */
export function AssignmentFields({ data, onUpdate, people, teams }: AssignmentFieldsProps) {
  const mode = assignmentModeOf(data);

  return (
    <>
      <SegmentedControl
        aria-label="Who is asked"
        fullWidth
        value={mode}
        onChange={(val) => {
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

      {mode === 'direct' ? (
        <Select
          label="Assign to"
          placeholder="Choose a person"
          description="It appears in their list and nobody else's."
          data={people}
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
            data={people}
            value={asTextList(data.candidateUsers)}
            onChange={(val) => onUpdate({ assignmentMode: 'pool', candidateUsers: val })}
            searchable
            clearable
          />
          <MultiSelect
            label="Or anyone in these teams"
            placeholder="e.g. finance"
            description="It waits in a shared list until one of them takes it."
            data={teams}
            value={asTextList(data.candidateGroups)}
            onChange={(val) => onUpdate({ assignmentMode: 'pool', candidateGroups: val })}
            searchable
            clearable
          />
        </Stack>
      )}
    </>
  );
}
