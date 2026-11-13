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
      return 'Installed from a Linux package. Take a backup, then install the new package and restart the service.';
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
