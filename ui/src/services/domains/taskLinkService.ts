import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** The task a link opens, as the server answers it. */
export interface LinkedTask {
  id: string;
  name: string;
  description?: string;
  form_definition?: string;
  variables?: Record<string, unknown>;
  due_date?: string;
}

/** Who the link is for. */
export interface LinkedPerson {
  username: string;
  display_name?: string;
}

export interface TaskLinkView {
  task: LinkedTask;
  person: LinkedPerson;
  expires_at: string;
}

/**
 * A task link acts for one person on one task, once, with no sign-in.
 *
 * Both calls are POSTs that carry the token in the body: opening a link must
 * never act on a task, and an address is written to access logs where a body
 * is not.
 */
export const taskLinkService = {
  async readTaskLink(token: string, signal?: AbortSignal): Promise<TaskLinkView> {
    const data = await requestJSON<Partial<TaskLinkView> & { err?: string }>("/people/links/read", {
      method: "POST",
      body: { token },
      signal,
      auth: false,
    });
    raiseIfRefused(data);
    if (!data.task || !data.person) {
      throw new Error("The server did not answer with the task.");
    }
    return { task: data.task, person: data.person, expires_at: data.expires_at ?? "" };
  },

  async completeByTaskLink(token: string, variables: Record<string, unknown>, signal?: AbortSignal): Promise<void> {
    const data = await requestJSON<{ err?: string }>("/people/links/complete", {
      method: "POST",
      body: { token, variables },
      signal,
      auth: false,
    });
    raiseIfRefused(data);
  },
};

/**
 * The token is what follows the "#" of the link: a browser never sends that
 * part of an address, so it reaches no server log on the way here.
 */
export function tokenFromHash(hash: string): string {
  return decodeURIComponent(hash.replace(/^#/, "")).trim();
}
