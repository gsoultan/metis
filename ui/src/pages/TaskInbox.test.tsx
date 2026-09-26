/**
 * The inbox's board, rendered with its reads stood in for.
 *
 * The board shows every task in the project, not only what its reader may
 * take, so it is where the inbox could offer somebody work the server will
 * refuse them. A task nobody was named for — no assignee, no candidates — is
 * one only an administrator or an operator may take, and the board offered
 * everybody a Claim button on it.
 */
import { afterEach, describe, expect, it, mock } from 'bun:test';
import { create } from '@bufbuild/protobuf';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';

import { GroupSchema } from '../gen/entities/group_pb';
import { TaskSchema, type Task } from '../gen/entities/task_pb';
import { appStoreDouble, resetAppStore, setAppState } from '../testing/appStoreDouble';

mock.module('../store/useAppStore', () => ({ useAppStore: appStoreDouble }));
// Everything the router exports stays as it is — other test files import it —
// but navigating goes nowhere.
const router = await import('@tanstack/react-router');
mock.module('@tanstack/react-router', () => ({ ...router, useNavigate: () => () => {} }));

const board: { tasks: Task[] } = { tasks: [] };
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
    currentTasks: [],
    viewMode: 'kanban',
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

afterEach(() => resetAppStore());

describe('the board', () => {
  it('does not offer a member a task nobody was named for, and says who can take it', () => {
    const cards = cardsFor(['USER'], [nobodyNamed, offeredToFinance, shipping]);
    expect(cards.get('Approve the refund')).not.toContain('Claim');
    expect(cards.get('Approve the refund')).toContain('Nobody was named for this. An administrator or an operator can take it.');
    // Work offered to people is still offered; the server checks who.
    expect(cards.get('Check the invoice')).toContain('Claim');
    // A manual step nobody was named for is anybody's.
    expect(cards.get('Ship the parcel')).toContain('Claim');
  });

  it('offers it to an operator and to an administrator', () => {
    for (const roles of [['OPERATOR'], ['ADMIN']]) {
      const cards = cardsFor(roles, [nobodyNamed]);
      expect(cards.get('Approve the refund')).toContain('Claim');
      expect(cards.get('Approve the refund')).not.toContain('Nobody was named for this');
    }
  });
});
