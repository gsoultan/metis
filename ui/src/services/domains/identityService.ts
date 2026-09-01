import { userClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type { ApiOrganizationUser, ApiGroup, CreateUserPayload } from "../types";
import { raiseIfRefused } from "../raise";

export const identityService = {
  async getUser(id: string, signal?: AbortSignal) {
    const response = await userClient.getUser({ id }, { signal });
    return { user: response.user };
  },

  async listUsers(organizationId: string, signal?: AbortSignal) {
    if (!organizationId) {
      return { users: [] };
    }

    const data = await requestJSON<{ users?: ApiOrganizationUser[] }>(`/organizations/${organizationId}/users`, { signal });
    return { users: data.users ?? [] };
  },

  async createUser(user: CreateUserPayload, signal?: AbortSignal) {
    const data = await requestJSON<{ user?: ApiOrganizationUser; err?: string }>("/users", {
      method: "POST",
      body: {
        user: {
          organization_id: user.organization_id,
          username: user.username,
          full_name: user.full_name,
          display_name: user.display_name,
          organization: user.organization,
          email: user.email,
          roles: user.roles,
        },
        password: user.password,
      },
      signal,
    });

    return { user: data.user, err: data.err };
  },

  // The caller is not named: the server takes the account from the session.
  // Sending an id here would be asking it to trust the browser about whose
  // password is being changed.
  async changeOwnPassword(currentPassword: string, newPassword: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>("/users/me/password", {
      method: "POST",
      body: { current_password: currentPassword, new_password: newPassword },
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async updateUser(id: string, user: { full_name: string; display_name: string; organization: string; email: string; roles: string[] }, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/users/${id}`, {
      method: "PUT",
      body: {
        user: {
          id: id,
          full_name: user.full_name,
          display_name: user.display_name,
          organization: user.organization,
          email: user.email,
          roles: user.roles,
        },
      },
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async deleteUser(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/users/${id}`, {
      method: "DELETE",
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async listGroups(organizationId: string, signal?: AbortSignal) {
    if (!organizationId) {
      return { groups: [] };
    }

    try {
      const data = await requestJSON<{ groups?: ApiGroup[] }>(`/organizations/${organizationId}/groups`, { signal });
      return { groups: data.groups ?? [] };
    } catch (error) {
      if (error instanceof Error && /unauthorized/i.test(error.message)) {
        return { groups: [] };
      }

      throw error;
    }
  },

  async createGroup(group: { organization_id: string; name: string; description: string }, signal?: AbortSignal) {
    const data = await requestJSON<{ group?: ApiGroup; err?: string }>(`/organizations/${group.organization_id}/groups`, {
      method: "POST",
      body: { group: { name: group.name, description: group.description } },
      signal,
    });

    return { group: data.group, err: data.err };
  },

  async updateGroup(id: string, group: { name: string; description: string }, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/groups/${id}`, {
      method: "PUT",
      body: { group: { name: group.name, description: group.description } },
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async deleteGroup(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/groups/${id}`, {
      method: "DELETE",
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async listGroupMembers(groupId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ users?: ApiOrganizationUser[]; err?: string }>(`/groups/${groupId}/members`, { signal });
    return { users: data.users ?? [], err: data.err };
  },

  async addMembership(groupId: string, userId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/groups/${groupId}/members/${userId}`, {
      method: "POST",
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async removeMembership(groupId: string, userId: string, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/groups/${groupId}/members/${userId}`, {
      method: "DELETE",
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async listUserGroups(userId: string, signal?: AbortSignal) {
    if (!userId) {
      return { groups: [] };
    }

    try {
      const data = await requestJSON<{ groups?: ApiGroup[] }>(`/users/${userId}/groups`, { signal });
      return { groups: data.groups ?? [] };
    } catch (error) {
      if (error instanceof Error && /unauthorized/i.test(error.message)) {
        return { groups: [] };
      }

      throw error;
    }
  },
};
