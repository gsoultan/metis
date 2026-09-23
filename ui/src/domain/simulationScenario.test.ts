import { describe, expect, it } from 'bun:test';

import {
  answerFor,
  answerKindFor,
  blankAnswer,
  curlSnippet,
  describeDuration,
  emptyScenario,
  formatValue,
  goSnippet,
  isIso8601Duration,
  parseValue,
  removeAnswer,
  simulationRequest,
  upsertAnswer,
  validateScenario,
  variablesOf,
  type Scenario,
  type SimulationTarget,
} from './simulationScenario';

const deployed: SimulationTarget = {
  kind: 'deployed',
  projectId: 'proj-1',
  definitionKey: 'expense-approval',
  version: 4,
};

const scenario: Scenario = {
  id: 's1',
  name: 'Small claim',
  seed: 42,
  variables: [
    { id: 'v1', name: 'amount', value: '4200' },
    { id: 'v2', name: 'region', value: 'EU' },
  ],
  answers: [
    { nodeId: 'Approve', kind: 'person', actor: 'sarah', after: 'PT24H' },
    { nodeId: 'credit-check', kind: 'service', outcome: 'succeed', returns: [{ id: 'r1', name: 'score', value: '720' }], failureCode: '' },
  ],
};

describe('parseValue', () => {
  it('reads a value the way somebody meant it, without a JSON lesson', () => {
    expect(parseValue('4200')).toBe(4200);
    expect(parseValue('-3.5')).toBe(-3.5);
    expect(parseValue('EU')).toBe('EU');
    expect(parseValue('true')).toBe(true);
    expect(parseValue('false')).toBe(false);
    expect(parseValue('null')).toBeNull();
  });

  it('still takes JSON, for the people who need nested payloads', () => {
    expect(parseValue('{"a":1}')).toEqual({ a: 1 });
    expect(parseValue('[1,2]')).toEqual([1, 2]);
  });

  it('treats half-typed JSON as text rather than shouting mid-keystroke', () => {
    expect(parseValue('{"a":')).toBe('{"a":');
  });

  it('round-trips through formatValue', () => {
    expect(formatValue(parseValue('4200'))).toBe('4200');
    expect(formatValue(parseValue('EU'))).toBe('EU');
    expect(formatValue(parseValue('true'))).toBe('true');
  });
});

describe('variablesOf', () => {
  it('turns rows into the object the wire carries', () => {
    expect(variablesOf(scenario.variables)).toEqual({ amount: 4200, region: 'EU' });
  });

  it('ignores a row with no name instead of failing — it is a half-typed row', () => {
    expect(variablesOf([{ id: 'x', name: '  ', value: '1' }])).toEqual({});
  });
});

describe('durations', () => {
  it('shows a duration in words, so nobody has to read ISO', () => {
    expect(describeDuration('PT24H')).toBe('1 day');
    expect(describeDuration('PT5M')).toBe('5 minutes');
  });

  it('falls back to the raw value for a custom duration', () => {
    expect(describeDuration('PT13M')).toBe('PT13M');
  });

  it('still validates ISO-8601, because the engine insists on it', () => {
    expect(isIso8601Duration('PT24H')).toBe(true);
    expect(isIso8601Duration('P1D')).toBe(true);
    expect(isIso8601Duration('24h')).toBe(false);
    expect(isIso8601Duration('')).toBe(false);
  });
});

describe('answerKindFor', () => {
  it('asks a person about a task a person does', () => {
    expect(answerKindFor('userTask')).toBe('person');
    expect(answerKindFor('manualTask')).toBe('person');
  });

  it('asks what a service returns', () => {
    expect(answerKindFor('serviceTask')).toBe('service');
    expect(answerKindFor('sendTask')).toBe('service');
  });

  it('asks when a message arrives', () => {
    expect(answerKindFor('intermediateCatchEvent')).toBe('message');
    expect(answerKindFor('receiveTask')).toBe('message');
  });

  it('asks nothing of a step that runs for real', () => {
    expect(answerKindFor('exclusiveGateway')).toBeNull();
    expect(answerKindFor('scriptTask')).toBeNull();
    expect(answerKindFor('businessRuleTask')).toBeNull();
    expect(answerKindFor(undefined)).toBeNull();
  });
});

describe('answers are keyed by node', () => {
  it('finds the answer for a node', () => {
    expect(answerFor(scenario, 'Approve')?.kind).toBe('person');
    expect(answerFor(scenario, 'Nowhere')).toBeNull();
  });

  it('replaces rather than duplicates when a node is answered twice', () => {
    const once = upsertAnswer(scenario, { nodeId: 'Approve', kind: 'person', actor: 'marc', after: 'PT1H' });
    const twice = upsertAnswer(once, { nodeId: 'Approve', kind: 'person', actor: 'nina', after: 'PT4H' });
    expect(twice.answers.filter((a) => a.nodeId === 'Approve')).toHaveLength(1);
    expect(answerFor(twice, 'Approve')).toMatchObject({ actor: 'nina' });
  });

  it('removes an answer by node', () => {
    expect(answerFor(removeAnswer(scenario, 'Approve'), 'Approve')).toBeNull();
  });

  it('starts a person answer at a working day, which is the common case', () => {
    expect(blankAnswer('X', 'person')).toEqual({ nodeId: 'X', kind: 'person', actor: '', after: 'PT24H' });
  });
});

describe('validateScenario', () => {
  it('lets an empty scenario run — running it is how you find out what it needs', () => {
    expect(validateScenario(emptyScenario('x'))).toEqual([]);
  });

  it('passes a filled-in scenario', () => {
    expect(validateScenario(scenario)).toEqual([]);
  });

  it('does not demand a name, or variables, or answers', () => {
    expect(validateScenario({ ...emptyScenario('x', ''), variables: [], answers: [] })).toEqual([]);
  });

  it('refuses a duration the engine would refuse', () => {
    const bad = upsertAnswer(scenario, { nodeId: 'Approve', kind: 'person', actor: 'sarah', after: '24h' });
    expect(validateScenario(bad).some((p) => p.includes('PT24H'))).toBe(true);
  });

  it('insists a failing step names an error code, so a boundary event can catch it', () => {
    const bad = upsertAnswer(scenario, {
      nodeId: 'payment',
      kind: 'service',
      outcome: 'fail',
      returns: [],
      failureCode: '',
    });
    expect(validateScenario(bad).some((p) => p.includes('boundary event'))).toBe(true);
  });
});

describe('simulationRequest', () => {
  it('sends key and version for a deployed definition', () => {
    const request = simulationRequest(scenario, deployed);
    expect(request.definition_key).toBe('expense-approval');
    expect(request.version).toBe(4);
    expect(request.bpmn_xml).toBeUndefined();
    expect(request.variables).toEqual({ amount: 4200, region: 'EU' });
    expect(request.max_steps).toBe(500);
  });

  it('sends the diagram instead of a version for a draft', () => {
    const request = simulationRequest(scenario, {
      kind: 'draft',
      projectId: 'proj-1',
      definitionKey: 'expense-approval',
      bpmnXml: '<definitions/>',
    });
    expect(request.bpmn_xml).toBe('<definitions/>');
    expect(request.version).toBeUndefined();
  });

  it('carries the instance id when replaying a real case', () => {
    const request = simulationRequest(scenario, {
      kind: 'replay',
      projectId: 'proj-1',
      instanceId: 'inst-9',
      definitionKey: 'expense-approval',
      version: 5,
    });
    expect(request.replay_instance_id).toBe('inst-9');
  });

  it('translates answers into the wire vocabulary, keyed by node', () => {
    expect(simulationRequest(scenario, deployed).answers).toEqual([
      { node: 'Approve', kind: 'person', actor: 'sarah', after: 'PT24H' },
      { node: 'credit-check', kind: 'service', returns: { score: 720 } },
    ]);
  });

  it('sends a failing service as a code, not a payload', () => {
    const failing = upsertAnswer(scenario, {
      nodeId: 'payment',
      kind: 'service',
      outcome: 'fail',
      returns: [],
      failureCode: 'TIMEOUT',
    });
    expect(simulationRequest(failing, deployed).answers).toContainEqual({
      node: 'payment',
      kind: 'service',
      fails: 'TIMEOUT',
    });
  });
});

describe('snippets', () => {
  it('prints the same values the request carries', () => {
    const request = simulationRequest(scenario, deployed);
    const snippet = goSnippet(request, scenario);
    expect(snippet).toContain('"expense-approval"');
    expect(snippet).toContain('Version:       4');
    expect(snippet).toContain('Seed: 42');
    expect(snippet).toContain('metis.Task("Approve").CompletedBy("sarah").After("PT24H")');
    expect(snippet).toContain('metis.Service("credit-check").Returns(metis.Vars{"score": 720})');
    expect(snippet).toContain('AssertNoIncidents');
  });

  it('turns the case name into a Go test name', () => {
    const named = { ...scenario, name: 'Over limit, no manager' };
    expect(goSnippet(simulationRequest(named, deployed), named)).toContain(
      'func TestOverLimitNoManager(t *testing.T)',
    );
  });

  it('offers no snippet for a draft, because CI has nothing to point at yet', () => {
    const request = simulationRequest(scenario, {
      kind: 'draft',
      projectId: 'proj-1',
      definitionKey: 'expense-approval',
      bpmnXml: '<definitions/>',
    });
    expect(goSnippet(request, scenario)).toBeNull();
  });

  it('gives a runnable curl for anyone not writing Go', () => {
    const curl = curlSnippet(simulationRequest(scenario, deployed), 'https://metis.example.com/api/v1');
    expect(curl).toContain('POST https://metis.example.com/api/v1/simulations');
    expect(curl).toContain('"seed": 42');
  });
});
