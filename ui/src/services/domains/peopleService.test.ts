import { afterEach, beforeEach, describe, expect, it } from 'bun:test';
import { actionsFor, peopleService, PeopleSessionEnded, type PersonTask } from './peopleService';
import { projectFromSearch } from './peopleSession';

const task = (over: Partial<PersonTask>): PersonTask => ({ id: 't1', name: 'Approve', status: 'unclaimed', ...over });

describe('what a person may do with a task', () => {
  it('takes a pool task nobody holds, and does nothing else with it yet', () => {
    expect(actionsFor(task({ candidate_groups: [{ name: 'approvers' }] }), 'ada')).toEqual({ claim: true, release: false, complete: false });
  });

  it('completes a task given to them by name, and cannot give it back', () => {
    expect(actionsFor(task({ status: 'claimed', assignee: { username: 'ada' } }), 'ada')).toEqual({ claim: false, release: false, complete: true });
  });

  it('completes or gives back a pool task they took', () => {
    const taken = task({ status: 'claimed', assignee: { username: 'ada' }, candidate_users: [{ username: 'ada' }, { username: 'bob' }] });
    expect(actionsFor(taken, 'ada')).toEqual({ claim: false, release: true, complete: true });
  });

  it('does nothing with a task somebody else holds', () => {
    expect(actionsFor(task({ status: 'claimed', assignee: { username: 'bob' } }), 'ada')).toEqual({ claim: false, release: false, complete: false });
  });
});

describe('the People routes', () => {
  const realFetch = globalThis.fetch;
  let seen: { url: string; init: RequestInit }[] = [];
  let answer: Response;

  beforeEach(() => {
    seen = [];
    globalThis.fetch = (async (url: string, init: RequestInit) => {
      seen.push({ url, init });
      return answer.clone();
    }) as typeof fetch;
  });
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  it("send the person's own token, never the User session's", async () => {
    answer = new Response(JSON.stringify({ tasks: [task({})] }), { status: 200 });
    const tasks = await peopleService.listTasks('v4.local.person');
    expect(tasks).toHaveLength(1);
    expect((seen[0].init.headers as Record<string, string>).Authorization).toBe('Bearer v4.local.person');
  });

  it('say the session is over when the token is refused', async () => {
    answer = new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401 });
    await expect(peopleService.listTasks('v4.local.old')).rejects.toBeInstanceOf(PeopleSessionEnded);
  });

  it('send the completion as variables', async () => {
    answer = new Response('{}', { status: 200 });
    await peopleService.complete('tok', 'a/b', { approved: true });
    expect(seen[0].url).toEndWith('/people/tasks/a%2Fb/complete');
    expect(JSON.parse(String(seen[0].init.body))).toEqual({ variables: { approved: true } });
  });
});

describe('the sign-in address', () => {
  it('names the project in its query', () => {
    expect(projectFromSearch('?project=0193-abc')).toBe('0193-abc');
    expect(projectFromSearch('')).toBe('');
  });
});
