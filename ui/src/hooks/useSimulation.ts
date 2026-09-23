/**
 * One simulation session: the cases, the run, and where the transport is.
 *
 * Built around the interaction the screen is for — **press Run first**. A case
 * starts empty. Where the engine needs something the outside world would have
 * given it, the run stops and names the step, and that step is answered in
 * place on the diagram. Answering re-runs automatically, so the loop is: press
 * Run once, then answer questions until it finishes.
 *
 * The server returns the whole trace in one response, so stepping, scrubbing
 * and stepping *back* are cursor moves over an array that is already here.
 * Everything the canvas draws is derived from `(run, cursor)` during render by
 * the pure selectors in `domain/simulationTrace`.
 */
import { useMutation } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { v4 as uuidv4 } from 'uuid';

import {
  answerFor,
  blankAnswer,
  emptyScenario,
  removeAnswer,
  simulationRequest,
  upsertAnswer,
  validateScenario,
  type AnswerKind,
  type Scenario,
  type SimulationAnswer,
  type SimulationTarget,
  type VariableRow,
} from '../domain/simulationScenario';
import {
  clampIndex,
  changedKeysAt,
  describeClock,
  flowStatesAt,
  lastIndex,
  nodeStatesAt,
  variablesAt,
  verdict,
  type FlowSimState,
  type NodeSimState,
  type SimulationRun,
  type SimulationStep,
  type Verdict,
} from '../domain/simulationTrace';
import { runSimulation, runSimulationBatch } from '../services/domains/simulationService';
import type { ProcessVariables } from '../services/types';

/** How long one step is held when playing, at 1x. */
const STEP_INTERVAL_MS = 700;

export type PlaySpeed = 0.5 | 1 | 2 | 4;

/**
 * Everything the canvas and the panel draw, derived from `(run, cursor)`.
 *
 * Named so the no-run case and the running case are the same type. Left to
 * inference the empty branch would widen `variables` to `{}`, and reading a
 * variable by name off it would stop compiling.
 */
interface SimulationView {
  nodeStates: Record<string, NodeSimState>;
  flowStates: Record<string, FlowSimState>;
  variables: ProcessVariables;
  changedKeys: string[];
  clockLabel: string;
  step: SimulationStep | null;
}

const EMPTY_VIEW: SimulationView = {
  nodeStates: {},
  flowStates: {},
  variables: {},
  changedKeys: [],
  clockLabel: '',
  step: null,
};

export function useSimulation(target: SimulationTarget | null) {
  const [cases, setCases] = useState<Scenario[]>(() => [emptyScenario(uuidv4(), 'First case')]);
  const [activeId, setActiveId] = useState('');
  const [run, setRun] = useState<SimulationRun | null>(null);
  const [cursor, setCursor] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState<PlaySpeed>(1);
  /** Which node's answer card is open on the canvas, if any. */
  const [editing, setEditing] = useState<string | null>(null);
  /** Last result per case, so the picker can show pass/fail. */
  const [suite, setSuite] = useState<Record<string, SimulationRun>>({});

  const active = useMemo(
    () => cases.find((c) => c.id === activeId) ?? cases[0] ?? null,
    [cases, activeId],
  );

  const problems = useMemo(() => (active === null ? [] : validateScenario(active)), [active]);
  const runnable = target !== null && active !== null && problems.length === 0;

  /* ── Editing a case ────────────────────────────────────────────────────── */

  const patchCase = useCallback((id: string, change: Partial<Scenario>) => {
    setCases((current) => current.map((c) => (c.id === id ? { ...c, ...change } : c)));
  }, []);

  const addCase = useCallback(() => {
    const created = emptyScenario(uuidv4());
    setCases((current) => [...current, created]);
    setActiveId(created.id);
    setRun(null);
  }, []);

  const removeCase = useCallback((id: string) => {
    setCases((current) => (current.length === 1 ? current : current.filter((c) => c.id !== id)));
    setSuite((current) => {
      const next = { ...current };
      delete next[id];
      return next;
    });
  }, []);

  const setVariable = useCallback(
    (rowId: string, change: Partial<VariableRow>) => {
      if (active === null) return;
      patchCase(active.id, {
        variables: active.variables.map((row) => (row.id === rowId ? { ...row, ...change } : row)),
      });
    },
    [active, patchCase],
  );

  const addVariable = useCallback(() => {
    if (active === null) return;
    patchCase(active.id, { variables: [...active.variables, { id: uuidv4(), name: '', value: '' }] });
  }, [active, patchCase]);

  const removeVariable = useCallback(
    (rowId: string) => {
      if (active === null) return;
      patchCase(active.id, { variables: active.variables.filter((row) => row.id !== rowId) });
    },
    [active, patchCase],
  );

  /* ── Answering a step ──────────────────────────────────────────────────── */

  const answered = useMemo(() => (active?.answers ?? []).map((answer) => answer.nodeId), [active]);

  const answerAt = useCallback((nodeId: string) => (active === null ? null : answerFor(active, nodeId)), [active]);

  /** Open the card for a node, creating a blank answer of the right kind. */
  const startAnswering = useCallback(
    (nodeId: string, kind: AnswerKind) => {
      if (active === null) return;
      if (answerFor(active, nodeId) === null) {
        setCases((current) => current.map((c) => (c.id === active.id ? upsertAnswer(c, blankAnswer(nodeId, kind)) : c)));
      }
      setEditing(nodeId);
    },
    [active],
  );

  const saveAnswer = useCallback(
    (answer: SimulationAnswer) => {
      if (active === null) return;
      setCases((current) => current.map((c) => (c.id === active.id ? upsertAnswer(c, answer) : c)));
    },
    [active],
  );

  const clearAnswer = useCallback(
    (nodeId: string) => {
      if (active === null) return;
      setCases((current) => current.map((c) => (c.id === active.id ? removeAnswer(c, nodeId) : c)));
      setEditing(null);
    },
    [active],
  );

  const stopAnswering = useCallback(() => setEditing(null), []);

  /* ── Running ───────────────────────────────────────────────────────────── */

  const runOne = useMutation({
    mutationFn: async (scenario: Scenario) => {
      if (target === null) throw new Error('Deploy this process first — a run needs a version to execute.');
      return runSimulation(simulationRequest(scenario, target));
    },
    onSuccess: (result, scenario) => {
      setRun(result);
      setPlaying(false);
      setSuite((current) => ({ ...current, [scenario.id]: result }));
      // Land on the last step. Somebody who pressed Run wants the answer, not
      // the beginning; the scrubber is right there for how it got there.
      setCursor(Math.max(lastIndex(result), 0));
      // A run that stopped to ask something opens that question straight away —
      // the alternative is telling somebody which node to click and making them
      // go find it.
      setEditing(result.outcome === 'needs_answer' ? result.awaitingNode : null);
    },
  });

  const runAll = useMutation({
    mutationFn: async () => {
      if (target === null) throw new Error('Deploy this process first — a run needs a version to execute.');
      const valid = cases.filter((c) => validateScenario(c).length === 0);
      const results = await runSimulationBatch(valid.map((c) => simulationRequest(c, target)));
      return valid.map((scenario, index) => ({ scenario, result: results[index] })).filter((pair) => pair.result);
    },
    onSuccess: (pairs) => {
      setSuite((current) => {
        const next = { ...current };
        for (const { scenario, result } of pairs) {
          next[scenario.id] = result;
        }
        return next;
      });
    },
  });

  const start = useCallback(() => {
    if (active !== null) runOne.mutate(active);
  }, [active, runOne]);

  /* ── Transport ─────────────────────────────────────────────────────────── */

  const max = run === null ? -1 : lastIndex(run);
  const atEnd = max < 0 || cursor >= max;

  // Scrubbing pauses. Someone dragging the slider is looking for a step, and
  // having playback drag it out from under them is what makes one feel broken.
  const moveTo = useCallback(
    (index: number) => {
      if (run === null) return;
      setPlaying(false);
      setCursor(clampIndex(run, index));
    },
    [run],
  );

  const stepForward = useCallback(() => {
    if (run === null) return;
    setCursor((c) => clampIndex(run, c + 1));
  }, [run]);

  const stepBack = useCallback(() => {
    if (run === null) return;
    setPlaying(false);
    setCursor((c) => clampIndex(run, c - 1));
  }, [run]);

  const togglePlay = useCallback(() => {
    if (run === null) return;
    // Pressing play at the end restarts, rather than doing nothing — which is
    // what somebody who has just watched it through and wants to watch it again
    // expects that button to mean.
    setPlaying((current) => {
      if (!current && cursor >= lastIndex(run)) setCursor(0);
      return !current;
    });
  }, [run, cursor]);

  useEffect(() => {
    if (!playing || run === null) return;

    const end = lastIndex(run);
    if (cursor >= end) return;

    const id = window.setTimeout(() => {
      // Stopping happens here rather than in the effect body: setting state
      // synchronously while an effect runs cascades a render, and the last step
      // is the moment playback is meant to end anyway.
      const next = clampIndex(run, cursor + 1);
      setCursor(next);
      if (next >= end) setPlaying(false);
    }, STEP_INTERVAL_MS / speed);

    return () => window.clearTimeout(id);
  }, [playing, cursor, run, speed]);

  /** Arrow keys step. The transport is small on purpose; this is how you drive it. */
  useEffect(() => {
    if (run === null) return;
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      // Not while somebody is typing an answer.
      if (target !== null && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return;
      if (event.key === 'ArrowRight') {
        event.preventDefault();
        stepForward();
      } else if (event.key === 'ArrowLeft') {
        event.preventDefault();
        stepBack();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [run, stepForward, stepBack]);

  /* ── What the canvas and the panel draw ────────────────────────────────── */

  const view = useMemo<SimulationView>(() => {
    if (run === null) return EMPTY_VIEW;
    return {
      nodeStates: nodeStatesAt(run, cursor),
      flowStates: flowStatesAt(run, cursor),
      variables: variablesAt(run, cursor),
      changedKeys: changedKeysAt(run, cursor),
      clockLabel: describeClock(run, cursor),
      step: run.steps[clampIndex(run, cursor)] ?? null,
    };
  }, [run, cursor]);

  const currentVerdict = useMemo<Verdict | null>(() => (run === null ? null : verdict(run)), [run]);

  const reset = useCallback(() => {
    setRun(null);
    setCursor(0);
    setPlaying(false);
    setEditing(null);
  }, []);

  return {
    cases,
    active,
    activeId: active?.id ?? '',
    selectCase: (id: string) => {
      setActiveId(id);
      reset();
    },
    patchCase,
    addCase,
    removeCase,

    addVariable,
    setVariable,
    removeVariable,

    answered,
    answerAt,
    editing,
    startAnswering,
    stopAnswering,
    saveAnswer,
    clearAnswer,

    problems,
    runnable,
    run,
    verdict: currentVerdict,
    suite,
    error: runOne.error ?? runAll.error ?? null,
    running: runOne.isPending,
    runningAll: runAll.isPending,
    start,
    startAll: () => runAll.mutate(),
    reset,

    cursor,
    max,
    atEnd,
    playing,
    speed,
    setSpeed,
    moveTo,
    stepForward,
    stepBack,
    togglePlay,

    ...view,
  };
}

export type SimulationController = ReturnType<typeof useSimulation>;
