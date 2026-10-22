/**
 * Keeping a process's simulation cases: loading them when Simulate opens,
 * saving one, deleting one, and knowing which have changed since.
 *
 * Saved by hand rather than as you type. A saved case is what the next person
 * to open the process — and every later version of it — is checked against,
 * so it changes when somebody means it to, not on the way to meaning it.
 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { caseFingerprint, fromWireCase, toWireCase } from '../domain/simulationCase';
import type { Scenario } from '../domain/simulationScenario';
import {
  deleteSimulationCase,
  listSimulationCases,
  saveSimulationCase,
} from '../services/domains/simulationCaseService';
import type { SimulationController } from './useSimulation';

export function useSimulationCases(
  sim: SimulationController,
  projectId: string | null,
  processKey: string,
  active: boolean,
) {
  const queryClient = useQueryClient();
  const enabled = active && projectId !== null && processKey.trim() !== '';
  const queryKey = useMemo(() => ['simulation-cases', projectId, processKey], [projectId, processKey]);

  const saved = useQuery({
    queryKey,
    queryFn: ({ signal }) => listSimulationCases(projectId as string, processKey, signal),
    enabled,
  });

  /** What each case looked like when it was last saved, by local id. */
  const [savedAs, setSavedAs] = useState<Record<string, string>>({});

  // Opens the saved cases once per process. Server data seeding editable
  // state, which is the one thing an effect is for here: refetches must not
  // overwrite a case somebody is in the middle of changing.
  const seeded = useRef<string | null>(null);
  const { loadSaved } = sim;
  useEffect(() => {
    if (saved.data === undefined) return;
    const key = `${projectId}/${processKey}`;
    if (seeded.current === key) return;
    seeded.current = key;
    const scenarios = saved.data.map(fromWireCase);
    loadSaved(scenarios);
    /* eslint-disable-next-line react-hooks/set-state-in-effect */
    setSavedAs(Object.fromEntries(scenarios.map((scenario) => [scenario.id, caseFingerprint(scenario)])));
  }, [loadSaved, processKey, projectId, saved.data]);

  const { markSaved } = sim;
  const save = useMutation({
    mutationFn: (scenario: Scenario) =>
      saveSimulationCase(projectId as string, processKey, scenario.savedId, toWireCase(scenario)),
    onSuccess: (stored, scenario) => {
      markSaved(scenario.id, stored.id);
      setSavedAs((current) => ({ ...current, [scenario.id]: caseFingerprint(scenario) }));
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const { removeCase } = sim;
  const remove = useMutation({
    mutationFn: async (scenario: Scenario) => {
      if (scenario.savedId !== undefined) {
        await deleteSimulationCase(projectId as string, processKey, scenario.savedId);
      }
    },
    onSuccess: (_nothing, scenario) => {
      removeCase(scenario.id);
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  /** Saved and unchanged since — the case the server holds is this one. */
  const isSaved = useCallback(
    (scenario: Scenario) => scenario.savedId !== undefined && savedAs[scenario.id] === caseFingerprint(scenario),
    [savedAs],
  );

  return {
    available: enabled,
    loading: saved.isLoading,
    loadError: saved.error,
    isSaved,
    save: (scenario: Scenario) => save.mutate(scenario),
    saving: save.isPending,
    saveError: save.error,
    remove: (scenario: Scenario) => remove.mutate(scenario),
    removeError: remove.error,
  };
}

export type SimulationCasesController = ReturnType<typeof useSimulationCases>;
