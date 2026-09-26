/**
 * The decisions a process step can ask, as a picker offers them.
 *
 * Once per key. A step names a decision by its key and a version separately;
 * the picker was filled from a page of the decision list, which has a row per
 * version, so a decision saved twice was offered twice — which Mantine's
 * Select refuses outright, taking the property panel down with it.
 */
export interface PickerOption {
  value: string;
  label: string;
}

/**
 * One option per key, named as the decision is. A key the step already names
 * stays an option even when no decision has it any more, so the step shows
 * what it is configured to do rather than an empty box.
 */
export function decisionOptions(decisions: { key: string; name?: string }[], chosen: string): PickerOption[] {
  const byKey = new Map<string, PickerOption>();
  for (const decision of decisions) {
    if (!byKey.has(decision.key)) byKey.set(decision.key, { value: decision.key, label: decision.name || decision.key });
  }
  if (chosen && !byKey.has(chosen)) byKey.set(chosen, { value: chosen, label: chosen });
  return [...byKey.values()];
}
