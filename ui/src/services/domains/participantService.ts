import { API_BASE_URL } from "../shared/config";
import { getAuthHeaders, getAuthToken } from "../shared/auth";
import { statusMessage } from "../shared/errors";
import { RequestError, requestJSON } from "../shared/rest";
import { reportSessionRefused } from "../shared/sessionRefusal";
import { raiseIfRefused } from "../raise";
import type { ImportSummary, Participant } from "../../domain/participantImport";
import type { PersonDevice } from "./peopleService";

interface ListParticipantsResponse {
  participants?: Participant[];
  err?: string;
}

/** A project's People group, as the server lists it. */
export interface ParticipantGroup {
  id: string;
  name: string;
  description?: string;
}

interface ListParticipantGroupsResponse {
  groups?: ParticipantGroup[];
  err?: string;
}

interface ImportParticipantsResponse extends ImportSummary {
  err?: string;
}

interface RemoveParticipantResponse {
  err?: string;
}

export const participantService = {
  /**
   * A project's participants, alphabetically. `limit` asks for at most that
   * many: one answers whether the project has anybody without downloading its
   * directory.
   */
  async listParticipants(projectId: string, signal?: AbortSignal, options: { limit?: number } = {}) {
    const limit = options.limit ? `&limit=${options.limit}` : "";
    const response = await requestJSON<ListParticipantsResponse>(
      `/participants?project_id=${encodeURIComponent(projectId)}${limit}`,
      { method: "GET", signal },
    );
    return { participants: response.participants ?? [], err: response.err };
  },

  /** A project's People groups, by name: the teams a step can offer its task to. */
  async listParticipantGroups(projectId: string, signal?: AbortSignal) {
    const response = await requestJSON<ListParticipantGroupsResponse>(
      `/participant-groups?project_id=${encodeURIComponent(projectId)}`,
      { method: "GET", signal },
    );
    return { groups: response.groups ?? [], err: response.err };
  },

  /**
   * Imports a directory.
   *
   * A file goes as multipart because it is a file; an endpoint or a query goes
   * as JSON. One method rather than three, because the caller is answering one
   * question — where does this list come from — and the reply is the same
   * summary either way.
   */
  async importParticipants(projectId: string, source: Record<string, unknown>, signal?: AbortSignal) {
    if (source.kind === "csv" && source.file instanceof File) {
      const form = new FormData();
      form.append("project_id", projectId);
      form.append("file", source.file);
      // Not requestJSON: it sets a JSON content type, and multipart needs the
      // browser to set its own boundary.
      const token = getAuthToken();
      const response = await fetch(`${API_BASE_URL}/participants/import`, {
        method: "POST",
        headers: getAuthHeaders(),
        body: form,
        signal,
      });
      if (response.status === 401) reportSessionRefused(token);
      const body = (await response.json().catch(() => ({}))) as ImportParticipantsResponse;
      if (!response.ok) {
        throw new RequestError(body.err || statusMessage(response.status), response.status);
      }
      return raiseIfRefused(body) as ImportSummary;
    }

    const body = await requestJSON<ImportParticipantsResponse>("/participants/import", {
      method: "POST",
      body: { project_id: projectId, ...source },
      signal,
    });
    return raiseIfRefused(body) as ImportSummary;
  },

  /**
   * Takes somebody out of a project's directory.
   *
   * The project goes with the id rather than being inferred from it, because the
   * server checks the two agree — an id on its own would let somebody who
   * manages one project's directory remove a person from another's.
   */
  async removeParticipant(projectId: string, id: string, signal?: AbortSignal) {
    const response = await requestJSON<RemoveParticipantResponse>(
      `/participants/${encodeURIComponent(id)}?project_id=${encodeURIComponent(projectId)}`,
      { method: "DELETE", signal },
    );
    return { err: raiseIfRefused(response).err };
  },

  /** Gives a person a password for the People login, ending their sessions. */
  async setParticipantPassword(projectId: string, id: string, password: string, signal?: AbortSignal) {
    const response = await requestJSON<{ err?: string }>(
      `/participants/${encodeURIComponent(id)}/password?project_id=${encodeURIComponent(projectId)}`,
      { method: "PUT", body: { password }, signal },
    );
    raiseIfRefused(response);
  },

  /** The names this project's work uses that its People lack. */
  async listDirectoryGaps(projectId: string, signal?: AbortSignal): Promise<DirectoryGap[]> {
    const response = await requestJSON<{ gaps?: DirectoryGap[]; err?: string }>(
      `/participants/gaps?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return raiseIfRefused(response).gaps ?? [];
  },

  /** The project's open tasks nobody can act on, with why. */
  async listStrandedTasks(projectId: string, signal?: AbortSignal): Promise<StrandedTask[]> {
    const response = await requestJSON<{ tasks?: StrandedTask[]; err?: string }>(
      `/participants/stranded-tasks?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return raiseIfRefused(response).tasks ?? [];
  },

  /** The open tasks a person holds, which deactivating them has to settle. */
  async listHeldTasks(projectId: string, id: string, signal?: AbortSignal): Promise<HeldTask[]> {
    const response = await requestJSON<{ tasks?: HeldTask[]; err?: string }>(
      `/participants/${encodeURIComponent(id)}/held-tasks?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return raiseIfRefused(response).tasks ?? [];
  },

  /**
   * Deactivates or reactivates a person. Deactivating says what happens to the
   * tasks they hold; reactivating moves nothing back.
   */
  async setParticipantActive(
    projectId: string,
    id: string,
    active: boolean,
    work?: HeldWork,
    signal?: AbortSignal,
  ): Promise<ActivationResult> {
    const response = await requestJSON<{ result?: ActivationResult; err?: string }>(
      `/participants/${encodeURIComponent(id)}/active?project_id=${encodeURIComponent(projectId)}`,
      { method: "PUT", body: { active, work }, signal },
    );
    return raiseIfRefused(response).result ?? { moved: 0 };
  },

  /** How many push devices each person in the project has, by their id. */
  async deviceCounts(projectId: string, signal?: AbortSignal): Promise<Record<string, number>> {
    const response = await requestJSON<{ counts?: Record<string, number>; err?: string }>(
      `/participants/device-counts?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return raiseIfRefused(response).counts ?? {};
  },

  /** One person's push devices. */
  async listParticipantDevices(projectId: string, id: string, signal?: AbortSignal): Promise<PersonDevice[]> {
    const response = await requestJSON<{ devices?: PersonDevice[]; err?: string }>(
      `/participants/${encodeURIComponent(id)}/devices?project_id=${encodeURIComponent(projectId)}`,
      { signal },
    );
    return raiseIfRefused(response).devices ?? [];
  },

  /** Removes a person's device, such as a phone they lost. */
  async removeParticipantDevice(projectId: string, id: string, deviceId: string): Promise<void> {
    const response = await requestJSON<{ err?: string }>(
      `/participants/${encodeURIComponent(id)}/devices/${encodeURIComponent(deviceId)}?project_id=${encodeURIComponent(projectId)}`,
      { method: "DELETE" },
    );
    raiseIfRefused(response);
  },

  /** Adds the named gaps to the project's People, from the organization's Users and groups. */
  async fillDirectoryGaps(projectId: string, people: string[], groups: string[], signal?: AbortSignal): Promise<DirectoryGapImport> {
    const response = await requestJSON<{ result?: DirectoryGapImport; err?: string }>("/participants/gaps/fill", {
      method: "POST",
      body: { project_id: projectId, people, groups },
      signal,
    });
    return raiseIfRefused(response).result ?? { people: 0, groups: 0 };
  },
};

/** A name a project's work uses that its People directory does not have. */
export interface DirectoryGap {
  name: string;
  kind: "person" | "group";
  open_tasks: number;
  models?: string[];
  has_account?: boolean;
  display_name?: string;
  email?: string;
  has_platform_group?: boolean;
  members?: string[];
}

/** One name a stranded task uses, and why it can no longer act. */
export interface StrandedName {
  name: string;
  kind: "person" | "group";
  /** gone: nobody or no group of that name; inactive: a deactivated person; empty: a group nobody in it can act. */
  why: "gone" | "inactive" | "empty";
}

/** An open task nobody can act on. */
export interface StrandedTask {
  task_id: string;
  name: string;
  node_id?: string;
  instance_id?: string;
  since?: string;
  kind: "assignee_gone" | "assignee_inactive" | "no_candidate";
  assignee?: string;
  missing: StrandedName[];
}

/** What filling the gaps did. */
export interface DirectoryGapImport {
  people: number;
  groups: number;
  skipped?: { username?: string; reason: string }[];
}

/** What deactivating somebody does with the tasks they hold. */
export interface HeldWork {
  /** keep: leave them (listed as stuck); release: back on offer; hand_over: to one account. */
  do: "keep" | "release" | "hand_over";
  to?: string;
  reason?: string;
}

/** An open task a person holds. */
export interface HeldTask {
  task_id: string;
  name: string;
  instance_id?: string;
  since?: string;
}

/** What changing whether somebody is active did. */
export interface ActivationResult {
  moved: number;
  kept?: (HeldTask & { why?: string })[];
}
