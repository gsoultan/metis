import { describe, expect, it } from 'bun:test';

import type { ApiRoleAccess } from '../services/domains/roleService';
import { areaHeadingKey, legendFor } from './roleLegend';

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
