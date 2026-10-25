import { RequestError, requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** The person a People token signs in. */
export interface SignedInPerson {
  id: string;
  project_id: string;
  username: string;
  display_name?: string;
  email?: string;
}

/** A task as the People routes answer it. */
export interface PersonTask {
  id: string;
  name: string;
  description?: string;
  status: string;
  assignee?: { username: string };
  candidate_users?: { username: string }[];
  candidate_groups?: { id?: string; name: string }[];
  priority?: number;
  due_date?: string;
  form_definition?: string;
  variables?: Record<string, unknown>;
  created_at?: string;
}

/** A People session: the token, until when it is good, and whose it is. */
export interface PeopleSession {
  token: string;
  expires_at: string;
  person: SignedInPerson;
}

/** Thrown when the server refuses the People token: the session is over. */
export class PeopleSessionEnded extends Error {
  constructor() {
    super("Your session has ended. Sign in again.");
    this.name = "PeopleSessionEnded";
  }
}

/*
 * Every call but the sign-in sends the People token itself, with auth off:
 * a People token is never the User session's, and a refusal of it must not
 * sign a User out of the platform in the same browser.
 */
async function asPerson<T>(token: string, path: string, init: { method?: string; body?: unknown; signal?: AbortSignal } = {}): Promise<T> {
  try {
    return await requestJSON<T>(path, { ...init, auth: false, headers: { Authorization: `Bearer ${token}` } });
  } catch (error) {
    if (error instanceof RequestError && error.status === 401) {
      throw new PeopleSessionEnded();
    }
    throw error;
  }
}

const taskPath = (id: string) => `/people/tasks/${encodeURIComponent(id)}`;

/** A person's own routes: sign in, and the tasks given or offered to them. */
export const peopleService = {
  async signIn(projectId: string, username: string, password: string, signal?: AbortSignal): Promise<PeopleSession> {
    const data = await requestJSON<Partial<PeopleSession> & { err?: string }>("/people/login", {
      method: "POST",
      body: { project_id: projectId, username, password },
      signal,
      auth: false,
      checksCredentials: true,
    });
    raiseIfRefused(data);
    if (!data.token || !data.person) {
      throw new Error("The server did not answer with a session.");
    }
    return { token: data.token, expires_at: data.expires_at ?? "", person: data.person };
  },

  async listTasks(token: string, signal?: AbortSignal): Promise<PersonTask[]> {
    const data = raiseIfRefused(await asPerson<{ tasks?: PersonTask[]; err?: string }>(token, "/people/tasks", { signal }));
    return data.tasks ?? [];
  },

  async getTask(token: string, id: string, signal?: AbortSignal): Promise<PersonTask> {
    const data = raiseIfRefused(await asPerson<{ task?: PersonTask; err?: string }>(token, taskPath(id), { signal }));
    if (!data.task) {
      throw new Error("The server did not answer with the task.");
    }
    return data.task;
  },

  async claim(token: string, id: string): Promise<void> {
    raiseIfRefused(await asPerson<{ err?: string }>(token, `${taskPath(id)}/claim`, { method: "POST" }));
  },

  async release(token: string, id: string): Promise<void> {
    raiseIfRefused(await asPerson<{ err?: string }>(token, `${taskPath(id)}/release`, { method: "POST" }));
  },

  async complete(token: string, id: string, variables: Record<string, unknown>): Promise<void> {
    raiseIfRefused(await asPerson<{ err?: string }>(token, `${taskPath(id)}/complete`, { method: "POST", body: { variables } }));
  },
};

/** What a person may do with a task, from where it stands. */
export function actionsFor(task: PersonTask, username: string): { claim: boolean; release: boolean; complete: boolean } {
  const holder = task.assignee?.username ?? "";
  if (holder === "") {
    return { claim: true, release: false, complete: false };
  }
  if (holder !== username) {
    return { claim: false, release: false, complete: false };
  }
  // Theirs. A task they took from a pool can go back; one given to them by
  // name has nobody to go back to, so the server refuses that release.
  const fromPool = (task.candidate_users?.length ?? 0) > 0 || (task.candidate_groups?.length ?? 0) > 0;
  return { claim: false, release: fromPool && task.status === "claimed", complete: true };
}
