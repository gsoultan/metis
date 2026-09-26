/**
 * A decision table as the editor holds it, and as it is saved.
 *
 * Loading and saving are written as a pair, so that saving a table nobody has
 * touched sends back what was stored. The save used to be assembled in the
 * page, and it had no examples in it: every save of an existing table replaced
 * the examples stored with it by none.
 */
import type { ApiDecision, CreateDecisionPayload } from '../services/types';
import {
  formatOutputValue,
  parseOutputValue,
  type DecisionInputColumn,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from './decisionTable';
import { testsFromPayload, testsToPayload, type DecisionTestRow } from './decisionTests';

export interface DecisionEditorState {
  name: string;
  key: string;
  hitPolicy: string;
  aggregation: string;
  /** Comma-separated, as typed. */
  requiredDecisions: string;
  inputs: DecisionInputColumn[];
  outputs: DecisionOutputColumn[];
  rules: DecisionRuleRow[];
  tests: DecisionTestRow[];
}

/** A stored decision as the editor holds it. */
export function editorStateFrom(decision: ApiDecision): DecisionEditorState {
  return {
    name: decision.name,
    key: decision.key,
    hitPolicy: decision.hit_policy || 'FIRST',
    aggregation: decision.aggregation || '',
    requiredDecisions: (decision.required_decisions || []).join(', '),
    inputs: decision.inputs || [],
    outputs: (decision.outputs || []).map((output) => ({
      id: output.id,
      label: output.label,
      name: output.name,
      type: output.type,
      values: output.values,
    })),
    rules: (decision.rules || []).map((rule) => ({
      id: rule.id,
      input_entries: rule.inputs || [],
      // Stored results carry whatever the old editor wrote, quotes included.
      output_entries: (rule.outputs || []).map((value) => formatOutputValue(value)),
      description: rule.description || '',
    })),
    tests: testsFromPayload(decision.tests),
  };
}

/** What saving the editor's table sends. */
export function decisionPayload(state: DecisionEditorState): CreateDecisionPayload {
  const { inputs, outputs } = state;
  return {
    name: state.name,
    key: state.key,
    hit_policy: state.hitPolicy,
    aggregation: state.aggregation || undefined,
    required_decisions: state.requiredDecisions
      .split(',')
      .map((entry) => entry.trim())
      .filter(Boolean),
    inputs: inputs.map((input) => ({
      id: input.id,
      label: input.label,
      expression: input.expression,
      type: input.type,
    })),
    outputs: outputs.map((output) => ({
      id: output.id,
      label: output.label,
      name: output.name,
      type: output.type,
      values: output.values?.length ? output.values : undefined,
    })),
    rules: state.rules.map((rule) => ({
      id: rule.id,
      inputs: rule.input_entries,
      description: rule.description,
      outputs: rule.output_entries.map((cell, index) => parseOutputValue(cell, outputs[index]?.type ?? 'string')),
    })),
    tests: testsToPayload(state.tests, inputs, outputs),
  };
}
