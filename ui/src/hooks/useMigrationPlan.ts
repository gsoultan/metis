import { useEffect, useState } from 'react';

import { migrationRequestKey, type MigrationRequest } from '../domain/instanceMigration';
import { versionPair } from '../domain/migrationDraft';
import { errorMessage } from '../services/shared/errors';
import type { ApiMigrationPlan } from '../services/types';
import { usePlanInstanceMigration } from './useDefinitions';

/** One answer from the planner, kept with the request it answers. */
interface PlanAnswer {
  key: string;
  pair: string;
  plan: ApiMigrationPlan | null;
  error: string | null;
}

/**
 * The dry run for the request on screen, and whether it answers that request.
 *
 * Worked out on open and again whenever what would be sent changes, because a
 * mapping that strands a task must stop saying "ready to apply" the moment it
 * does. Each answer is kept with the request it answers, so the dialog can
 * tell the plan for what is on screen from the last one it happened to
 * receive. The last one stays on screen while the next is worked out — rather
 * than blanking the holds somebody is ticking through — but only for the same
 * two versions, and it cannot be applied.
 */
export function useMigrationPlan(request: MigrationRequest | null) {
  const preview = usePlanInstanceMigration();
  // Bumped to ask again about the same request: after an apply that stopped
  // part-way, the plan in hand counts instances that have since moved.
  const [round, setRound] = useState(0);
  const [answer, setAnswer] = useState<PlanAnswer | null>(null);
  const key = request ? `${round}|${migrationRequestKey(request)}` : null;
  const pair = request ? versionPair(request.source, request.target) : null;

  useEffect(() => {
    if (!request || key === null || pair === null) return;
    let cancelled = false;
    const answered = (plan: ApiMigrationPlan | null, error: string | null) => {
      if (!cancelled) setAnswer({ key, pair, plan, error });
    };
    preview
      .mutateAsync(request)
      .then((result) => answered(result.plan ?? null, result.err ?? null))
      .catch((error: unknown) => answered(null, errorMessage(error, 'The plan could not be worked out.')));
    return () => {
      cancelled = true;
    };
    // Keyed by what would be sent rather than by the request object, which is
    // new on every render; preview is a stable mutation object.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const shown = answer !== null && answer.pair === pair ? answer : null;
  return {
    plan: shown?.plan ?? null,
    error: shown?.error ?? null,
    fresh: answer !== null && answer.key === key,
    /** Works the plan out again for the same request. */
    replan: () => setRound((current) => current + 1),
    /** Forgets the plan, for a dialog that is closing: reopened, it plans afresh. */
    reset: () => setAnswer(null),
  };
}
