import { Text } from '@mantine/core';

import { advancedVisibility, CHANGE_IN_EXPERT_MODE } from '../../domain/disclosure';
import { loopSummary } from '../../domain/loopSummary';
import { useAppStore } from '../../store/useAppStore';
import type { NodeConfigProps } from '../PropertyPanel';
import { MultiInstanceConfig } from './MultiInstanceConfig';
import { PropertySection } from './PropertySection';

/**
 * Whether a step repeats, and how, told the same way on every panel.
 *
 * The sentence shows in both modes, and expert mode adds the editor below it.
 * Expert mode used to swap the sentence for the editor, which then had no
 * field for a count and read an unfamiliar kind of repeat as none, so turning
 * it on showed less than basic mode: a step set to run three times showed
 * empty fields and no 3.
 */
export function LoopSettings({ data, onUpdate }: Pick<NodeConfigProps, 'data' | 'onUpdate'>) {
  const expertMode = useAppStore((state) => state.expertMode);
  const repeats = loopSummary(data);
  const shown = advancedVisibility(expertMode, repeats);
  if (shown === 'hidden') return null;

  return (
    <PropertySection title="Repeats" hint={shown === 'summary' ? CHANGE_IN_EXPERT_MODE : undefined}>
      {repeats !== undefined && <Text size="sm">{repeats}</Text>}
      {shown === 'edit' && <MultiInstanceConfig data={data} onUpdate={onUpdate} />}
    </PropertySection>
  );
}
