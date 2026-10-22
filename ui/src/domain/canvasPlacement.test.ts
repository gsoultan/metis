import { describe, expect, it } from 'bun:test';

import { DEFAULT_STEP_WIDTH, canFollow, clearSpot, followsOn } from './canvasPlacement';

describe('a step added by a click', () => {
  it('follows a start, a task or a gateway', () => {
    expect(followsOn('startEvent', 'userTask')).toBe(true);
    expect(followsOn('userTask', 'serviceTask')).toBe(true);
    expect(followsOn('exclusiveGateway', 'endEvent')).toBe(true);
  });

  it('never follows a step that ends its path', () => {
    expect(followsOn('endEvent', 'userTask')).toBe(false);
    expect(followsOn('terminateEndEvent', 'userTask')).toBe(false);
    expect(canFollow('endEvent')).toBe(false);
  });

  it('never draws an arrow into a start, or to or from a pool', () => {
    expect(followsOn('userTask', 'startEvent')).toBe(false);
    expect(followsOn('userTask', 'pool')).toBe(false);
    expect(followsOn('pool', 'userTask')).toBe(false);
  });
});

describe('where a clicked step lands', () => {
  it('stays put on an empty spot', () => {
    expect(clearSpot({ x: 100, y: 100 }, [])).toEqual({ x: 100, y: 100 });
  });

  it('moves right, clear of a step already there', () => {
    const spot = clearSpot({ x: 100, y: 100 }, [{ position: { x: 100, y: 100 } }]);
    expect(spot.y).toBe(100);
    expect(spot.x).toBeGreaterThanOrEqual(100 + DEFAULT_STEP_WIDTH);
  });

  it('ignores steps inside a sub-process, whose positions are not the canvas’s', () => {
    expect(clearSpot({ x: 0, y: 0 }, [{ position: { x: 0, y: 0 }, parentId: 'sub' }])).toEqual({ x: 0, y: 0 });
  });
});
