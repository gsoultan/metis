/**
 * The three pieces the inbox shows for a delegated task, rendered on their own.
 */
import { describe, expect, it } from 'bun:test';
import { MantineProvider } from '@mantine/core';
import { renderToStaticMarkup } from 'react-dom/server';
import type { ReactElement } from 'react';

import { visibleText } from '../../testing/markup';
import type { DelegatedTask } from '../../services/types';
import { DelegatedByYou } from './DelegatedByYou';
import { DelegationNote } from './DelegationNote';
import { HandBackButton } from './HandBackButton';

const html = (element: ReactElement) => renderToStaticMarkup(<MantineProvider>{element}</MantineProvider>);

describe('a task delegated to the reader', () => {
  it('says who delegated it', () => {
    expect(visibleText(html(<DelegationNote owner="budi" />))).toBe('Delegated to you by budi');
  });

  it('offers Hand back, named for a screen reader by the task and who it goes to', () => {
    const markup = html(<HandBackButton taskName="Approve the refund" owner="budi" onHandBack={() => {}} />);
    expect(visibleText(markup)).toBe('Hand back');
    expect(markup).toContain('aria-label="Hand Approve the refund back to budi"');
    expect(markup).toContain('<button');
    // At one press: nothing opens.
    expect(markup).not.toContain('aria-haspopup');
  });

  it('cannot be pressed twice while the first press is on its way', () => {
    const markup = html(<HandBackButton taskName="Approve the refund" owner="budi" onHandBack={() => {}} busy />);
    expect(markup).toContain('data-loading');
  });
});

/*
 * An administrator may hand back a task somebody else is working on, and the
 * server takes it only with a reason — so their button leads to a question
 * rather than doing it at one press.
 */
describe('a delegated task, for an administrator who is not its delegate', () => {
  it('offers Hand back as something that opens a dialog', () => {
    const markup = html(
      <HandBackButton taskName="Approve the refund" owner="budi" delegate="mallory" onHandBack={() => {}} />,
    );
    expect(visibleText(markup)).toBe('Hand back');
    expect(markup).toContain('aria-label="Hand Approve the refund back to budi"');
    expect(markup).toContain('aria-haspopup="dialog"');
  });
});

describe('what the reader delegated', () => {
  const tasks: DelegatedTask[] = [
    { id: 't1', name: 'Approve the refund', assignee: { username: 'mallory' }, owner: { username: 'budi' } },
    { id: 't2', name: 'Check the invoice', assignee: { username: 'citra' }, owner: { username: 'budi' } },
  ];

  it('lists each task with who has it', () => {
    const markup = html(<DelegatedByYou tasks={tasks} total={2} />);
    const text = visibleText(markup);
    expect(text).toContain('Delegated by you');
    expect(text).toContain('Approve the refund With mallory');
    expect(text).toContain('Check the invoice With citra');
    expect(text).not.toContain('more');
    expect(markup).toContain('aria-label="Delegated by you"');
  });

  it('says how many more there are than it shows', () => {
    expect(visibleText(html(<DelegatedByYou tasks={tasks} total={5} />))).toContain('and 3 more');
  });

  it('is not there at all when nothing is delegated', () => {
    expect(html(<DelegatedByYou tasks={[]} total={0} />)).not.toContain('Delegated by you');
  });
});
