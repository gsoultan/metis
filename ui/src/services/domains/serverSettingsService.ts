import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** Where the value a setting has comes from. */
export type SettingSource = "environment" | "saved" | "default";

/**
 * A server setting the Settings page may change. Mirrors
 * serversetting.SettingView on the server.
 *
 * `value` is always empty for a secret: the server never returns one, and
 * `is_set` says whether one is set.
 */
export interface ServerSetting {
  name: string;
  group: string;
  label: string;
  help: string;
  default: string;
  kind: string;
  secret: boolean;
  applies: "restart" | "live";
  source: SettingSource;
  value: string;
  is_set: boolean;
  pending_restart: boolean;
  updated_at?: string;
  updated_by?: string;
}

/** A setting only the server's environment sets, shown and never changed. */
export interface EnvironmentOnlySetting {
  name: string;
  class: "bootstrap" | "guardrail";
  reason: string;
  secret: boolean;
  is_set: boolean;
  value: string;
}

/** One change in a setting's history. `value` is null for a clearing. */
export interface ServerSettingChange {
  value: string | null;
  reason: string;
  set_by: string;
  set_at: string;
  cleared: boolean;
}

interface ListResponse {
  settings?: ServerSetting[];
  environment_only?: EnvironmentOnlySetting[];
  err?: string;
}

/**
 * The server's own settings: a mail server, a retention period, a worker
 * count. Every call is a platform administrator's; the server refuses anybody
 * else.
 */
export const serverSettingsService = {
  async listServerSettings(signal?: AbortSignal) {
    const response = raiseIfRefused(await requestJSON<ListResponse>("/settings/server", { signal }));
    return { settings: response.settings ?? [], environmentOnly: response.environment_only ?? [] };
  },

  /** Saves a value, or with null returns the setting to the environment's value or the default. */
  async changeServerSetting(name: string, value: string | null, reason: string, signal?: AbortSignal) {
    const response = await requestJSON<{ setting?: ServerSetting; err?: string }>(
      `/settings/server/${encodeURIComponent(name)}`,
      { method: "PUT", body: { value, reason }, signal },
    );
    return raiseIfRefused(response).setting;
  },

  async serverSettingHistory(name: string, signal?: AbortSignal) {
    const response = await requestJSON<{ changes?: ServerSettingChange[]; err?: string }>(
      `/settings/server/${encodeURIComponent(name)}/history`,
      { signal },
    );
    return raiseIfRefused(response).changes ?? [];
  },
};
