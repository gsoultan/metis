import { describe, expect, it } from 'bun:test';
import { createElement } from 'react';

import { ROLE_OPTIONS } from '../../domain/roles';
import id from '../../i18n/catalogues/id';
import { inLanguage } from '../../test/renderStatic';
import { renderMarkup } from '../../testing/renderMarkup';
import { visibleText } from '../../testing/markup';
import { RoleLegend, type LegendState } from './RoleLegend';

const designer = ROLE_OPTIONS.find((option) => option.value === 'DESIGNER')!;
const queryAuthor = ROLE_OPTIONS.find((option) => option.value === 'QUERY_AUTHOR')!;

const designerLegend: LegendState = {
  status: 'known',
  areas: [
    {
      area: 'processes',
      actions: [
        { method: 'CreateDefinition', area: 'processes', label: 'Create definition' },
        { method: 'PromoteDefinition', area: 'processes', label: 'Promote definition' },
      ],
    },
    { area: 'decisions', actions: [{ method: 'UpdateDecision', area: 'decisions', label: 'Update decision' }] },
  ],
};

const text = (option = designer, legend: LegendState = designerLegend) =>
  visibleText(renderMarkup(createElement(RoleLegend, { option, legend })));

/*
 * What a role allows was one sentence per role, written by hand. The sentence
 * stays as the summary; under it is what the server's gates admit the role to,
 * which is the part that cannot say something the server would refuse.
 */
describe('what a role allows', () => {
  it('lists what the gates admit the role to, under the area each belongs to', () => {
    const shown = text();
    expect(shown).toContain('Designer');
    expect(shown).toContain(designer.description);
    expect(shown).toContain('Required for Processes Create definition Promote definition Decisions Update decision');
  });

  /*
   * It said "Anything not listed is open to anybody signed in", which claims
   * more than a role check can: completing a task still takes being the one it
   * is assigned to, and deploying a lookup still takes the Query author role.
   * What an unlisted action does not take is a role.
   */
  it('says that what it does not list needs no role, not that anybody may do it', () => {
    const shown = text();
    expect(shown).toContain('Actions not listed here need no role, only a sign-in.');
    expect(shown).not.toContain('open to anybody');
  });

  it('says where a role no action requires by itself is checked, rather than showing an empty list', () => {
    // The query author is checked while a process is deployed, not by a gate.
    const shown = text(queryAuthor, { status: 'known', areas: [] });
    expect(shown).toContain('No action requires this role by itself. It is checked as part of other actions, as described above.');
    expect(shown).toContain(queryAuthor.description);
  });

  it('says it is still asking, and says so when it could not find out', () => {
    expect(text(designer, { status: 'loading' })).toContain('Loading…');
    expect(text(designer, { status: 'failed' })).toContain('Could not load what this role allows.');
  });

  it('gives the headings in the interface’s language', () => {
    const html = renderMarkup(inLanguage(createElement(RoleLegend, { option: designer, legend: designerLegend }), 'id', id));
    expect(visibleText(html)).toContain('Diperlukan untuk Proses Create definition');
  });
});
