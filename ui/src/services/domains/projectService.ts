import { projectClient } from "../shared/connect";
import { raiseIfRefused } from "../raise";

export const projectService = {
  async listProjects(organizationId?: string, signal?: AbortSignal) {
    const response = await projectClient.listProjects({ organizationId }, { signal });
    return { projects: response.projects, err: response.error };
  },

  async createProject(organizationId: string, name: string, description: string, signal?: AbortSignal) {
    const response = await projectClient.createProject({ organizationId, name, description }, { signal });
    return { project: response.project, err: response.error };
  },

  async updateProject(projectId: string, organizationId: string, name: string, description: string, signal?: AbortSignal) {
    const response = await projectClient.updateProject({ id: projectId, organizationId, name, description }, { signal });
    return { err: raiseIfRefused(response).error };
  },

  async deleteProject(projectId: string, signal?: AbortSignal) {
    const response = await projectClient.deleteProject({ id: projectId }, { signal });
    return { err: raiseIfRefused(response).error };
  },
};
