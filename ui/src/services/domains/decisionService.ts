import { requestJSON } from "../shared/rest";
import type {
  ApiDecision,
  CreateDecisionPayload,
  DecisionResult,
  ProcessVariables,
} from "../types";

type DecisionListResponse = {
  decisions?: ApiDecision[];
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
  async listDecisions(projectId: string, signal?: AbortSignal) {
    const data = await requestJSON<DecisionListResponse>(`/decisions?project_id=${projectId}`, { signal });
    return { decisions: data.decisions ?? [], err: data.err };
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
    return { err: data.err };
  },

  async deleteDecision(id: string) {
    const data = await requestJSON<MutationResponse>(`/decisions/${id}`, {
      method: "DELETE",
    });
    return { err: data.err };
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
    return { result: data.result, err: data.err };
  },
};
