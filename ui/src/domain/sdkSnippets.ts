/**
 * The same call, written out as code you can paste.
 *
 * Generated from `requestFor` rather than from a second table of examples, so a
 * snippet cannot describe a request the sandbox did not just make. The values
 * are the ones you filled in — a real project id, the real topic off the model,
 * the variables you typed — because an example full of `YOUR_PROJECT_ID` is an
 * example you have to translate before you can run it, and translating is where
 * the typo goes in.
 *
 * **No credential is ever interpolated.** Every snippet reads the token from the
 * environment. A page that prints a working bearer token is a page whose
 * screenshots leak one, and this screen is the one people screenshot.
 */
import { requestFor, type SdkCall } from './sdkCalls';
import type { ProcessVariables } from '../services/types';

export type SnippetLanguage = 'go' | 'curl' | 'fetch';

export interface SnippetOptions {
  /** Where this Metis lives, so the snippet runs without being edited first. */
  baseUrl: string;
}

export const SNIPPET_LANGUAGES: ReadonlyArray<{ id: SnippetLanguage; label: string; hint: string }> = [
  { id: 'go', label: 'Go SDK', hint: 'github.com/gsoultan/metis-sdk' },
  { id: 'curl', label: 'curl', hint: 'The raw HTTP call' },
  { id: 'fetch', label: 'JavaScript', hint: 'Plain fetch — there is no JS SDK yet' },
];

export function snippetFor(language: SnippetLanguage, call: SdkCall, options: SnippetOptions): string {
  switch (language) {
    case 'go':
      return goSnippet(call, options);
    case 'curl':
      return curlSnippet(call, options);
    case 'fetch':
      return fetchSnippet(call, options);
  }
}

// ─── Go ──────────────────────────────────────────────────────────────────────

/**
 * The Go SDK form of a call.
 *
 * The worker calls are deliberately shown as `metis.NewWorker` rather than as
 * three separate fetch/complete/fail calls: the SDK's worker owns the lock
 * lifetime, cancels the handler's context when the lock would expire, and
 * reports the outcome for you. Showing the raw calls would teach somebody to
 * reimplement all of that, and the first thing they would get wrong is the one
 * the lock exists to prevent — two workers charging the same card.
 */
function goSnippet(call: SdkCall, { baseUrl }: SnippetOptions): string {
  const client = [
    `client := metis.NewClient(${quote(baseUrl)}, metis.WithToken(os.Getenv("METIS_TOKEN")))`,
  ].join('\n');

  switch (call.kind) {
    case 'start': {
      const vars = goVariables(call.variables, 1);
      return [
        client,
        '',
        `instanceID, err := client.StartProcess(ctx, ${quote(call.projectId)}, ${quote(call.definitionKey)},`,
        `\t${vars})`,
        'if err != nil {',
        `\treturn fmt.Errorf("starting ${call.definitionKey}: %w", err)`,
        '}',
        'log.Printf("started %s", instanceID)',
      ].join('\n');
    }

    case 'fetchAndLock':
      return [
        client,
        '',
        `worker := metis.NewWorker(client, ${quote(call.topic)}, ${quote(call.workerId)},`,
        '\tmetis.WorkerOptions{},',
        '\tfunc(ctx context.Context, task *metis.ExternalTask) (metis.Variables, error) {',
        '\t\t// Every number the engine sends is a float64, so a .(int)',
        '\t\t// assertion here would panic. Use the typed accessors.',
        '\t\t// amount, ok := task.Variables.Float64("amount")',
        '\t\treturn metis.Variables{"done": true}, nil',
        '\t})',
        '',
        'log.Fatal(worker.Run(ctx)) // polls until ctx is cancelled',
      ].join('\n');

    case 'complete':
      return [
        `// What your handler returns is what the engine writes back into the`,
        `// instance, then the process carries on from this step.`,
        `worker := metis.NewWorker(client, ${quote(call.topic)}, ${quote(call.workerId)},`,
        '\tmetis.WorkerOptions{},',
        '\tfunc(ctx context.Context, task *metis.ExternalTask) (metis.Variables, error) {',
        '\t\t// … your work here …',
        `\t\treturn ${goVariables(call.variables, 2)}, nil`,
        '\t})',
      ].join('\n');

    case 'fail':
      return [
        '// A returned error fails the task and spends one retry. When the',
        '// retries run out it stays failed and raises an incident for an',
        '// operator — it is not lost.',
        `worker := metis.NewWorker(client, ${quote(call.topic)}, ${quote(call.workerId)},`,
        `\tmetis.WorkerOptions{Retries: ${call.retries}, RetryTimeout: ${goDuration(call.retryTimeoutMs)}},`,
        '\tfunc(ctx context.Context, task *metis.ExternalTask) (metis.Variables, error) {',
        `\t\treturn nil, errors.New(${quote(call.errorMessage || 'the call failed')})`,
        '\t})',
      ].join('\n');

    case 'message':
      return [
        client,
        '',
        `err := client.SendMessage(ctx, ${quote(call.projectId)}, ${quote(call.messageName)},`,
        `\t${quote(call.correlationKey)}, ${goVariables(call.variables, 1)})`,
      ].join('\n');

    case 'signal':
      return [
        client,
        '',
        `err := client.BroadcastSignal(ctx, ${quote(call.projectId)}, ${quote(call.signalName)},`,
        `\t${goVariables(call.variables, 1)})`,
      ].join('\n');
  }
}

/** `metis.Variables{…}`, or `nil` when there is nothing to send. */
function goVariables(variables: ProcessVariables, depth: number): string {
  const entries = Object.entries(variables);
  if (entries.length === 0) {
    return 'nil';
  }
  const pad = '\t'.repeat(depth + 1);
  const close = '\t'.repeat(depth);
  const lines = entries.map(([key, value]) => `${pad}${quote(key)}: ${goValue(value, depth + 1)},`);
  return ['metis.Variables{', ...lines, `${close}}`].join('\n');
}

function goValue(value: ProcessVariables[string], depth: number): string {
  if (value === null) return 'nil';
  if (typeof value === 'string') return quote(value);
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  if (typeof value === 'number') return Number.isInteger(value) ? String(value) : String(value);
  if (Array.isArray(value)) {
    if (value.length === 0) return '[]any{}';
    const pad = '\t'.repeat(depth + 1);
    const close = '\t'.repeat(depth);
    return ['[]any{', ...value.map((item) => `${pad}${goValue(item, depth + 1)},`), `${close}}`].join('\n');
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return 'map[string]any{}';
  const pad = '\t'.repeat(depth + 1);
  const close = '\t'.repeat(depth);
  return [
    'map[string]any{',
    ...entries.map(([key, nested]) => `${pad}${quote(key)}: ${goValue(nested, depth + 1)},`),
    `${close}}`,
  ].join('\n');
}

/** Milliseconds as a Go duration expression, kept readable rather than exact-to-the-nanosecond. */
function goDuration(ms: number): string {
  if (ms > 0 && ms % 60_000 === 0) return `${ms / 60_000} * time.Minute`;
  if (ms > 0 && ms % 1_000 === 0) return `${ms / 1_000} * time.Second`;
  return `${ms} * time.Millisecond`;
}

/**
 * A Go string literal.
 *
 * JSON.stringify is the right escaper here rather than a coincidence: the
 * escapes it emits — \" \\ \n \r \t \uXXXX — are all valid in a Go interpreted
 * string literal and mean the same thing in both languages.
 */
function quote(value: string): string {
  return JSON.stringify(value);
}

// ─── curl ────────────────────────────────────────────────────────────────────

function curlSnippet(call: SdkCall, { baseUrl }: SnippetOptions): string {
  const request = requestFor(call);
  const lines = [`curl -X ${request.method} ${baseUrl}${request.path} \\`, `  -H "Authorization: Bearer $TOKEN" \\`];
  for (const [name, value] of Object.entries(request.headers ?? {})) {
    lines.push(`  -H ${shellQuote(`${name}: ${value}`)} \\`);
  }
  if (request.body === undefined) {
    return lines.join('\n').replace(/ \\$/, '');
  }
  lines.push(`  -H 'Content-Type: application/json' \\`);
  lines.push(`  -d ${shellQuote(JSON.stringify(request.body, null, 2))}`);
  return lines.join('\n');
}

/**
 * A single-quoted shell word.
 *
 * `'\''` is the only way to get a single quote inside one: close the quote,
 * emit an escaped quote, reopen. A JSON body containing an apostrophe — a
 * customer's name — would otherwise produce a command that does not parse.
 */
function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

// ─── fetch ───────────────────────────────────────────────────────────────────

function fetchSnippet(call: SdkCall, { baseUrl }: SnippetOptions): string {
  const request = requestFor(call);
  const headers = [`    Authorization: \`Bearer \${token}\`,`];
  if (request.body !== undefined) {
    headers.push(`    'Content-Type': 'application/json',`);
  }
  for (const [name, value] of Object.entries(request.headers ?? {})) {
    headers.push(`    ${JSON.stringify(name)}: ${JSON.stringify(value)},`);
  }

  const lines = [
    `const response = await fetch(${quote(`${baseUrl}${request.path}`)}, {`,
    `  method: '${request.method}',`,
    '  headers: {',
    ...headers,
    '  },',
  ];
  if (request.body !== undefined) {
    lines.push(`  body: JSON.stringify(${indentJson(request.body, 1)}),`);
  }
  lines.push('});');
  lines.push('if (!response.ok) {');
  lines.push('  // The server reports a refusal as { "error": "…" } with a 4xx.');
  lines.push('  throw new Error((await response.json()).error);');
  lines.push('}');
  return lines.join('\n');
}

function indentJson(value: unknown, depth: number): string {
  const pad = '  '.repeat(depth);
  return JSON.stringify(value, null, 2)
    .split('\n')
    .map((line, index) => (index === 0 ? line : pad + line))
    .join('\n');
}
