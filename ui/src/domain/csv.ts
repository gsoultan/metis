/**
 * Turning a table into a CSV file somebody opens in a spreadsheet.
 *
 * Two things make this worth having rather than joining with commas.
 *
 * A field can contain a comma, a quote or a newline, and one that is not quoted
 * shifts every column after it — silently, because the file still opens. An
 * export that misreports which approvals are late is worse than no export.
 *
 * And a field a spreadsheet reads as a formula is an attack. Process data is
 * user-authored: a task named `=HYPERLINK(...)` or a customer called
 * `@SUM(A1:A9)` becomes executable the moment somebody double-clicks the
 * download. Excel, LibreOffice and Sheets all do this, and it is the reason a
 * CSV export from an application that handles other people's data has to think
 * about it at all.
 */

/** A CSV cell. Anything nullish becomes empty rather than the string "null". */
export type CsvValue = string | number | boolean | null | undefined;

/**
 * The characters a spreadsheet treats as the start of a formula.
 *
 * Tab and carriage return are in the list because they are stripped by some
 * readers before the first character is examined, which puts the next one in
 * the firing line.
 */
const FORMULA_LEAD = ['=', '+', '-', '@', '\t', '\r'];

/**
 * Escapes one field.
 *
 * A leading formula character is prefixed with a single quote, which is the
 * convention spreadsheets understand as "this is text". The value is preserved
 * — nothing is dropped — it simply stops being executable.
 */
export function csvField(value: CsvValue): string {
  if (value === null || value === undefined) return '';

  let text = String(value);
  if (text !== '' && FORMULA_LEAD.includes(text[0])) {
    text = `'${text}`;
  }

  if (/[",\n\r]/.test(text)) {
    return `"${text.replaceAll('"', '""')}"`;
  }
  return text;
}

/**
 * Builds a CSV document from a header row and body rows.
 *
 * CRLF line endings, because that is what RFC 4180 says and what Excel on
 * Windows expects; every reader worth the name accepts it.
 */
export function toCsv(headers: readonly string[], rows: readonly CsvValue[][]): string {
  const lines = [headers.map(csvField).join(',')];
  for (const row of rows) {
    lines.push(row.map(csvField).join(','));
  }
  return lines.join('\r\n');
}

/**
 * A filename with the date in it, so two downloads do not collide in a
 * downloads folder and somebody can tell which is which a week later.
 */
export function csvFilename(prefix: string, now: Date = new Date()): string {
  const stamp = now.toISOString().slice(0, 10);
  return `${prefix}-${stamp}.csv`;
}

/**
 * The file a browser hands over for a CSV document.
 *
 * It starts with a UTF-8 byte order mark. Without one, Excel on Windows reads
 * the file in the machine's legacy code page, so an assignee called José
 * arrives as "JosÃ©". Every other reader skips the mark.
 */
export function csvBlob(contents: string): Blob {
  return new Blob(['\uFEFF', contents], { type: 'text/csv;charset=utf-8;' });
}
