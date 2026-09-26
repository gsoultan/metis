import { describe, expect, it } from 'bun:test';

import { csvBlob, csvField, csvFilename, toCsv } from './csv';

describe('csvField', () => {
  it('leaves an ordinary value alone', () => {
    expect(csvField('Approve')).toBe('Approve');
    expect(csvField(42)).toBe('42');
    expect(csvField(true)).toBe('true');
  });

  it('reads nothing as empty rather than as the word null', () => {
    expect(csvField(null)).toBe('');
    expect(csvField(undefined)).toBe('');
  });

  it('quotes a field that would otherwise shift every column after it', () => {
    // A comma in an unquoted field silently moves every later column, and the
    // file still opens — which is the worst way to be wrong.
    expect(csvField('Acme, Inc.')).toBe('"Acme, Inc."');
    expect(csvField('line one\nline two')).toBe('"line one\nline two"');
  });

  it('doubles a quote inside a quoted field', () => {
    expect(csvField('say "hello"')).toBe('"say ""hello"""');
  });

  it('defuses a value a spreadsheet would run as a formula', () => {
    // Process data is user-authored. A task named like a formula becomes
    // executable the moment somebody double-clicks the download.
    expect(csvField('=HYPERLINK("http://evil","click")')).toBe(
      `"'=HYPERLINK(""http://evil"",""click"")"`,
    );
    expect(csvField('+1')).toBe(`'+1`);
    expect(csvField('-1')).toBe(`'-1`);
    expect(csvField('@SUM(A1:A9)')).toBe(`'@SUM(A1:A9)`);
  });

  it('keeps the value it defused', () => {
    // Prefixing rather than stripping: the reader still gets the data, it just
    // stops being executable.
    expect(csvField('=2+2')).toContain('=2+2');
  });

  it('does not mistake a minus inside a value for a formula', () => {
    expect(csvField('re-quote')).toBe('re-quote');
  });
});

describe('toCsv', () => {
  it('writes a header and rows with CRLF, as the format says', () => {
    expect(toCsv(['a', 'b'], [[1, 2], [3, 4]])).toBe('a,b\r\n1,2\r\n3,4');
  });

  it('writes just the header when there is nothing to report', () => {
    // A file with only a header reads as "nothing is late"; an empty file reads
    // as "the export is broken".
    expect(toCsv(['Task', 'Due'], [])).toBe('Task,Due');
  });
});

describe('csvFilename', () => {
  it('dates the file so two downloads do not collide', () => {
    expect(csvFilename('sla', new Date('2026-09-23T11:00:00Z'))).toBe('sla-2026-09-23.csv');
  });
});

// Excel on Windows reads a CSV file with no byte order mark in the machine's
// legacy code page, so an assignee called José arrived as "JosÃ©" and a
// process named in Indonesian with any accented letter came out garbled.
describe('csvBlob', () => {
  it('starts with a UTF-8 byte order mark, so a spreadsheet reads the text as written', async () => {
    const bytes = new Uint8Array(await csvBlob('Assignee\r\nJosé').arrayBuffer());
    expect([...bytes.slice(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
    expect(new TextDecoder().decode(bytes)).toBe('Assignee\r\nJosé');
  });
});
