import { describe, expect, it } from 'bun:test';
import { Table } from '@mantine/core';
import { createElement } from 'react';

import { ROLE_OPTIONS } from '../../domain/roles';
import type { ApiOrganizationUser } from '../../services/types';
import { namedControl, visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import { RoleCell } from './RoleCell';

const admin = ROLE_OPTIONS.find((option) => option.value === 'ADMIN')!;
const designer = ROLE_OPTIONS.find((option) => option.value === 'DESIGNER')!;

const dana: ApiOrganizationUser = { id: 'u-dana', username: 'dana', full_name: 'Dana Scully', roles: ['ADMIN'] };

/** One cell, inside the table row it belongs in. */
function cell(option: typeof admin, saving?: string): string {
  const td = createElement(RoleCell, { account: dana, option, canEdit: true, saving, onToggle: () => {} });
  return renderMarkup(createElement(Table, null, createElement(Table.Tbody, null, createElement(Table.Tr, null, td))));
}

/*
 * A box shows what the server holds, never what was clicked. While a change to
 * the person's roles is on its way, the box keeps showing the server's answer
 * and ignores clicks, so a refusal leaves it exactly as it was.
 */
describe('a box while a change is being saved', () => {
  it('still shows what the server holds, and says what is being saved', () => {
    const html = cell(admin, 'ADMIN');
    const box = namedControl(html, 'Administrator for Dana Scully');

    expect(box).toHaveProperty('checked');
    expect(box?.['aria-disabled']).toBe('true');
    expect(visibleText(html)).toContain('Saving Administrator for Dana Scully…');
  });

  it('holds the person’s other boxes still too, without taking them out of reach of the keyboard', () => {
    const html = cell(designer, 'ADMIN');
    const box = namedControl(html, 'Designer for Dana Scully');

    expect(box?.['aria-disabled']).toBe('true');
    expect(box).not.toHaveProperty('disabled');
    expect(visibleText(html)).not.toContain('Saving');
  });

  it('is an ordinary box when nothing is being saved', () => {
    const box = namedControl(cell(admin), 'Administrator for Dana Scully');
    expect(box).toHaveProperty('checked');
    expect(box).not.toHaveProperty('aria-disabled');
  });
});
