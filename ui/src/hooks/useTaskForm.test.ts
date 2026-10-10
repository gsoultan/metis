import { describe, expect, it } from 'bun:test';
import type { FormEvent } from 'react';

import type { FormField } from '../components/FormBuilder';
import { MAX_PATTERN_LENGTH } from '../domain/formPattern';
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

describe('a field pattern from the definition', () => {
  const submitEvent = { preventDefault: () => {} } as FormEvent;

  /** Types `typed` into a field with `pattern`, presses Submit, and says whether it went. */
  function submitWith(pattern: string, typed: string) {
    const fields = [{ id: 'code', type: 'text', label: 'Code', validation: { pattern, message: 'Wrong format' } }] as FormField[];
    let submitted = false;
    const seen = playScript(
      () => useTaskForm(fields, {}, () => { submitted = true; }),
      [(form) => form.handleChange('code', typed), (form) => form.handleSubmit(submitEvent)],
    );
    return { submitted, error: seen.at(-1)?.errors.code };
  }

  it('still checks the field when it is a sound pattern', () => {
    expect(submitWith('^[A-Z]{3}$', 'abc')).toEqual({ submitted: false, error: 'Wrong format' });
    expect(submitWith('^[A-Z]{3}$', 'ABC').submitted).toBe(true);
  });

  it('does not stop Submit working when it does not compile', () => {
    expect(submitWith('([A-Z', 'anything').submitted).toBe(true);
  });

  it('is not run when it would backtrack without end', () => {
    const started = performance.now();
    expect(submitWith('^(a+)+$', `${'a'.repeat(40)}!`).submitted).toBe(true);
    expect(performance.now() - started).toBeLessThan(1_000);
  });

  it('is not run when it is far longer than any format check', () => {
    expect(submitWith(`^${'a'.repeat(MAX_PATTERN_LENGTH)}$`, 'b').submitted).toBe(true);
  });
});
