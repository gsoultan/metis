import { organizationClient } from "../shared/connect";
import { raiseIfRefused } from "../raise";

export const organizationService = {
  async listOrganizations(signal?: AbortSignal) {
    const response = await organizationClient.listOrganizations({}, { signal });
    return { organizations: response.organizations ?? [], err: response.error };
  },

  async createOrganization(name: string, description: string, signal?: AbortSignal) {
    const response = await organizationClient.createOrganization({ name, description }, { signal });
    return { organization: response.organization, err: response.error };
  },

  async updateOrganization(id: string, name: string, description: string, signal?: AbortSignal) {
    const response = await organizationClient.updateOrganization({ id, name, description }, { signal });
    return { err: raiseIfRefused(response).error };
  },

  async deleteOrganization(id: string, signal?: AbortSignal) {
    const response = await organizationClient.deleteOrganization({ id }, { signal });
    return { err: raiseIfRefused(response).error };
  },
};
