/**
 * Which build is serving this page, in the corner of the sidebar.
 *
 * Small, quiet, and always there. It is not information anybody needs while
 * working, and it is the first thing asked for when something is wrong — so it
 * earns a line in the footer rather than a place in the interface.
 *
 * A released build links to its page on GitHub, which is where the answer to
 * "what changed" lives. A build that is not from a release does not link
 * anywhere, because there is no page to send anybody to and a link to the tag
 * it happens to be *after* would describe code this build does not contain.
 */
import { Anchor, Text, Tooltip } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';

import { readBuildIdentity } from '../../domain/buildVersion';
import { buildService } from '../../services/domains/buildService';
import classes from './Sidebar.module.css';

export function BuildVersion({ collapsed }: { collapsed: boolean }) {
  const { data } = useQuery({
    queryKey: ['build-version'],
    queryFn: ({ signal }) => buildService.getBuildVersion(signal),
    // The binary cannot change under a page that is already open: this bundle
    // is embedded in it, so a new version is a new bundle and a reload.
    staleTime: Infinity,
    gcTime: Infinity,
    // One retry. A probe that does not answer is worth one more try and not
    // worth a retry storm in the corner of a sidebar.
    retry: 1,
  });

  // Nothing at all until the answer arrives, rather than a skeleton: a
  // placeholder flickering in the footer would draw the eye to the one element
  // on the page that should not.
  if (data === undefined) {
    return null;
  }

  const build = readBuildIdentity(data);
  const shown = collapsed ? build.shortLabel : build.label;
  const description = build.released
    ? `Metis ${build.raw} — open the release notes`
    : `Metis ${build.raw} — not from a release tag`;

  return (
    <Tooltip label={description} position="right" withArrow openDelay={300}>
      {build.releaseUrl === null ? (
        <Text className={classes.buildVersion} component="span" aria-label={description}>
          {shown}
        </Text>
      ) : (
        <Anchor
          className={classes.buildVersion}
          href={build.releaseUrl}
          target="_blank"
          rel="noreferrer noopener"
          aria-label={description}
        >
          {shown}
        </Anchor>
      )}
    </Tooltip>
  );
}
