import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { collectPages, type CollectedPages, type ListPage } from '../domain/allPages';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { ApiDecisionSummary, CreateDecisionPayload, ProcessVariables } from '../services/types';

type DecisionsResult = Awaited<ReturnType<typeof processService.listDecisionSummaries>>;

type DecisionResult = Awaited<ReturnType<typeof processService.getDecision>>;

/**
 * One page of the current project's decisions, one row each, searched on the
 * server: a list holds the page it shows and nothing more, however many
 * decisions there are. A row is a decision rather than a version of one — its
 * live version, and the newest when that is newer.
 */
export const useDecisions = (page = 1, pageSize = 25, search = '') => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['decisions', currentProjectId, page, pageSize, search],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.listDecisionSummaries(currentProjectId, { page, pageSize, search }, signal)
        : Promise.resolve({ summaries: [], pageInfo: undefined } as DecisionsResult),
    enabled: !!currentProjectId,
    placeholderData: (previous) => previous,
  });
};

/**
 * How many decision keys a view that needs all of them reads: five pages of
 * the largest page the server serves (MaxPageSize). Past that the result says
 * it stopped short.
 */
const SUMMARY_PAGE_SIZE = 200;
const SUMMARY_MAX_PAGES = 5;

const NO_SUMMARIES: CollectedPages<ApiDecisionSummary> = { items: [], truncated: false, total: 0 };

/**
 * Every decision key in the current project, each as its live version and
 * without its table, up to that bound: for a view that cannot work from a page.
 * The dependency graph cannot tell a decision that does not exist from one on
 * the next page, and a step's picker has to offer every decision.
 */
export const useDecisionSummaries = () => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    // Under 'decisions', so saving, adding or deleting a decision refreshes it.
    queryKey: ['decisions', currentProjectId, 'summaries'],
    queryFn: ({ signal }) =>
      currentProjectId
        ? collectPages((page) => summaryPage(currentProjectId, page, signal), SUMMARY_PAGE_SIZE, SUMMARY_MAX_PAGES)
        : Promise.resolve(NO_SUMMARIES),
    enabled: !!currentProjectId,
  });
};

async function summaryPage(projectId: string, page: number, signal: AbortSignal): Promise<ListPage<ApiDecisionSummary>> {
  const listed = await processService.listDecisionSummaries(projectId, { page, pageSize: SUMMARY_PAGE_SIZE }, signal);
  return { items: listed.summaries, total: listed.pageInfo?.total ?? listed.summaries.length };
}

export const useDecision = (id: string | null) => {
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['decision', id],
    queryFn: ({ signal }) =>
      id
        ? processService.getDecision(id, signal)
        : Promise.resolve({ decision: undefined, err: undefined } as DecisionResult),
    enabled: !!id,
  });
};

export const useCreateDecision = () => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (params: CreateDecisionPayload) =>
      // The API reads a nested project, not a project_id. Sending the id meant
      // the decision was stored belonging to no project, so it never appeared
      // in the project's list again.
      processService.createDecision({ ...params, project: currentProjectId ? { id: currentProjectId } : undefined }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['decisions'] });
    },
  });
};

export const useUpdateDecision = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...params }: CreateDecisionPayload & { id: string }) =>
      processService.updateDecision(id, params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['decisions'] });
      queryClient.invalidateQueries({ queryKey: ['decision'] });
    },
  });
};

export const useDeleteDecision = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => processService.deleteDecision(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['decisions'] });
    },
  });
};

/** Runs a saved table in the current project — the one the editor saves into. */
export const useEvaluateDecision = () => {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  return useMutation({
    mutationFn: ({ key, variables, version }: { key: string; variables: ProcessVariables; version?: number }) =>
      processService.evaluateDecision(currentProjectId ?? '', key, variables, version),
  });
};

/**
 * What depends on a decision.
 *
 * Only fetched when an editor is open on a saved table — it walks every process
 * definition in the project, which is fine for a deliberate question and wrong
 * for a list view.
 */
export const useDecisionImpact = (decisionId: string | null) => {
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['decision-impact', decisionId],
    queryFn: ({ signal }) => (decisionId ? processService.decisionImpact(decisionId, signal) : Promise.resolve(undefined)),
    enabled: !!decisionId,
  });
};

/** Runs a saved table against the examples stored with it. */
export const useRunDecisionTests = () => {
  return useMutation({
    mutationFn: (id: string) => processService.runDecisionTests(id),
  });
};
