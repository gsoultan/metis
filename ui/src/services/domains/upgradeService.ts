import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** One numbered schema change a release applies. Mirrors releasemanifest.Migration. */
export interface UpgradeMigration {
  version: number;
  name: string;
}

/** What a release says it does to a database. Mirrors releasemanifest.Manifest. */
export interface ReleaseManifest {
  release: string;
  schema_version: number;
  oldest_schema: number;
  via_release: string;
  migrations: UpgradeMigration[];
}

/** A published release newer than this installation. Mirrors releasecheck.Release. */
export interface AvailableRelease {
  tag: string;
  name: string;
  published_at: string;
  url: string;
  notes: string;
  prerelease: boolean;
  manifest?: ReleaseManifest;
  manifest_error?: string;
}

export type InstallKind = "package" | "container" | "binary";

/** What the Upgrades page shows. Mirrors contracts.UpgradeStatus. */
export interface UpgradeStatus {
  current: string;
  current_is_release: boolean;
  newest?: AvailableRelease;
  checked_at?: string;
  error?: string;
  enabled: boolean;
  channel: "stable" | "prerelease";
  install: InstallKind;
  schema_version: number;
  pending: UpgradeMigration[] | null;
  reachable: boolean;
}

interface StatusResponse {
  status?: UpgradeStatus;
  err?: string;
}

function statusOf(response: StatusResponse): UpgradeStatus {
  const status = raiseIfRefused(response).status;
  if (status === undefined) {
    throw new Error("The server answered without an upgrade status");
  }
  return status;
}

/**
 * Newer releases of Metis. Every call is a platform administrator's; the
 * server refuses anybody else.
 */
export const upgradeService = {
  async getStatus(signal?: AbortSignal) {
    return statusOf(await requestJSON<StatusResponse>("/upgrades", { signal }));
  },

  /** Asks GitHub now. The server asks at most once a minute, whatever is pressed. */
  async checkNow(signal?: AbortSignal) {
    return statusOf(await requestJSON<StatusResponse>("/upgrades/check", { method: "POST", signal }));
  },
};
