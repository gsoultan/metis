import { describe, expect, it } from 'bun:test';

import { playScript } from '../testing/playScript';
import { useTaskForm } from './useTaskForm';

const definition = JSON.stringify([{ id: 'name', type: 'text', label: 'Name' }]);

describe('the task form', () => {
  // The inbox parses the form definition afresh on every render, and renders
  // again whenever a task event arrives. Comparing inputs by identity wiped
  // what the person had typed each time.
  it('keeps what is typed when it is given the same form again', () => {
    const seen = playScript(
      () => useTaskForm(JSON.parse(definition), { ...{ amount: 5 } }, () => {}),
      [(form) => form.handleChange('name', 'Ada'), () => {}, () => {}],
    );

    expect(seen.at(-1)?.values.name).toBe('Ada');
  });
});
