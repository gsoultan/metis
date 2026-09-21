/**
 * Which build is serving this page.
 *
 * "Which version is running" is the first question of any incident and the last
 * question of any deploy, and the UI could not answer it: the bundle is
 * embedded in the Go binary, so the two always ship together, and yet nothing
 * on screen said which one you were looking at.
 *
 * The answer is taken from the server rather than stamped into the bundle at
 * build time. Both would work, but only one of them cannot drift: a version
 * baked into JavaScript is whatever the machine that ran `vite build` believed,
 * while `/healthz` reports the string the running binary was linked with — the
 * same one in its logs, its traces and its container tag.
 *
 * What arrives is one of three things, and they mean different things to the
 * person reading them:
 *
 *   - `dev`            — nobody stamped this build. Someone's laptop.
 *   - `v1.4.2`         — a released tag. There is a page on GitHub about it.
 *   - `v1.4.2-3-gabc…` — `git describe`: three commits past a tag. Not a
 *                        release, and linking it to `v1.4.2` would claim it was.
 */

/**
 * Where a release tag can be looked up.
 *
 * Hardcoded rather than read from the server: the repository a build came from
 * is a property of this source tree, and a fork that changes it changes this
 * line in the same commit that changes its remote.
 */
const REPOSITORY = 'gsoultan/metis';

/** `v1.4.2`, `v1.4.2-rc.1`, `v2.0.0+build.5`. */
const RELEASE_TAG = /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.]*)?(?:\+[0-9A-Za-z.]+)?$/;

/**
 * `git describe` output: a tag, how far past it, and the commit.
 *
 * Checked before RELEASE_TAG because `v1.4.2-3-gabc1234` also looks like a tag
 * with a prerelease suffix, and calling it one would publish a link to a
 * release page for code that is not in that release.
 */
const DESCRIBED = /^(v\d+\.\d+\.\d+(?:-[0-9A-Za-z][0-9A-Za-z.]*)?)-(\d+)-g[0-9a-f]{7,40}(-dirty)?$/;

export interface BuildIdentity {
  /** Exactly what the server said, for copying into an issue. */
  raw: string;
  /** What to put on screen. */
  label: string;
  /**
   * The same thing in a 60px rail.
   *
   * The sidebar collapses, and "development build" does not fit — but dropping
   * the version entirely there means the one screen element that answers "which
   * build is this" disappears for anybody who prefers the narrow rail.
   */
  shortLabel: string;
  /** True only for a build linked from a release tag. */
  released: boolean;
  /** The GitHub release page, when this build has one. */
  releaseUrl: string | null;
}

export function readBuildIdentity(raw: string): BuildIdentity {
  const version = raw.trim();

  if (version === '' || version === 'dev') {
    return { raw: version, label: 'development build', shortLabel: 'dev', released: false, releaseUrl: null };
  }

  const described = DESCRIBED.exec(version);
  if (described !== null) {
    const [, tag, ahead, dirty] = described;
    const commits = Number(ahead) === 1 ? '1 commit' : `${ahead} commits`;
    return {
      raw: version,
      // Says what it is in words, because `v1.4.2-3-gabc1234` reads as a
      // version number to everyone who has not used `git describe`.
      label: `${tag} + ${commits}${dirty === undefined ? '' : ', uncommitted'}`,
      // The plus is the point: it says "past this tag" in one character.
      shortLabel: `${tag}+`,
      released: false,
      releaseUrl: null,
    };
  }

  if (RELEASE_TAG.test(version)) {
    return {
      raw: version,
      label: version,
      shortLabel: version,
      released: true,
      releaseUrl: `https://github.com/${REPOSITORY}/releases/tag/${encodeURIComponent(version)}`,
    };
  }

  // Anything else is shown as-is. A build stamped with something this file does
  // not recognise is still the answer to "which one is running", and inventing
  // a nicer label for it would hide the only fact there is.
  return { raw: version, label: version, shortLabel: version, released: false, releaseUrl: null };
}
