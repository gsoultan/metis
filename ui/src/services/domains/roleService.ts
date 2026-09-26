import { raiseIfRefused } from "../raise";
import { requestJSON } from "../shared/rest";

/**
 * What each role is required for, as the server reads it from its own gates.
 *
 * The browser used to know a role by one sentence in domain/roles.ts, written
 * by hand and held to nothing; GET /roles is the list the gates were built
 * from, so it cannot say a role allows something the server refuses it.
 */

/** One action a role is required for. Mirrors entities.RoleAction. */
export interface ApiRoleAction {
  /** The name the gate was built with, as the request log shows it. */
  method: string;
  /** Which part of the product it belongs to, as a key the catalogues translate. */
  area: string;
  /** The method in words: "Create definition". */
  label: string;
}

/** One role and everything it is required for. Mirrors entities.RoleAccess. */
export interface ApiRoleAccess {
  role: string;
  actions: ApiRoleAction[];
}

export const roleService = {
  /**
   * Every role with the actions it is required for. Anybody signed in may ask,
   * and the answer is the same for everybody.
   */
  async listRoles(signal?: AbortSignal): Promise<ApiRoleAccess[]> {
    const data = await requestJSON<{ roles?: ApiRoleAccess[] | null; err?: string }>("/roles", { signal });
    return (raiseIfRefused(data).roles ?? []).map((access) => ({
      role: access.role,
      actions: access.actions ?? [],
    }));
  },
};
