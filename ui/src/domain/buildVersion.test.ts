import { describe, expect, it } from 'bun:test';
import { readBuildIdentity } from './buildVersion';

describe('readBuildIdentity', () => {
  it('says a laptop build is a laptop build rather than inventing a number', () => {
    expect(readBuildIdentity('dev')).toEqual({
      raw: 'dev',
      label: 'development build',
      shortLabel: 'dev',
      released: false,
      releaseUrl: null,
    });
  });

  it('treats an absent version the same way', () => {
    expect(readBuildIdentity('  ')).toMatchObject({ label: 'development build', released: false });
  });

  it('links a released tag to its page on GitHub', () => {
    expect(readBuildIdentity('v1.4.2')).toEqual({
      raw: 'v1.4.2',
      label: 'v1.4.2',
      shortLabel: 'v1.4.2',
      released: true,
      releaseUrl: 'https://github.com/gsoultan/metis/releases/tag/v1.4.2',
    });
  });

  it('links a pre-release tag too, which has its own page', () => {
    const identity = readBuildIdentity('v2.0.0-rc.1');
    expect(identity.released).toBe(true);
    expect(identity.releaseUrl).toBe('https://github.com/gsoultan/metis/releases/tag/v2.0.0-rc.1');
  });

  it('accepts build metadata after a plus', () => {
    expect(readBuildIdentity('v1.4.2+build.5').released).toBe(true);
  });

  it('refuses to call a describe stamp a release, which would link to code it does not contain', () => {
    const identity = readBuildIdentity('v1.4.2-3-gabc1234');
    expect(identity.released).toBe(false);
    expect(identity.releaseUrl).toBeNull();
    expect(identity.label).toBe('v1.4.2 + 3 commits');
  });

  it('counts one commit in the singular, because the plural is the thing people notice', () => {
    expect(readBuildIdentity('v1.4.2-1-gabc1234').label).toBe('v1.4.2 + 1 commit');
  });

  it('describes a dirty tree as dirty, since those bytes are in no commit at all', () => {
    expect(readBuildIdentity('v1.4.2-2-gabc1234-dirty').label).toBe('v1.4.2 + 2 commits, uncommitted');
  });

  it('describes past a pre-release tag as well', () => {
    expect(readBuildIdentity('v2.0.0-rc.1-4-gdeadbee').label).toBe('v2.0.0-rc.1 + 4 commits');
  });

  it('shows an unrecognised stamp as-is rather than hiding the only fact there is', () => {
    expect(readBuildIdentity('nightly-2026-09-21')).toEqual({
      raw: 'nightly-2026-09-21',
      label: 'nightly-2026-09-21',
      shortLabel: 'nightly-2026-09-21',
      released: false,
      releaseUrl: null,
    });
  });

  it('has a short form for the collapsed rail, where the long one does not fit', () => {
    expect(readBuildIdentity('dev').shortLabel).toBe('dev');
    expect(readBuildIdentity('v1.4.2').shortLabel).toBe('v1.4.2');
    expect(readBuildIdentity('v1.4.2-3-gabc1234').shortLabel).toBe('v1.4.2+');
  });

  it('refuses a tag that is not a version, so a branch name never becomes a link', () => {
    expect(readBuildIdentity('main').releaseUrl).toBeNull();
    expect(readBuildIdentity('v1.4').releaseUrl).toBeNull();
  });
});
