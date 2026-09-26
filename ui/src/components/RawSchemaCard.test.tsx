import { afterEach, describe, expect, it, mock } from 'bun:test';

import { appStoreDouble, resetAppStore, setAppState } from '../testing/appStoreDouble';
import { isHiddenAt, labelledControl, visibleText } from '../testing/markup';
import { renderMarkup } from '../testing/renderMarkup';

mock.module('../store/useAppStore', () => ({ useAppStore: appStoreDouble }));

const { RawSchemaCard } = await import('./RawSchemaCard');

afterEach(resetAppStore);

const settings = { label: 'Approve', nodeType: 'userTask' };

function render(expertMode: boolean): string {
  setAppState({ expertMode });
  return renderMarkup(<RawSchemaCard settings={settings} onApply={() => undefined} />);
}

const editorAt = (html: string) => html.indexOf('<textarea');

/**
 * Turning Expert mode off keeps what was typed in the raw editor.
 *
 * The editor was rendered only in Expert mode, so turning the mode off
 * unmounted it and threw away any JSON typed and not yet applied, without a
 * word. It stays on the page in basic mode, hidden, and whatever it held is
 * there again when Expert mode comes back.
 */
describe('the raw schema card', () => {
  it('shows the editor in expert mode', () => {
    const html = render(true);

    expect(labelledControl(html, 'Raw Node Schema')?.value).toBe(JSON.stringify(settings, null, 2));
    expect(isHiddenAt(html, editorAt(html))).toBe(false);
  });

  it('keeps the editor on the page in basic mode, hidden', () => {
    const html = render(false);

    expect(labelledControl(html, 'Raw Node Schema')).toBeDefined();
    expect(isHiddenAt(html, editorAt(html))).toBe(true);
  });

  it('says in basic mode where the advanced settings went', () => {
    expect(visibleText(render(false))).toContain('Simplified view');
    expect(visibleText(render(true))).not.toContain('Simplified view');
  });
});
