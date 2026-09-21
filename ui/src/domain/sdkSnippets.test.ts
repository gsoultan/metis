import { describe, expect, it } from 'bun:test';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { snippetFor, SNIPPET_LANGUAGES } from './sdkSnippets';
import type { SdkCall } from './sdkCalls';

const options = { baseUrl: 'https://bpm.example.com/api/v1' };

const START: SdkCall = {
  kind: 'start',
  projectId: 'proj-1',
  definitionKey: 'refund',
  variables: { amount: 42.5, note: "the customer's own words" },
  version: 0,
};

describe('every language', () => {
  const calls: SdkCall[] = [
    START,
    { kind: 'fetchAndLock', topic: 'reverse-charge', workerId: 'sandbox-1', maxTasks: 1, lockDurationMs: 60_000 },
    { kind: 'complete', taskId: 't1', workerId: 'sandbox-1', topic: 'reverse-charge', variables: { reversed: true } },
    { kind: 'fail', taskId: 't1', workerId: 'sandbox-1', topic: 'reverse-charge', errorMessage: 'card declined', retries: 2, retryTimeoutMs: 10_000 },
    { kind: 'message', projectId: 'proj-1', messageName: 'payment.received', correlationKey: 'order-4471', variables: {} },
    { kind: 'signal', projectId: 'proj-1', signalName: 'quarter.closed', variables: {} },
  ];

  for (const { id } of SNIPPET_LANGUAGES) {
    it(`writes something for every call in ${id}`, () => {
      for (const call of calls) {
        const snippet = snippetFor(id, call, options);
        expect(snippet.length).toBeGreaterThan(0);
        expect(snippet).not.toContain('undefined');
      }
    });

    it(`never interpolates a credential in ${id}`, () => {
      for (const call of calls) {
        const snippet = snippetFor(id, call, options);
        // The token is read from the environment in every language. A literal
        // one here is a token in every screenshot of this screen.
        expect(snippet).not.toMatch(/Bearer\s+ey/);
      }
    });
  }
});

describe('the Go snippet', () => {
  it('reads the token from the environment rather than printing one', () => {
    expect(snippetFor('go', START, options)).toContain('os.Getenv("METIS_TOKEN")');
  });

  it('renders variables as metis.Variables with Go literals', () => {
    const snippet = snippetFor('go', START, options);
    expect(snippet).toContain('metis.Variables{');
    expect(snippet).toContain('"amount": 42.5,');
    expect(snippet).toContain(String.raw`"note": "the customer's own words",`);
  });

  it('passes nil rather than an empty map when there is nothing to send', () => {
    const snippet = snippetFor('go', { ...START, variables: {} }, options);
    expect(snippet).toContain('nil)');
    expect(snippet).not.toContain('metis.Variables{}');
  });

  it('teaches the worker, not a hand-rolled fetch-complete loop', () => {
    const snippet = snippetFor('go', { kind: 'fetchAndLock', topic: 'reverse-charge', workerId: 'w1', maxTasks: 1, lockDurationMs: 60_000 }, options);
    expect(snippet).toContain('metis.NewWorker(client, "reverse-charge", "w1"');
    expect(snippet).toContain('worker.Run(ctx)');
  });

  it('writes a round retry timeout as a readable duration', () => {
    const snippet = snippetFor('go', { kind: 'fail', taskId: 't', workerId: 'w', topic: 'x', errorMessage: 'no', retries: 1, retryTimeoutMs: 600_000 }, options);
    expect(snippet).toContain('10 * time.Minute');
  });

  it('renders a nested object as map[string]any', () => {
    const snippet = snippetFor('go', { ...START, variables: { customer: { id: 7, vip: true } } }, options);
    expect(snippet).toContain('map[string]any{');
    expect(snippet).toContain('"vip": true,');
  });
});

describe('the curl snippet', () => {
  it('survives an apostrophe in the body, which would otherwise break the command', () => {
    const snippet = snippetFor('curl', START, options);
    expect(snippet).toContain(String.raw`'\''`);
    // Quoting is balanced once the escaped quote is discounted: an odd count
    // of *shell* quotes means the command swallows the rest of the line.
    const shellQuotes = snippet.replaceAll(String.raw`\'`, '').match(/'/g) ?? [];
    expect(shellQuotes.length % 2).toBe(0);
  });

  it('points at this deployment, so it runs without being edited', () => {
    expect(snippetFor('curl', START, options)).toContain('https://bpm.example.com/api/v1/process/start');
  });

  it('sends the idempotency key as a header', () => {
    const snippet = snippetFor('curl', { ...START, idempotencyKey: 'order-4471' }, options);
    expect(snippet).toContain("-H 'Idempotency-Key: order-4471'");
  });
});

describe('the fetch snippet', () => {
  it('handles the refusal shape the server actually sends', () => {
    const snippet = snippetFor('fetch', START, options);
    expect(snippet).toContain('response.ok');
    expect(snippet).toContain('.error');
  });
});

describe('the claim that there is no JavaScript SDK', () => {
  it('is still true — a published one would make this hint a lie', () => {
    const hint = SNIPPET_LANGUAGES.find((language) => language.id === 'fetch')?.hint ?? '';
    expect(hint).toContain('no JS SDK yet');

    // If a JS client is ever vendored in, this fails and the hint gets fixed
    // in the same change rather than a year later.
    const deps = JSON.parse(readFileSync(join(import.meta.dir, '..', '..', 'package.json'), 'utf8')) as {
      dependencies?: Record<string, string>;
    };
    expect(Object.keys(deps.dependencies ?? {}).filter((name) => name.includes('metis-sdk'))).toEqual([]);
  });
});

describe('the documented wire contract', () => {
  /**
   * The paths here must be paths the server actually serves. A snippet for an
   * endpoint that does not exist is the failure this screen exists to prevent,
   * and it would not show up in a UI test — the button would work and the
   * printed curl would 404.
   */
  it('names only routes the Go handlers register', () => {
    const repoRoot = join(import.meta.dir, '..', '..', '..');
    const httpsDir = join(repoRoot, 'server', 'transports', 'https');
    const registered = new Set<string>();

    const walk = (dir: string): void => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const path = join(dir, entry.name);
        if (entry.isDirectory()) {
          walk(path);
        } else if (entry.name.endsWith('.go')) {
          for (const match of readFileSync(path, 'utf8').matchAll(/m\.Handle\("(GET|POST) (\/api\/v1[^"]*)"/g)) {
            registered.add(`${match[1]} ${match[2]}`);
          }
        }
      }
    };
    walk(httpsDir);

    const used = [
      'POST /api/v1/process/start',
      'POST /api/v1/external-tasks/fetch-and-lock',
      'POST /api/v1/external-tasks/{id}/complete',
      'POST /api/v1/external-tasks/{id}/failure',
      'POST /api/v1/processes/message',
      'POST /api/v1/processes/signal',
      'GET /api/v1/instances/{id}',
      'GET /api/v1/instances/{id}/audit',
    ];

    expect(used.filter((route) => !registered.has(route))).toEqual([]);
  });
});
