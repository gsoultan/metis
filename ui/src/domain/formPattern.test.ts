import { describe, expect, it } from 'bun:test';

import { compileFieldPattern } from './formPattern';

describe('which field patterns may run', () => {
  it('runs the format checks people write', () => {
    for (const pattern of ['^[A-Z]{3}-\\d{4}$', '^\\S+@\\S+\\.\\S+$', '^(\\d{3})-(\\d{4})$', '^(?:EU|US)\\d+$', '^[(]\\d+[)]$']) {
      expect(compileFieldPattern(pattern)).not.toBeNull();
    }
  });

  it('refuses a repeated group that itself repeats', () => {
    for (const pattern of ['^(a+)+$', '(\\w*)*x', '^((ab)*c)+$', '(x+y){2,}']) {
      expect(compileFieldPattern(pattern)).toBeNull();
    }
  });

  it('refuses what does not compile, without throwing', () => {
    expect(compileFieldPattern('([A-Z')).toBeNull();
  });
});
