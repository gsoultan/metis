/**
 * The report's own stylesheet. Static: nothing a person typed is in it, so it is
 * safe to set as text. Colours are forced to print, because a bar printed
 * without its fill is an empty box.
 */
export const REPORT_STYLES = `
@page { size: A4; margin: 16mm 14mm; }
* { box-sizing: border-box; }
body { margin: 0; font: 10.5pt/1.45 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; color: #111; }
h1 { font-size: 18pt; margin: 0 0 2pt; }
h2 { font-size: 13pt; margin: 16pt 0 6pt; border-bottom: 1px solid #ccc; padding-bottom: 2pt; }
.meta { color: #555; margin: 0; }
table { width: 100%; border-collapse: collapse; margin: 6pt 0 10pt; page-break-inside: auto; }
caption { text-align: left; font-weight: 600; margin-bottom: 3pt; }
th, td { text-align: left; padding: 3pt 6pt; border-bottom: 1px solid #e3e3e3; vertical-align: top; }
thead { display: table-header-group; }
tr { page-break-inside: avoid; }
.figures th { width: 40%; font-weight: 500; }
.count { text-align: right; white-space: nowrap; width: 1%; }
.bar-cell { width: 40%; }
.bar { display: block; height: 7pt; background: #e8590c; border-radius: 2pt;
  -webkit-print-color-adjust: exact; print-color-adjust: exact; }
footer { margin-top: 18pt; color: #555; font-size: 9pt; }
`;
