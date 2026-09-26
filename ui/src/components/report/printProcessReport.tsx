import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';

import { ProcessReport, type ProcessReportFigures } from './ProcessReport';
import { REPORT_STYLES } from './reportStyles';

/** How long a frame is kept if the browser never says printing has finished. */
const FRAME_LIFETIME_MS = 60_000;

/**
 * Opens the browser's print dialog on the report, where "Save as PDF" is the
 * PDF export.
 *
 * Printed from a hidden frame of its own, so the page comes out as a report
 * rather than as the dashboard with its navigation. React renders into it, and
 * React writes every name as text, so a process or a person called
 * `<img onerror=…>` prints as those characters and runs nothing. The frame is
 * removed once the browser says printing is over, or after a minute if it never
 * does.
 *
 * Loaded when somebody asks for the report, not with the dashboard.
 */
export function printProcessReport(figures: ProcessReportFigures): void {
  const frame = document.createElement('iframe');
  frame.setAttribute('aria-hidden', 'true');
  frame.tabIndex = -1;
  Object.assign(frame.style, { position: 'fixed', right: '0', bottom: '0', width: '0', height: '0', border: '0' });
  document.body.appendChild(frame);

  const doc = frame.contentDocument;
  const win = frame.contentWindow;
  if (!doc || !win) {
    frame.remove();
    throw new Error('The report could not be prepared for printing.');
  }

  // The title is what "Save as PDF" suggests as the file's name.
  doc.title = `${figures.projectName} process report`;
  const style = doc.createElement('style');
  style.textContent = REPORT_STYLES;
  doc.head.appendChild(style);
  const mount = doc.createElement('div');
  doc.body.appendChild(mount);

  const root = createRoot(mount);
  flushSync(() => root.render(<ProcessReport figures={figures} />));

  let removed = false;
  const remove = () => {
    if (removed) return;
    removed = true;
    root.unmount();
    frame.remove();
  };
  win.addEventListener('afterprint', remove, { once: true });
  window.setTimeout(remove, FRAME_LIFETIME_MS);

  win.focus();
  win.print();
}
