import { describe, expect, it } from 'bun:test';
import { createElement } from 'react';

import id from '../../i18n/catalogues/id';
import { inLanguage } from '../../test/renderStatic';
import { visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import { AccountRoleBadges } from './AccountRoleBadges';

/*
 * The Accounts view listed an account's roles as one row of badges, and they
 * were all the account's own — held in every organization it belonged to. Now
 * an account holds some roles in this organization alone, and the list says
 * which are which.
 */
describe('the roles on the Accounts view', () => {
  const kim = { roles: ['operator'], organization_roles: ['ADMIN', 'DESIGNER'] };

  it('names the roles held here as they are, and marks the ones held in every organization', () => {
    const text = visibleText(renderMarkup(createElement(AccountRoleBadges, { account: kim })));

    expect(text).toContain('Administrator Designer');
    // Written in lowercase by the older picker: the same role, in the server's words.
    expect(text).toContain('Operator · every organization');
    expect(text).not.toContain('Administrator · every organization');
  });

  it('marks them in the interface’s language', () => {
    const html = renderMarkup(inLanguage(createElement(AccountRoleBadges, { account: kim }), 'id', id));
    expect(visibleText(html)).toContain('Operator · setiap organisasi');
  });

  it('shows nothing for an account that holds nothing', () => {
    expect(visibleText(renderMarkup(createElement(AccountRoleBadges, { account: {} })))).toBe('');
  });
});
