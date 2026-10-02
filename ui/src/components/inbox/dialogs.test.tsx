/**
 * What each dialog asks of somebody changing a task they do not hold.
 *
 * Reassigning, editing, releasing and handing back are refused by the server
 * without a reason from anyone but the person holding the task. The inbox sent
 * none, so for an administrator acting on somebody else's task every one of
 * those buttons led to a refusal. Each dialog now asks for the reason when one
 * is needed and does not go ahead without it; the holder is asked nothing.
 *
 * The dialogs open in a portal, which a server render leaves out, so what is
 * rendered here is what each one holds.
 */
import { describe, expect, it } from 'bun:test';
import { create } from '@bufbuild/protobuf';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';
import type { ReactElement } from 'react';

import { TaskSchema } from '../../gen/entities/task_pb';
import { UserSchema } from '../../gen/entities/user_pb';
import { labelledControl, visibleText, type Control } from '../../testing/markup';
import { EditTaskForm } from './EditTaskDialog';
import { ReasonForm } from './ReasonDialog';
import { ReassignForm } from './ReassignDialog';

const html = (element: ReactElement) => renderToStaticMarkup(<MantineProvider>{element}</MantineProvider>);
const noop = () => {};

/** The button with this text on it, as its attributes. */
function button(markup: string, text: string): Control | undefined {
  for (const match of markup.matchAll(/<button([^>]*)>([\s\S]*?)<\/button>/g)) {
    if (visibleText(match[2]) !== text) continue;
    const attributes: Control = {};
    for (const attribute of match[1].matchAll(/\s([\w:-]+)(?:="([^"]*)")?/g)) attributes[attribute[1]] = attribute[2] ?? '';
    return attributes;
  }
  return undefined;
}

/** The field a required reason is written in. Its label carries the asterisk. */
const reasonField = (markup: string) => labelledControl(markup, 'Reason *');

const HINT = 'Needed because this task is not with you. People looking at this task later will see this.';

function expectsAReason(markup: string) {
  const field = reasonField(markup);
  expect(field, 'no field labelled Reason').toBeDefined();
  expect(field).toHaveProperty('required');
  // React writes the attribute in its own spelling; a browser reads either.
  expect(field?.maxLength).toBe('1000');
  expect(visibleText(markup)).toContain(HINT);
}

const heldByMallory = create(TaskSchema, {
  id: 't1', name: 'Approve the refund', type: 'userTask', status: 'claimed', priority: 50,
  assignee: create(UserSchema, { username: 'mallory' }),
});
const heldByNobody = create(TaskSchema, { id: 't2', name: 'Check the invoice', type: 'userTask', status: 'unclaimed' });
const people = [{ value: 'citra', label: 'Citra' }];

describe('reassigning a task', () => {
  const form = (viewer: string, task = heldByMallory, assignee: string | null = 'citra') =>
    html(<ReassignForm task={task} viewer={viewer} users={people} assignee={assignee} onAssigneeChange={noop} onConfirm={noop} onCancel={noop} />);

  it('asks its holder for nothing but who it goes to', () => {
    const markup = form('mallory');
    expect(labelledControl(markup, 'New Assignee')).toBeDefined();
    expect(visibleText(markup)).not.toContain('Reason');
    expect(button(markup, 'Confirm Reassignment')).not.toHaveProperty('disabled');
  });

  it('asks anybody else why, and does not go ahead until they have said', () => {
    const markup = form('ani');
    expectsAReason(markup);
    expect(button(markup, 'Confirm Reassignment')).toHaveProperty('disabled');
  });

  it('asks why of somebody reassigning a task nobody holds', () => {
    expectsAReason(form('ani', heldByNobody));
  });

  it('does not go ahead with nobody chosen', () => {
    expect(button(form('mallory', heldByMallory, null), 'Confirm Reassignment')).toHaveProperty('disabled');
  });
});

describe('editing a task', () => {
  const form = (viewer: string, task = heldByMallory) =>
    html(<EditTaskForm task={task} viewer={viewer} onChange={noop} onSave={noop} onCancel={noop} />);

  it('asks its holder for nothing more than the details', () => {
    const markup = form('mallory');
    expect(labelledControl(markup, 'Task Name')?.value).toBe('Approve the refund');
    expect(labelledControl(markup, 'Priority')).toBeDefined();
    expect(visibleText(markup)).toContain('Due Date');
    expect(visibleText(markup)).not.toContain('Reason');
    expect(button(markup, 'Save Changes')).not.toHaveProperty('disabled');
  });

  it('asks anybody else why, and does not save until they have said', () => {
    const markup = form('ani');
    expectsAReason(markup);
    expect(button(markup, 'Save Changes')).toHaveProperty('disabled');
  });

  it('asks why of somebody editing a task nobody holds', () => {
    expectsAReason(form('ani', heldByNobody));
  });
});

describe('releasing or handing back a task somebody else holds', () => {
  it('says what will happen, asks why, and does not go ahead until they have said', () => {
    const markup = html(
      <ReasonForm
        body="This takes the task from mallory and puts it back for its group to claim."
        confirmLabel="Release task"
        onConfirm={noop}
        onCancel={noop}
      />,
    );
    expect(visibleText(markup)).toContain('This takes the task from mallory and puts it back for its group to claim.');
    expectsAReason(markup);
    expect(button(markup, 'Release task')).toHaveProperty('disabled');
    expect(button(markup, 'Cancel')).not.toHaveProperty('disabled');
  });

  it('cannot be sent twice while the first is on its way', () => {
    const markup = html(<ReasonForm body="…" confirmLabel="Hand back" onConfirm={noop} onCancel={noop} busy />);
    expect(markup).toContain('data-loading');
  });
});
