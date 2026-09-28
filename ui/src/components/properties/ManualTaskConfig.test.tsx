/**
 * The manual step's panel: who does the step, shown and edited.
 *
 * A manual step had no field to name anybody for it — a line of free text,
 * which the engine never read, and a hint that an empty one was anybody's. A
 * task nobody was named for is an administrator's or an operator's, so the
 * panel names people the way a user step's does, with the same fields.
 */
import { afterEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { isValidElement, type ReactElement, type ReactNode } from 'react';

import { appStoreDouble, resetAppStore, setAppState } from '../../testing/appStoreDouble';
import { labelledControl, visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import type { BPMNNodeData } from '../../types/bpmn';

mock.module('../../store/useAppStore', () => ({ useAppStore: appStoreDouble }));

const { ManualTaskConfig } = await import('./ManualTaskConfig');

const ORGANIZATION = 'acme';
const PEOPLE = [
  { username: 'dana', full_name: 'Dana Scully' },
  { username: 'eli', full_name: 'Eli Ness' },
];
const TEAMS = [{ id: 'g1', name: 'warehouse' }];

afterEach(resetAppStore);

const step = (settings: Record<string, unknown>) =>
  ({ label: 'Ship the parcel', nodeType: 'manualTask', ...settings }) as BPMNNodeData;

/** The panel, with the organization's people and teams already read. */
function panel(settings: Record<string, unknown>): string {
  setAppState({ currentOrganizationId: ORGANIZATION });
  const client = new QueryClient();
  client.setQueryData(['users', ORGANIZATION], { users: PEOPLE });
  client.setQueryData(['groups', ORGANIZATION], { groups: TEAMS });
  return renderMarkup(<ManualTaskConfig data={step(settings)} onUpdate={() => undefined} />, client);
}

/** The text from a control's label onwards. */
function from(html: string, label: string): string {
  const at = html.indexOf(`>${label}</label>`);
  return at < 0 ? '' : visibleText(html.slice(at));
}

describe('who does a manual step', () => {
  it('can be one person', () => {
    expect(labelledControl(panel({ assignee: 'dana' }), 'Assign to')?.value).toBe('Dana Scully');
  });

  it('can be people or teams, one of whom takes it', () => {
    const html = panel({ candidateUsers: ['eli'], candidateGroups: ['warehouse'] });

    expect(from(html, 'These people')).toContain('Eli Ness');
    expect(from(html, 'Or anyone in these teams')).toContain('warehouse');
  });

  /* Nobody named is not anybody: it is the administrators' and the operators'. */
  it('is not said to be anybody when nobody is named', () => {
    const text = visibleText(panel({ actor: 'Warehouse manager' }));

    expect(text).not.toContain('Leave empty if anyone can pick it up');
    expect(text).toContain('it does not decide who can take the step');
  });
});

/** Every element a component returned, found without rendering any of them. */
function elementsIn(node: ReactNode): ReactElement<Record<string, unknown>>[] {
  if (Array.isArray(node)) return node.flatMap(elementsIn);
  if (!isValidElement<Record<string, unknown>>(node)) return [];
  return [node, ...elementsIn(node.props.children as ReactNode)];
}

/** Changes the control a label or an accessible name names, and returns what the panel wrote. */
async function edit(settings: Record<string, unknown>, name: string, value: unknown): Promise<Partial<BPMNNodeData>[]> {
  const { AssignmentFields } = await import('./AssignmentSettings');
  const written: Partial<BPMNNodeData>[] = [];
  const fields = AssignmentFields({
    data: step(settings),
    onUpdate: (change) => written.push(change),
    people: PEOPLE.map((person) => ({ value: person.username, label: person.full_name })),
    teams: TEAMS.map((team) => ({ value: team.name, label: team.name })),
  });
  const control = elementsIn(fields).find((el) => el.props.label === name || el.props['aria-label'] === name);
  if (!control) throw new Error(`nothing on the panel is named "${name}"`);
  (control.props.onChange as (changed: unknown) => void)(value);
  return written;
}

describe('editing who does a step', () => {
  it('gives it to the person chosen', async () => {
    expect(await edit({}, 'Assign to', 'dana')).toEqual([{ assignee: 'dana' }]);
  });

  it('gives it to nobody when the person is cleared', async () => {
    expect(await edit({ assignee: 'dana' }, 'Assign to', null)).toEqual([{ assignee: '' }]);
  });

  it('offers it to the people chosen', async () => {
    expect(await edit({ assignmentMode: 'pool' }, 'These people', ['dana', 'eli']))
      .toEqual([{ assignmentMode: 'pool', candidateUsers: ['dana', 'eli'] }]);
  });

  it('offers it to the teams chosen', async () => {
    expect(await edit({ assignmentMode: 'pool' }, 'Or anyone in these teams', ['warehouse']))
      .toEqual([{ assignmentMode: 'pool', candidateGroups: ['warehouse'] }]);
  });

  it('lets go of the person when it is offered to several instead', async () => {
    expect(await edit({ assignee: 'dana' }, 'Who is asked', 'pool')).toEqual([{ assignmentMode: 'pool', assignee: '' }]);
  });

  it('lets go of the people and teams when it is given to one person instead', async () => {
    expect(await edit({ candidateUsers: ['eli'], candidateGroups: ['warehouse'] }, 'Who is asked', 'direct'))
      .toEqual([{ assignmentMode: 'direct', candidateUsers: [], candidateGroups: [] }]);
  });
});
