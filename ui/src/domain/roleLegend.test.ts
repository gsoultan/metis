import { describe, expect, it } from 'bun:test';

import en from '../i18n/catalogues/en';
import id from '../i18n/catalogues/id';
import { format, type Catalogue } from '../i18n/translate';
import type { ApiRoleAccess, ApiRoleAction } from '../services/domains/roleService';
import { actionLabel, areaHeadingKey, legendFor } from './roleLegend';

const access: ApiRoleAccess[] = [
  {
    role: 'DESIGNER',
    actions: [
      { method: 'CreateDefinition', area: 'processes', label: 'Create definition' },
      { method: 'DeleteDefinition', area: 'processes', label: 'Delete definition' },
      { method: 'UpdateDecision', area: 'decisions', label: 'Update decision' },
    ],
  },
  { role: 'QUERY_AUTHOR', actions: [] },
];

describe('a role’s legend', () => {
  it('groups the role’s actions under one heading per area, in the order the server gave', () => {
    expect(legendFor(access, 'DESIGNER')).toEqual([
      {
        area: 'processes',
        actions: [
          { method: 'CreateDefinition', area: 'processes', label: 'Create definition' },
          { method: 'DeleteDefinition', area: 'processes', label: 'Delete definition' },
        ],
      },
      { area: 'decisions', actions: [{ method: 'UpdateDecision', area: 'decisions', label: 'Update decision' }] },
    ]);
  });

  it('keeps an area together even when the server splits it', () => {
    const split: ApiRoleAccess[] = [
      {
        role: 'ADMIN',
        actions: [
          { method: 'CreateGroup', area: 'groups', label: 'Create group' },
          { method: 'CreateUser', area: 'accounts', label: 'Create user' },
          { method: 'DeleteGroup', area: 'groups', label: 'Delete group' },
        ],
      },
    ];
    expect(legendFor(split, 'ADMIN')?.map((area) => [area.area, area.actions.length])).toEqual([
      ['groups', 2],
      ['accounts', 1],
    ]);
  });

  it('finds the role whatever case it is asked for in', () => {
    expect(legendFor(access, 'designer')?.[0].area).toBe('processes');
  });

  it('answers a role required for nothing with an empty legend, and an unlisted one with none', () => {
    expect(legendFor(access, 'QUERY_AUTHOR')).toEqual([]);
    expect(legendFor(access, 'OPERATOR')).toBeUndefined();
  });

  it('names a heading by its area, and one the server could not place as other', () => {
    expect(areaHeadingKey('processes')).toBe('access.area.processes');
    expect(areaHeadingKey('')).toBe('access.area.other');
  });
});

/*
 * The server words each action from its method name, in English. The legend
 * reads in the interface's language: the catalogues word each action by the
 * same name, and an action they do not know yet keeps the server's words, so
 * a gate added tomorrow is listed under its English name rather than as a
 * blank or a key.
 */
describe('an action in the legend', () => {
  const inCatalogue = (catalogue: Catalogue) => (key: string) => format(catalogue, key);
  const create: ApiRoleAction = { method: 'CreateDefinition', area: 'processes', label: 'Create definition' };
  const unknown: ApiRoleAction = { method: 'ArchiveDefinition', area: 'processes', label: 'Archive definition' };

  it('reads in the interface’s language, by its method', () => {
    expect(actionLabel(create, inCatalogue(en))).toBe('Create definition');
    expect(actionLabel(create, inCatalogue(id))).toBe('Buat definisi');
  });

  it('keeps the server’s words for a method the catalogue does not know yet', () => {
    expect(actionLabel(unknown, inCatalogue(en))).toBe('Archive definition');
    expect(actionLabel(unknown, inCatalogue(id))).toBe('Archive definition');
  });

  it('is never blank, even when the server gave no words', () => {
    expect(actionLabel({ ...unknown, label: '' }, inCatalogue(id))).toBe('ArchiveDefinition');
  });
});
