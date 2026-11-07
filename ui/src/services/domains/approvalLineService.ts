import { requestJSON } from "../shared/rest";
import { raiseIfRefused } from "../raise";

/** One step of a request, as the server says where it stands. */
export interface ApprovalStepState {
  id: string;
  name: string;
  status: "pending" | "waiting" | "approved" | "rejected" | "sent_back";
  waiting?: { task_id: string; assignee?: string; users?: string[]; groups?: string[] };
  users?: string[];
  groups?: string[];
}

/** A request on an approval line. */
export interface ApprovalRequest {
  instance_id: string;
  key: string;
  version: number;
  status: string;
  submitted_by?: string;
  submitted_at?: string;
  steps: ApprovalStepState[];
  revision?: { task_id: string; assignee?: string };
  variables?: Record<string, unknown>;
}

/** One older version's open requests, with what moving them would do. */
export interface ApprovalBehind {
  version: number;
  definition_id: string;
  requests: ApprovalRequest[];
  plan: { refusals?: string[]; warnings?: string[]; compliance_holds?: unknown[]; requires_second_approver?: boolean };
}

/** What still runs on a line's older versions. */
export interface ApprovalTakeover {
  key: string;
  live_version: number;
  behind: ApprovalBehind[];
}

/** What a move did. */
export interface ApprovalMoveResult {
  moved: number;
  left?: { instance_id: string; why: string }[];
}

const linePath = (projectId: string, key: string) =>
  `/projects/${encodeURIComponent(projectId)}/approval-lines/${encodeURIComponent(key)}`;

export const approvalLineService = {
  /** The open requests on a line's versions older than the live one. */
  async listApprovalsBehind(projectId: string, key: string, signal?: AbortSignal): Promise<ApprovalTakeover> {
    const response = await requestJSON<{ takeover?: ApprovalTakeover; err?: string }>(`${linePath(projectId, key)}/behind`, { signal });
    return raiseIfRefused(response).takeover ?? { key, live_version: 0, behind: [] };
  },

  /** Moves the named requests onto the line's live version. */
  async moveApprovals(projectId: string, key: string, requests: string[], signal?: AbortSignal): Promise<ApprovalMoveResult> {
    const response = await requestJSON<{ result?: ApprovalMoveResult; err?: string }>(`${linePath(projectId, key)}/move`, {
      method: "POST",
      body: { requests },
      signal,
    });
    return raiseIfRefused(response).result ?? { moved: 0 };
  },
};
