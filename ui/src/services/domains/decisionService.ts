import { requestJSON } from "../shared/rest";
import type {
  ApiDecision,
  ApiDecisionSummary,
  CreateDecisionPayload,
  DecisionResult,
  ProcessVariables,
} from "../types";
import { raiseIfRefused } from "../raise";

type PageResponse = { total: number; page: number; page_size: number; has_more: boolean };

type DecisionListResponse = {
  decisions?: ApiDecision[];
  page?: PageResponse;
  err?: string;
};

type DecisionSummaryListResponse = {
  summaries?: ApiDecisionSummary[];
  page?: PageResponse;
  err?: string;
};

/** One page of a list, and where it sits in the whole. */
export interface DecisionListPage {
  page: number;
  pageSize: number;
  /** Keeps the decisions whose name or key contains it; the server does the searching. */
  search?: string;
}

function pageQuery(projectId: string, page?: DecisionListPage): URLSearchParams {
  const query = new URLSearchParams({ project_id: projectId });
  if (page) {
    query.set("page", String(page.page));
    query.set("page_size", String(page.pageSize));
    if (page.search?.trim()) query.set("q", page.search.trim());
  }
  return query;
}

function pageInfoOf(page: PageResponse | undefined) {
  return page
    ? { total: page.total, page: page.page, pageSize: page.page_size, hasMore: page.has_more }
    : undefined;
}

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

type UpdateDecisionResponse = {
  id?: string;
  version?: number;
  new_version?: boolean;
  live?: boolean;
  err?: string;
};

/**
 * What saving an edit did. A save never changes the version it was sent to:
 * the edit becomes the key's next version, so the id that was edited no longer
 * names what it now holds. `newVersion` is false when nothing had changed, and
 * `live` says whether the version saved is the one evaluations now read.
 */
export interface SavedDecision {
  id: string;
  version: number;
  newVersion: boolean;
  live: boolean;
}

/** How a save puts its version into force: now, or staged beside the live one. */
export interface SaveDecisionOptions {
  stage?: boolean;
}

type EvaluateDecisionResponse = {
  /**
   * matched_rule_ids is on entities.DecisionResult and not yet on the shared
   * DecisionResult type; it is read here, where it is used.
   */
  result?: DecisionResult & { matched_rule_ids?: string[] };
  err?: string;
};

export const decisionService = {
  async listDecisions(projectId: string, page?: DecisionListPage, signal?: AbortSignal) {
    const data = await requestJSON<DecisionListResponse>(`/decisions?${pageQuery(projectId, page)}`, { signal });
    return { decisions: data.decisions ?? [], err: data.err, pageInfo: pageInfoOf(data.page) };
  },

  /**
   * One page of the project's decision keys, each as its newest version and
   * without the table: for views that need every decision's name and
   * dependencies, and none of its lines.
   */
  async listDecisionSummaries(projectId: string, page: DecisionListPage, signal?: AbortSignal) {
    const data = await requestJSON<DecisionSummaryListResponse>(
      `/decisions/summaries?${pageQuery(projectId, page)}`,
      { signal },
    );
    return { summaries: raiseIfRefused(data).summaries ?? [], pageInfo: pageInfoOf(data.page) };
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
    return { id: raiseIfRefused(data).id };
  },

  async updateDecision(id: string, params: CreateDecisionPayload, options: SaveDecisionOptions = {}): Promise<SavedDecision> {
    const data = raiseIfRefused(
      await requestJSON<UpdateDecisionResponse>(`/decisions/${id}`, {
        method: "PUT",
        body: options.stage ? { decision: params, stage: true } : { decision: params },
      }),
    );
    return {
      id: data.id ?? id,
      version: data.version ?? 0,
      newVersion: data.new_version ?? false,
      live: data.live ?? false,
    };
  },

  async deleteDecision(id: string) {
    const data = await requestJSON<MutationResponse>(`/decisions/${id}`, {
      method: "DELETE",
    });
    return { err: raiseIfRefused(data).err };
  },

  /**
   * Runs a saved table. The project says whose table the key names: keys are
   * unique per project, so the server refuses a key on its own.
   */
  async evaluateDecision(
    projectId: string,
    key: string,
    variables: ProcessVariables = {},
    version: number = 0,
    signal?: AbortSignal,
  ) {
    const data = await requestJSON<EvaluateDecisionResponse>("/decisions/evaluate", {
      method: "POST",
      body: { project_id: projectId, key, variables, version },
      signal,
    });
    return {
      result: data.result,
      // Which lines of the table produced the answer, so the editor can show
      // the reasoning rather than only the outcome. Positions count lines in
      // the stored table; ids name them, and survive lines being moved.
      matchedRules: data.result?.matched_rules ?? [],
      matchedRuleIds: data.result?.matched_rule_ids ?? [],
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
