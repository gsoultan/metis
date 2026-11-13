/**
 * What the Upgrades page says about a newer release, kept apart from React so
 * the wording rules can be tested alone.
 */
import type { InstallKind, UpgradeMigration, UpgradeStatus } from '../services/domains/upgradeService';

/** The banner and the sidebar offer an upgrade only when there is one to offer. */
export function upgradeAvailable(status: UpgradeStatus | undefined): boolean {
  return status !== undefined && status.enabled && status.newest !== undefined;
}

/** "2 migrations", "No migrations", or null when the release did not say. */
export function migrationsLabel(status: UpgradeStatus): string | null {
  if (status.newest?.manifest === undefined) {
    return null;
  }
  const count = (status.pending ?? []).length;
  if (count === 0) {
    return 'No migrations';
  }
  return count === 1 ? '1 migration' : `${count} migrations`;
}

/** One line per migration, in the order they run. */
export function migrationLines(pending: UpgradeMigration[] | null): string[] {
  return (pending ?? []).map((m) => `Migration ${m.version}: ${m.name}`);
}

/** How this installation is upgraded, in one sentence. */
export function installGuidance(install: InstallKind): string {
  switch (install) {
    case 'package':
      return 'Installed from a Linux package. Upgrade here, or from a shell with sudo metis upgrade --to <release>: either way the release is verified and a backup taken before anything stops.';
    case 'container':
      return 'Running in a container. Take a backup, then move your deployment to the new image; Docker, Compose or Kubernetes owns the image, not Metis.';
    case 'binary':
      return 'Installed as a binary. Take a backup, then replace the binary with the one from the release and restart it.';
  }
}

/** Says when the last check ran, or that none has. */
export function checkedLabel(checkedAt: string | undefined, now: Date): string {
  if (checkedAt === undefined || checkedAt === '') {
    return 'Not checked yet';
  }
  const minutes = Math.max(0, Math.round((now.getTime() - new Date(checkedAt).getTime()) / 60_000));
  if (minutes < 1) {
    return 'Just now';
  }
  if (minutes < 60) {
    return minutes === 1 ? '1 minute ago' : `${minutes} minutes ago`;
  }
  const hours = Math.round(minutes / 60);
  return hours === 1 ? '1 hour ago' : `${hours} hours ago`;
}

/** Whether an upgrade is waiting for the helper or running, so the page keeps asking. */
export function upgradeInFlight(status: UpgradeStatus | undefined): boolean {
  return status?.requested !== undefined || status?.progress?.outcome === 'running';
}

export interface ProgressView {
  color: 'blue' | 'green' | 'orange' | 'red';
  title: string;
  body: string;
}

/**
 * What the page says about the last upgrade. A finished upgrade whose release
 * is the one running needs no words: the page already shows it running.
 */
export function progressView(status: UpgradeStatus): ProgressView | null {
  if (status.requested !== undefined) {
    return {
      color: 'blue',
      title: `Upgrade to ${status.requested.to} asked for`,
      body: `${status.requested.requested_by} asked for it. The upgrade service on this host picks it up within seconds, verifies the release and takes a backup before anything stops.`,
    };
  }
  const progress = status.progress;
  if (progress === undefined) {
    return null;
  }
  const detail = progress.detail ?? '';
  switch (progress.outcome) {
    case 'running':
      return {
        color: 'blue',
        title: `Upgrading from ${progress.from} to ${progress.to}`,
        body: `Now: ${progress.step}. Metis restarts during the upgrade; this page reloads into the new version when it is back.`,
      };
    case 'done':
      return progress.to === status.current
        ? null
        : { color: 'green', title: `Upgraded to ${progress.to}`, body: detail };
    case 'refused':
      return {
        color: 'orange',
        title: `The upgrade to ${progress.to} did not start`,
        body: `Nothing on this host was changed. ${detail}`.trim(),
      };
    case 'rolled_back':
      return {
        color: 'orange',
        title: `${progress.to} did not come up, so ${progress.from} is serving again`,
        body: `No migration had run. ${detail}`.trim(),
      };
    case 'needs_restore':
      return {
        color: 'red',
        title: `${progress.to} did not come up after migrating the database`,
        body: `Metis was stopped there, because ${progress.from} cannot read the newer schema. Restore ${progress.backup ?? 'the backup taken before the upgrade'} and reinstall ${progress.from}; see docs/upgrading.md. ${detail}`.trim(),
      };
  }
}
