import { userClient } from "../shared/connect";
import { requestJSON } from "../shared/rest";
import type { ApiOrganizationUser, ApiGroup, CreateUserPayload } from "../types";
import { raiseIfRefused } from "../raise";

/**
 * What the server reads when a user is written. It mirrors entities.User: an
 * organization is a membership, `organizations: [{ id }]`, never a name.
 *
 * The body used to carry `organization_id`, which nothing on the server
 * reads, and a free-text `organization`, which the server decodes into a
 * struct — so any string, even "", failed the whole request with a 400. A
 * user created without a membership then got "no organization membership"
 * on every request they made.
 */
interface UserWriteBody {
  id?: string;
  username?: string;
  full_name: string;
  display_name: string;
  email: string;
  roles?: string[];
  organizations?: Array<{ id: string }>;
}

/**
 * A self-update omits `roles`: the server keeps the stored ones when the
 * field is absent, and a person editing their own name must not be able to
 * hand themselves a different role on the way past.
 */
export interface UserUpdate {
  full_name: string;
  display_name: string;
  email: string;
  roles?: string[];
}

/**
 * What a person may change about themselves: how they are named and where mail
 * reaches them. The server's /users/me has no field for anything else — not
 * roles, not organizations, not the username.
 */
export interface OwnProfileUpdate {
  full_name: string;
  display_name: string;
  email: string;
}

/** A group's roles are what its members inherit, so they travel with it. */
export interface GroupWrite {
  name: string;
  description: string;
  roles?: string[];
}

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
    const body: { user: UserWriteBody; password: string } = {
      user: {
        username: user.username,
        full_name: user.full_name,
        display_name: user.display_name,
        email: user.email,
        roles: user.roles,
        organizations: [{ id: user.organization_id }],
      },
      password: user.password,
    };
    const data = await requestJSON<{ user?: ApiOrganizationUser; err?: string }>("/users", {
      method: "POST",
      body,
      signal,
    });

    return { user: raiseIfRefused(data).user };
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

  // The caller's own profile, named by the session like the password above.
  // The Profile page saved through updateUser, whose PUT /users/{id} only an
  // administrator may call, so for everybody else it failed.
  async getOwnProfile(signal?: AbortSignal) {
    const data = await requestJSON<{ user?: ApiOrganizationUser; err?: string }>("/users/me", { signal });
    return { user: raiseIfRefused(data).user };
  },

  async updateOwnProfile(profile: OwnProfileUpdate, signal?: AbortSignal) {
    // Field by field rather than spread, so nothing else the object carries
    // is sent along.
    const body = {
      user: { full_name: profile.full_name, display_name: profile.display_name, email: profile.email },
    };
    const data = await requestJSON<{ err?: string }>("/users/me", {
      method: "PUT",
      body,
      signal,
    });

    return { err: raiseIfRefused(data).err };
  },

  async updateUser(id: string, user: UserUpdate, signal?: AbortSignal) {
    const body: { user: UserWriteBody } = {
      user: {
        id,
        full_name: user.full_name,
        display_name: user.display_name,
        email: user.email,
        roles: user.roles,
      },
    };
    const data = await requestJSON<{ err?: string }>(`/users/${id}`, {
      method: "PUT",
      body,
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

  async createGroup(group: GroupWrite & { organization_id: string }, signal?: AbortSignal) {
    const data = await requestJSON<{ group?: ApiGroup; err?: string }>(`/organizations/${group.organization_id}/groups`, {
      method: "POST",
      body: { group: { name: group.name, description: group.description, roles: group.roles } },
      signal,
    });

    return { group: raiseIfRefused(data).group };
  },

  async updateGroup(id: string, group: GroupWrite, signal?: AbortSignal) {
    const data = await requestJSON<{ err?: string }>(`/groups/${id}`, {
      method: "PUT",
      body: { group: { name: group.name, description: group.description, roles: group.roles } },
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
