import { describe, expect, test } from 'bun:test';

import { deviceDetail, deviceName } from './deviceLabels';

const phone = { id: '1', platform: 'ios' as const, token_tail: 'a91F', created_at: '2026-11-03T00:00:00Z' };

describe('device labels', () => {
  test('a device is called what its app named it, or its platform', () => {
    expect(deviceName({ ...phone, label: 'iPhone 15' })).toBe('iPhone 15');
    expect(deviceName(phone)).toBe('iOS');
    expect(deviceName({ ...phone, platform: 'web' })).toBe('Browser');
  });

  test('the detail line tells two devices of the same name apart', () => {
    expect(deviceDetail(phone)).toBe('iOS · token …a91F');
    expect(deviceDetail({ ...phone, token_tail: '' })).toBe('iOS');
  });
});
