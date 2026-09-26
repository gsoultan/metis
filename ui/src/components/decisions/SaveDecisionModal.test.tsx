/**
 * The question a save of a decision asks when a version is in force: put the
 * edit into force now, or stage it beside that version.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider, Modal, createTheme } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { SaveDecisionModal } from './SaveDecisionModal';

const theme = createTheme({ components: { Modal: Modal.extend({ defaultProps: { withinPortal: false } }) } });

function render(): string {
  const html = renderToStaticMarkup(
    <MantineProvider theme={theme}>
      <SaveDecisionModal opened name="Discount" nextVersion={5} liveVersion={4} saving={false} onClose={() => {}} onSave={() => {}} />
    </MantineProvider>,
  );
  return html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
}

describe('saving a decision that has a live version', () => {
  it('offers to make the new version live, or to stage it with the live one kept in force', () => {
    const text = render();
    expect(text).toContain('Save Discount v5');
    expect(text).toContain('Make v5 live');
    expect(text).toContain('Stage v5 for later');
    expect(text).toContain('steps that name no version keep using v4');
  });

  it('puts the new version live unless asked otherwise', () => {
    // Staging is a deliberate choice; the button starts on the recommended one.
    expect(render()).toContain('Save v5 and make it live');
  });
});
