import { createElement, useState } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

/**
 * Plays steps against a hook, one per render, inside a single server render.
 *
 * There is no browser here, so nothing can type or click. But a component may
 * update its own state while it renders, and React then runs it again at once
 * with the update applied. So a component that takes one step each time it
 * runs carries a hook's state through a whole sequence, as a person's
 * keystrokes would. The hook's value is recorded before each step and once
 * more after the last.
 *
 * React stops a component that updates itself more than 25 times in one
 * render, so a script is kept short.
 */
export function playScript<T>(useSubject: () => T, steps: ReadonlyArray<(subject: T) => void>): T[] {
  const seen: T[] = [];

  function Player({ record }: { record: (step: number, subject: T) => void }) {
    const subject = useSubject();
    const [next, setNext] = useState(0);
    record(next, subject);
    if (next < steps.length) {
      steps[next](subject);
      setNext(next + 1);
    }
    return null;
  }

  renderToStaticMarkup(createElement(Player, { record: (step: number, subject: T) => { seen[step] = subject; } }));
  return seen;
}
