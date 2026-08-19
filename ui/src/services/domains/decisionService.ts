import { requestJSON } from "../shared/rest";
import type {
  ApiDecision,
  CreateDecisionPayload,
  DecisionResult,
  ProcessVariables,
} from "../types";
import { raiseIfRefused } from "../raise";

type DecisionListResponse = {
  decisions?: ApiDecision[];
  page?: { total: number; page: number; page_size: number; has_more: boolean };
  err?: string;
};

type DecisionResponse = {
  decision?: ApiDecision;
  err?: string;
};

type CreateDecisionResponse = {
  id: string;
  err?: string;
};

type MutationResponse = {
  err?: string;
};

type EvaluateDecisionResponse = {
  result?: DecisionResult;
  err?: string;
};

export const decisionService = {
  async listDecisions(
    projectId: string,
    page?: { page: number; pageSize: number },
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams({ project_id: projectId });
    if (page) {
      query.set("page", String(page.page));
      query.set("page_size", String(page.pageSize));
    }
    const data = await requestJSON<DecisionListResponse>(`/decisions?${query}`, { signal });
    return {
      decisions: data.decisions ?? [],
      err: data.err,
      pageInfo: data.page
        ? {
            total: data.page.total,
            page: data.page.page,
            pageSize: data.page.page_size,
            hasMore: data.page.has_more,
          }
        : undefined,
    };
  },

  async getDecision(id: string, signal?: AbortSignal) {
    const data = await requestJSON<DecisionResponse>(`/decisions/${id}`, { signal });
    return { decision: data.decision, err: data.err };
  },

  async createDecision(params: CreateDecisionPayload) {
    const data = await requestJSON<CreateDecisionResponse>("/decisions", {
      method: "POST",
      body: { decision: params },
    });
    return { id: data.id, err: data.err };
  },

  async updateDecision(id: string, params: CreateDecisionPayload) {
    const data = await requestJSON<MutationResponse>(`/decisions/${id}`, {
      method: "PUT",
      body: { decision: params },
    });
    return { err: raiseIfRefused(data).err };
  },

  async deleteDecision(id: string) {
    const data = await requestJSON<MutationResponse>(`/decisions/${id}`, {
      method: "DELETE",
    });
    return { err: raiseIfRefused(data).err };
  },

  async evaluateDecision(
    key: string,
    variables: ProcessVariables = {},
    version: number = 0,
    signal?: AbortSignal,
  ) {
    const data = await requestJSON<EvaluateDecisionResponse>("/decisions/evaluate", {
      method: "POST",
      body: { key, variables, version },
      signal,
    });
    return {
      result: data.result,
      // Which lines of the table produced the answer, so the editor can show
      // the reasoning rather than only the outcome.
      matchedRules: data.result?.matched_rules ?? [],
      err: data.err,
    };
  },

  /** Runs a table against the examples stored with it. */
  async runDecisionTests(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ results?: ApiDecisionTestResult[]; err?: string }>(
      `/decisions/${id}/tests/run`,
      { method: "POST", signal },
    );
    return data.results ?? [];
  },

  /** What depends on a decision, before somebody changes it. */
  async decisionImpact(id: string, signal?: AbortSignal) {
    const data = await requestJSON<{ impact?: ApiDecisionImpact; err?: string }>(`/decisions/${id}/impact`, { signal });
    return data.impact;
  },
};
/** Mirrors entities.DecisionImpact. */
export interface ApiDecisionImpact {
  decision_key: string;
  running_instances: number;
  processes?: Array<{
    definition_id: string;
    definition_key: string;
    definition_name?: string;
    version: number;
    steps?: string[];
    running_instances: number;
  }>;
}

/** Mirrors entities.DecisionTest. */
export interface ApiDecisionTest {
  id: string;
  name: string;
  inputs?: Record<string, unknown>;
  expected?: Record<string, unknown>;
}

/** Mirrors entities.DecisionTestResult. */
export interface ApiDecisionTestResult {
  id: string;
  name: string;
  passed: boolean;
  actual?: Record<string, unknown>;
  mismatches?: string[];
  matched_rules?: number[];
  err?: string;
}
