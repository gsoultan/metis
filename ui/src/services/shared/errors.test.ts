import { describe, expect, it } from 'bun:test';
import { Code, ConnectError } from '@connectrpc/connect';

import { errorDetail, errorMessage, failureMessage } from './errors';

/*
 * The server words a refusal as "<class>: <why>" (internal/pkg/apierr), so a
 * transport can tell a 403 from a 400 from a 404. The class is for the
 * transport; the person reading the notification needs the why. "forbidden:
 * only the person holding this task, or an administrator, can hand it to
 * someone else" read as if the app were shouting a status code at them.
 */
describe('a refusal shown to a person', () => {
  it('drops each class the server puts in front of a refusal', () => {
    expect(errorMessage(new Error('forbidden: only the person holding this task, or an administrator, can hand it to someone else'))).toBe(
      'only the person holding this task, or an administrator, can hand it to someone else',
    );
    expect(errorMessage(new Error('invalid argument: say who the task is delegated to'))).toBe(
      'say who the task is delegated to',
    );
    expect(errorMessage(new Error('not found: no such task'))).toBe('no such task');
  });

  it('leaves a message that merely contains one of the words alone', () => {
    const sentences = [
      'The address is forbidden: it resolves to a private network',
      'An invalid argument: the date has no year',
      'The file was not found: check the name',
      'Forbidden: that is how a person writes it, not how the server classes it',
      'forbidden',
      'not found',
      'forbidden:no space after the colon is not the server’s shape',
    ];
    for (const sentence of sentences) {
      expect(errorMessage(new Error(sentence))).toBe(sentence);
    }
  });

  it('keeps the whole message rather than showing nothing after the class', () => {
    expect(errorMessage(new Error('forbidden: '))).toBe('forbidden: ');
  });

  it('drops the class however the refusal was thrown', () => {
    expect(errorMessage('not found: no such webhook')).toBe('no such webhook');
    expect(errorMessage({ message: 'invalid argument: the name is empty' })).toBe('the name is empty');
    // A Connect call refused by a role gate arrives as "[unknown] forbidden: …".
    expect(errorMessage(new ConnectError('forbidden: this needs the ADMIN role', Code.Unknown))).toBe(
      'this needs the ADMIN role',
    );
  });

  it('still says something when there is nothing to say', () => {
    expect(errorMessage(undefined, 'It was not saved.')).toBe('It was not saved.');
    expect(errorMessage(new Error(''), 'It was not saved.')).toBe('It was not saved.');
  });

  it('builds a notification from what was being done and the why', () => {
    expect(failureMessage('Failed to save project', new Error('forbidden: this needs the ADMIN role'))).toBe(
      'Failed to save project: this needs the ADMIN role',
    );
  });
});

/*
 * Where the message is logged, or kept for whoever a problem is escalated to,
 * the class is information: it is how a 403 is told from a 404. That text is
 * left whole, and so is the error itself.
 */
describe('a refusal kept for a log', () => {
  it('keeps the class', () => {
    const error = new Error('forbidden: only the person holding this task can hand it on');
    expect(errorDetail(error)).toBe('forbidden: only the person holding this task can hand it on');
    errorMessage(error);
    expect(error.message).toBe('forbidden: only the person holding this task can hand it on');
  });

  it('keeps a Connect error exactly as it was raised', () => {
    expect(errorDetail(new ConnectError('forbidden: this needs the ADMIN role', Code.Unknown))).toBe(
      '[unknown] forbidden: this needs the ADMIN role',
    );
  });
});
