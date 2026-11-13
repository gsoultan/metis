import { describe, expect, it } from 'bun:test';

import { compareServerBuild, reloadOnNavigation } from './serverUpgrade';

describe('compareServerBuild', () => {
  it.each([
    ['nothing loaded yet', null, 'v0.9.0'],
    ['the probe failed, as it does while the server is stopped', 'v0.8.2', null],
    ['the same build', 'v0.8.2', 'v0.8.2'],
    ['the same build with stray whitespace', 'v0.8.2', ' v0.8.2\n'],
    ['an empty answer', 'v0.8.2', ''],
  ])('reports no change when %s', (_name, loaded, current) => {
    expect(compareServerBuild(loaded, current)).toEqual({ kind: 'same' });
  });

  it('reports an upgrade with the new version as the sidebar names it', () => {
    expect(compareServerBuild('v0.8.2', 'v0.9.0')).toEqual({ kind: 'upgraded', to: 'v0.9.0', label: 'v0.9.0' });
  });

  it('reports a rollback as a change too: the bundle is still not the server’s', () => {
    expect(compareServerBuild('v0.9.0', 'v0.8.2')).toMatchObject({ kind: 'upgraded', to: 'v0.8.2' });
  });

  it('names a build that is not a release in words', () => {
    expect(compareServerBuild('v0.8.2', 'dev')).toMatchObject({ kind: 'upgraded', label: 'development build' });
  });
});

describe('reloadOnNavigation', () => {
  it('reloads when moving to another page after an upgrade', () => {
    expect(reloadOnNavigation(true, '/inbox', '/instances')).toBe(true);
  });

  it('stays put on the same page, where a form may be half filled', () => {
    expect(reloadOnNavigation(true, '/inbox', '/inbox')).toBe(false);
  });

  it('does not reload on the first load, which has no page to come from', () => {
    expect(reloadOnNavigation(true, undefined, '/inbox')).toBe(false);
  });

  it('does nothing while the server is unchanged', () => {
    expect(reloadOnNavigation(false, '/inbox', '/instances')).toBe(false);
  });
});
