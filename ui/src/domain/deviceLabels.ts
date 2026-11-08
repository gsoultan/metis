import type { PersonDevice } from '../services/domains/peopleService';

const PLATFORM_NAMES: Record<PersonDevice['platform'], string> = {
  android: 'Android',
  ios: 'iOS',
  web: 'Browser',
};

/** What a list calls a device: the name its app gave it, or its platform. */
export function deviceName(device: PersonDevice): string {
  return device.label || PLATFORM_NAMES[device.platform] || device.platform;
}

/** The second line under a device: its platform and the end of its token. */
export function deviceDetail(device: PersonDevice): string {
  const platform = PLATFORM_NAMES[device.platform] || device.platform;
  return device.token_tail ? `${platform} · token …${device.token_tail}` : platform;
}
