import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { AUTHORED_STALE_TIME } from '../services/queryDefaults';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import type { CreateDecisionPayload, ProcessVariables } from '../services/types';

type DecisionsResult = Awaited<ReturnType<typeof processService.listDecisions>>;

type DecisionResult = Awaited<ReturnType<typeof processService.getDecision>>;

export const useDecisions = (page = 1, pageSize = 25) => {
  const { currentProjectId } = useAppStore();
  return useQuery({
    staleTime: AUTHORED_STALE_TIME,
    queryKey: ['decisions', currentProjectId, page, pageSize],
    queryFn: ({ signal }) =>
      currentProjectId
        ? processService.listDecisions(currentProjectId, { page, pageSize }, signal)
        : Promise.resolve({ decisions: [], err: undefined, pageInfo: undefined } as DecisionsResult),
    enabled: !!currentProjectId,
    placeholderData: (previous) => previous,
  });
};

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
  const { currentProjectId } = useAppStore();
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

export const useEvaluateDecision = () => {
  return useMutation({
    mutationFn: ({ key, variables, version }: { key: string; variables: ProcessVariables; version?: number }) => 
      processService.evaluateDecision(key, variables, version),
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
