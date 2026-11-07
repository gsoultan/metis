import { describe, expect, it } from 'bun:test';
import { activationReady, heldWorkToSend } from './participantActivation';

describe('deactivating somebody who holds work', () => {
  it('needs nothing when they hold nothing or their work is kept', () => {
    expect(activationReady({ do: 'hand_over', to: null, reason: '' }, 0)).toBe(true);
    expect(activationReady({ do: 'keep', to: null, reason: '' }, 3)).toBe(true);
    expect(heldWorkToSend({ do: 'keep', to: 'dana', reason: 'x' }, 3)).toBeUndefined();
    expect(heldWorkToSend({ do: 'release', to: null, reason: 'x' }, 0)).toBeUndefined();
  });

  it('needs a reason to move it, and somebody to hand it to', () => {
    expect(activationReady({ do: 'release', to: null, reason: '  ' }, 2)).toBe(false);
    expect(activationReady({ do: 'release', to: null, reason: 'Left' }, 2)).toBe(true);
    expect(activationReady({ do: 'hand_over', to: null, reason: 'Left' }, 2)).toBe(false);
    expect(activationReady({ do: 'hand_over', to: 'dana', reason: 'Left' }, 2)).toBe(true);
  });

  it('sends the hand-over with its target and the trimmed reason', () => {
    expect(heldWorkToSend({ do: 'hand_over', to: 'dana', reason: ' Left ' }, 2)).toEqual({ do: 'hand_over', to: 'dana', reason: 'Left' });
    expect(heldWorkToSend({ do: 'release', to: 'dana', reason: 'Left' }, 2)).toEqual({ do: 'release', reason: 'Left' });
  });
});
