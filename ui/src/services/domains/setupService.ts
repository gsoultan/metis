import { requestJSON } from "../shared/rest";
import type { SetupRequest } from "../types";
import type { SetupPeople } from "../../domain/setupWizard";
import { raiseIfRefused } from "../raise";

/**
 * Mirrors contracts.SetupStatus on the server, which serialises as
 * `{"status":{"is_initialized":false}}`.
 *
 * This was previously declared as the string union
 * 'not_configured' | 'configured' | 'ready', which no endpoint has ever
 * returned. Every consumer already reads `status.is_initialized` — the three
 * route guards that decide whether to show the setup wizard, the login page or
 * the app — so the declaration contradicted both the wire format and its own
 * callers. Nothing caught it because the processService facade was typed
 * `any`, which erased the mismatch at every call site.
 */
export interface SetupStatus {
  is_initialized: boolean;
  /**
   * The server's environment names its database and both secrets, so the
   * wizard asks only for the organization and its first administrator.
   */
  configured_by_environment?: boolean;
}

export const setupService = {
  async getSetupStatus(signal?: AbortSignal) {
    const data = await requestJSON<{ status?: SetupStatus; err?: string }>("/setup/status", {
      signal,
      auth: false,
    });

    return { status: data.status, err: data.err };
  },

  /** A full request for the wizard, only the people for a server configured by its environment. */
  async setup(req: SetupPeople | SetupRequest, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>("/setup", {
      method: "POST",
      body: req,
      signal,
      auth: false,
    });

    return { err: raiseIfRefused(data).err };
  },

  async testConnection(req: Pick<SetupRequest, 'database_driver' | 'db_host' | 'db_port' | 'db_username' | 'db_password' | 'db_name' | 'db_ssl_enabled'>, signal?: AbortSignal) {
    const data = await requestJSON<{ success: boolean; message: string }>("/setup/test-connection", {
      method: "POST",
      body: req,
      signal,
      auth: false,
    });

    return { success: data.success, message: data.message };
  },
};
