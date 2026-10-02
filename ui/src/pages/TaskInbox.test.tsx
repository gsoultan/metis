/**
 * The inbox's board, rendered with its reads stood in for.
 *
 * The board shows every task in the project, not only what its reader may
 * take, so it is where the inbox could offer somebody work the server will
 * refuse them. A task nobody was named for — no assignee, no candidates — is
 * one only an administrator or an operator may take, and the board offered
 * everybody a Claim button on it. A manual step is held to the same rule as a
 * user step, and the board offered everybody Claim on one that named nobody.
 */
import { afterEach, describe, expect, it, mock } from 'bun:test';
import { create } from '@bufbuild/protobuf';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { GroupSchema } from '../gen/entities/group_pb';
import { TaskSchema, type Task } from '../gen/entities/task_pb';
import { UserSchema } from '../gen/entities/user_pb';
import type { DelegatedTask } from '../services/types';
import { appStoreDouble, resetAppStore, setAppState } from '../testing/appStoreDouble';

mock.module('../store/useAppStore', () => ({ useAppStore: appStoreDouble }));
// Everything the router exports stays as it is — other test files import it —
// but navigating goes nowhere.
const router = await import('@tanstack/react-router');
mock.module('@tanstack/react-router', () => ({ ...router, useNavigate: () => () => {} }));

const board: { tasks: Task[] } = { tasks: [] };
// What the table shows, for the tests that look at it rather than the board.
const view: { mode: 'kanban' | 'table'; rows: Task[]; delegated: DelegatedTask[] } = { mode: 'kanban', rows: [], delegated: [] };
const noop = () => {};
const inbox = await import('../hooks/useTaskInbox');
mock.module('../hooks/useTaskInbox', () => ({
  ...inbox,
  useTaskInbox: () => ({
    currentUser: 'mallory',
    activeTab: 'assigned',
    setActiveTab: noop,
    searchQuery: '',
    setSearchQuery: noop,
    selectedTask: null,
    setSelectedTask: noop,
    editingTask: null,
    setEditingTask: noop,
    handleSort: noop,
    availableUsers: [],
    reassignModalOpened: false,
    setReassignModalOpened: noop,
    taskToReassign: null,
    setTaskToReassign: noop,
    newAssignee: null,
    setNewAssignee: noop,
    assignedLoading: false,
    candidateLoading: false,
    assignedCount: 0,
    candidateCount: 0,
    setPage: noop,
    pageSize: 25,
    setPageSize: noop,
    activePageInfo: undefined,
    currentTasks: view.rows,
    viewMode: view.mode,
    setViewMode: noop,
    selectedTaskIds: [],
    bulkInFlight: false,
    setSelectedTaskIds: noop,
    toggleSelection: noop,
    handleBulkClaim: noop,
    handleBulkUnclaim: noop,
    allTasks: board.tasks,
    allTasksLoading: false,
    handleClaim: noop,
    handleUnclaim: noop,
    handleComplete: noop,
    handleAssign: noop,
    assigning: false,
    handleResolve: noop,
    resolvingTaskId: null,
    delegatedByMe: view.delegated,
    delegatedTotal: view.delegated.length,
    reasonRequest: null,
    setReasonRequest: noop,
    reasonInFlight: false,
    updateTaskMutation: { mutate: noop, isPending: false },
  }),
}));

const { TaskInbox } = await import('./TaskInbox');

const unclaimed = (name: string, fields: Partial<Task> = {}) =>
  create(TaskSchema, { id: name, name, type: 'userTask', status: 'unclaimed', ...fields });

/** Each card on the board, as its text, by the task's name. */
function cardsFor(roles: string[], tasks: Task[]): Map<string, string> {
  board.tasks.splice(0, board.tasks.length, ...tasks);
  setAppState({ user: { id: 'u1', name: 'Mallory', displayName: 'Mallory', organization: 'Acme', username: 'mallory', role: roles.join(','), roles } });
  const html = renderToStaticMarkup(
    <MantineProvider>
      <TaskInbox />
    </MantineProvider>,
  );
  const text = html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
  const cards = new Map<string, string>();
  for (const task of tasks) {
    const start = text.indexOf(` ${task.name} `);
    const next = tasks.map((other) => text.indexOf(` ${other.name} `)).filter((at) => at > start).sort((a, b) => a - b)[0];
    cards.set(task.name, text.slice(start, next ?? text.length));
  }
  return cards;
}

const nobodyNamed = unclaimed('Approve the refund');
const offeredToFinance = unclaimed('Check the invoice', { candidateGroups: [create(GroupSchema, { name: 'finance' })] });
const shipping = unclaimed('Ship the parcel', { type: 'manualTask' });
const shippingByTheWarehouse = unclaimed('Pack the parcel', {
  type: 'manualTask',
  candidateGroups: [create(GroupSchema, { name: 'warehouse' })],
});

const NOBODY_NAMED_NOTE = 'Nobody was named for this. An administrator or an operator can take it.';

afterEach(() => {
  resetAppStore();
  view.mode = 'kanban';
  view.rows = [];
  view.delegated = [];
});

describe('the board', () => {
  it('does not offer a member a task nobody was named for, and says who can take it', () => {
    const cards = cardsFor(['USER'], [nobodyNamed, offeredToFinance, shipping, shippingByTheWarehouse]);
    expect(cards.get('Approve the refund')).not.toContain('Claim');
    expect(cards.get('Approve the refund')).toContain(NOBODY_NAMED_NOTE);
    // Work offered to people is still offered; the server checks who.
    expect(cards.get('Check the invoice')).toContain('Claim');
    // A manual step nobody was named for is no more anybody's than a user step.
    expect(cards.get('Ship the parcel')).not.toContain('Claim');
    expect(cards.get('Ship the parcel')).not.toContain('Done');
    expect(cards.get('Ship the parcel')).toContain(NOBODY_NAMED_NOTE);
    expect(cards.get('Pack the parcel')).toContain('Claim');
  });

  it('offers it to an operator and to an administrator', () => {
    for (const roles of [['OPERATOR'], ['ADMIN']]) {
      const cards = cardsFor(roles, [nobodyNamed, shipping]);
      for (const name of ['Approve the refund', 'Ship the parcel']) {
        expect(cards.get(name)).toContain('Claim');
        expect(cards.get(name)).not.toContain('Nobody was named for this');
      }
    }
  });
});

/*
 * A delegated task is its owner's to complete, and the server refuses the
 * delegate. The table offered Complete on it anyway, said nothing about whose
 * it was, and an owner could not see the task they had delegated at all.
 */
describe('the table, for a delegated task', () => {
  const render = (username = 'mallory', roles = ['USER']) => {
    setAppState({ user: { id: 'u1', name: 'Mallory', displayName: 'Mallory', organization: 'Acme', username, role: roles.join(','), roles } });
    const html = renderToStaticMarkup(
      <MantineProvider>
        <TaskInbox />
      </MantineProvider>,
    );
    const text = html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');
    // What a row says and offers: everything from the task's name on. The
    // column headings and the tabs above it are not the row's.
    const row = (name: string) => text.slice(text.indexOf(` ${name} `));
    return { html, text, row };
  };

  const delegatedToMallory = create(TaskSchema, {
    id: 't1', name: 'Approve the refund', type: 'userTask', status: 'delegated', delegationState: 'pending',
    assignee: create(UserSchema, { username: 'mallory' }),
    owner: create(UserSchema, { username: 'budi' }),
  });

  it('tells the delegate whose it is and offers Hand back, not Complete', () => {
    view.mode = 'table';
    view.rows = [delegatedToMallory];
    const { html, row } = render();
    expect(row('Approve the refund')).toContain('Delegated to you by budi');
    expect(row('Approve the refund')).toContain('Hand back');
    expect(html).toContain('aria-label="Hand Approve the refund back to budi"');
    expect(row('Approve the refund')).not.toContain('Complete');
    // Releasing it would be refused as well: it goes back, not to the group.
    expect(html).not.toContain('aria-label="Release Approve the refund back to its group"');
  });

  it('still offers Complete on a task that is simply the reader’s', () => {
    view.mode = 'table';
    view.rows = [create(TaskSchema, {
      id: 't2', name: 'Check the invoice', type: 'userTask', status: 'claimed',
      assignee: create(UserSchema, { username: 'mallory' }),
    })];
    const { html, text } = render();
    expect(text).toContain('Complete');
    expect(text).not.toContain('Hand back');
    expect(text).not.toContain('Delegated to you by');
    expect(html).toContain('aria-label="Release Check the invoice back to its group"');
  });

  /*
   * The server sends a pending mark only for a task waiting to be handed back.
   * A status of "delegated" with no mark is not one, and is its holder's.
   */
  it('goes by the delegation the server sent, never by the status alone', () => {
    view.mode = 'table';
    view.rows = [create(TaskSchema, {
      id: 't4', name: 'File the claim', type: 'userTask', status: 'delegated',
      assignee: create(UserSchema, { username: 'mallory' }),
    })];
    const { row } = render();
    expect(row('File the claim')).toContain('Complete');
    expect(row('File the claim')).not.toContain('Hand back');
  });

  it('offers an administrator who is not the delegate Hand back through a dialog, and does not call the task theirs', () => {
    view.mode = 'table';
    view.rows = [delegatedToMallory];
    const { html, row } = render('ani', ['ADMIN']);
    expect(row('Approve the refund')).toContain('Hand back');
    expect(html).toMatch(/<button[^>]*aria-haspopup="dialog"[^>]*aria-label="Hand Approve the refund back to budi"|<button[^>]*aria-label="Hand Approve the refund back to budi"[^>]*aria-haspopup="dialog"/);
    expect(row('Approve the refund')).not.toContain('Delegated to you by');
    expect(row('Approve the refund')).not.toContain('Complete');
  });

  it('offers somebody who can neither hand it back nor complete it nothing but where it is', () => {
    view.mode = 'table';
    view.rows = [delegatedToMallory];
    const { row } = render('citra');
    expect(row('Approve the refund')).toContain('With mallory');
    expect(row('Approve the refund')).not.toContain('Hand back');
    expect(row('Approve the refund')).not.toContain('Complete');
  });

  it('shows an owner what is with their delegate', () => {
    view.mode = 'table';
    view.delegated = [{ id: 't3', name: 'Sign the contract', assignee: { username: 'citra' }, owner: { username: 'mallory' } }];
    const { text } = render();
    expect(text).toContain('Delegated by you');
    expect(text).toContain('Sign the contract With citra');
  });
});
