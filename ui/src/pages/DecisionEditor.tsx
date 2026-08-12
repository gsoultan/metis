import { 
  Stack, 
  TextInput, 
  Button, 
  Group, 
  Text, 
  Divider, 
  Paper, 
  Table, 
  Select, 
  ScrollArea, 
  Title, 
  Badge, 
  rem, 
  Code,
  Alert,
  Tabs,
  Switch,
  Tooltip as MantineTooltip,
  Center,
  ActionIcon,
  Menu,
} from '@mantine/core';
import { 
  Plus, 
  Trash2, 
  Save, 
  ArrowLeft, 
  Play,
  Settings,
  HelpCircle,
  FlaskConical,
  AlertCircle,
  CheckCircle2,
  Info,
  ChevronDown,
} from 'lucide-react';
import { useState, useEffect } from 'react';
import { v4 as uuidv4 } from 'uuid';
import { PageHeader } from '../components/PageHeader';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCreateDecision, useUpdateDecision, useDecision, useEvaluateDecision } from '../hooks/useDecisions';
import { notifications } from '@mantine/notifications';
import { useAppStore } from '../store/useAppStore';

interface DecisionInput {
  id: string;
  label: string;
  expression: string;
  type: string;
}

interface DecisionOutput {
  id: string;
  label: string;
  name: string;
  type: string;
}

interface DecisionRule {
  id: string;
  input_entries: string[];
  output_entries: string[];
  description?: string;
}

const FEEL_TEMPLATES: Record<string, { value: string; label: string }[]> = {
  string: [
    { value: '"Value"', label: 'Exact match (e.g. "Approved")' },
    { value: 'not("Value")', label: 'Does not match' },
    { value: '"A", "B"', label: 'Matches either A or B' },
    { value: '""', label: 'Empty string' },
    { value: '-', label: 'Any value (wildcard)' },
  ],
  number: [
    { value: '10', label: 'Exactly 10' },
    { value: '> 10', label: 'Greater than 10' },
    { value: '< 10', label: 'Less than 10' },
    { value: '>= 0', label: 'Zero or more' },
    { value: '[1..10]', label: 'Between 1 and 10 (incl.)' },
    { value: ']1..10]', label: 'Between 1 and 10 (excl. 1)' },
    { value: '10, 20', label: 'Either 10 or 20' },
    { value: '-', label: 'Any number (wildcard)' },
  ],
  boolean: [
    { value: 'true', label: 'True' },
    { value: 'false', label: 'False' },
  ],
  date: [
    { value: '"2024-01-01"', label: 'Exact date' },
    { value: '> "2024-01-01"', label: 'After date' },
    { value: '< "2024-01-01"', label: 'Before date' },
    { value: '-', label: 'Any date (wildcard)' },
  ]
};

const HIT_POLICIES = [
  { value: 'FIRST', label: 'First (F)', description: 'Returns result of the first matching rule.' },
  { value: 'UNIQUE', label: 'Unique (U)', description: 'Only one rule is allowed to match.' },
  { value: 'COLLECT', label: 'Collect (C)', description: 'Returns all matching results in a list.' },
  { value: 'ANY', label: 'Any (A)', description: 'Multiple matching rules must have the same output.' },
  { value: 'PRIORITY', label: 'Priority (P)', description: 'Returns the output with the highest priority.' },
];

function RuleCell({ 
  value, 
  onChange, 
  type, 
  isOutput = false, 
  placeholder 
}: { 
  value: string, 
  onChange: (val: string) => void, 
  type: string, 
  isOutput?: boolean,
  placeholder?: string
}) {
  const cellPlaceholder = placeholder || (isOutput ? "Result" : "Condition (e.g. > 10)");

  if (type === 'boolean') {
    return (
      <Select 
        variant="unstyled"
        size="xs"
        value={value === 'true' ? 'true' : value === 'false' ? 'false' : ''}
        onChange={(val) => onChange(val || '')}
        data={[
          { value: 'true', label: 'true' },
          { value: 'false', label: 'false' },
          { value: '', label: isOutput ? 'false' : '-' },
        ]}
        styles={{ 
          input: { 
            textAlign: 'center', 
            fontWeight: isOutput ? 600 : 400,
            color: isOutput ? 'var(--mantine-color-teal-9)' : 'inherit',
            fontSize: rem(13)
          } 
        }}
      />
    );
  }

  const templates = FEEL_TEMPLATES[type] || [];

  return (
    <Group gap={0} wrap="nowrap" align="center">
      <TextInput 
        variant="unstyled" 
        px="sm"
        placeholder={cellPlaceholder}
        value={value || ''} 
        onChange={(e) => onChange(e.currentTarget.value)} 
        styles={{ 
          input: { 
            fontSize: rem(13),
            fontWeight: isOutput ? 600 : 400,
            color: isOutput ? 'var(--mantine-color-teal-9)' : 'inherit',
            flex: 1
          },
          root: { flex: 1 }
        }}
      />
      {!isOutput && templates.length > 0 && (
        <Menu position="bottom-end" shadow="md" width={200}>
          <Menu.Target>
            <ActionIcon aria-label="Expand rule" size="xs" variant="subtle" color="gray" mr={4}>
              <ChevronDown size={12} />
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>FEEL Templates ({type})</Menu.Label>
            {templates.map((t) => (
              <Menu.Item key={t.value} onClick={() => onChange(t.value)}>
                <Group justify="space-between">
                  <Text size="xs" fw={500}>{t.value}</Text>
                  <Text size="xs" c="dimmed">{t.label}</Text>
                </Group>
              </Menu.Item>
            ))}
          </Menu.Dropdown>
        </Menu>
      )}
    </Group>
  );
}

export function DecisionEditor({ definitionId }: { definitionId?: string }) {
  const navigate = useNavigate();
  const search = useSearch({ from: '/_authenticated/decision-editor' }) as any;
  const { expertMode, setExpertMode } = useAppStore();
  const { data: existingDef } = useDecision(definitionId || null);
  const createDecision = useCreateDecision();
  const updateDecision = useUpdateDecision();
  const evaluateDecision = useEvaluateDecision();

  const [name, setName] = useState(search.name || 'New Decision');
  const [key, setKey] = useState(search.key || 'new_decision');
  const [hitPolicy, setHitPolicy] = useState('FIRST');
  const [aggregation, setAggregation] = useState('');
  const [requiredDecisions, setRequiredDecisions] = useState('');
  const [inputs, setInputs] = useState<DecisionInput[]>([
    { id: uuidv4(), label: 'Input 1', expression: 'input1', type: 'string' }
  ]);
  const [outputs, setOutputs] = useState<DecisionOutput[]>([
    { id: uuidv4(), label: 'Output 1', name: 'result', type: 'string' }
  ]);
  const [rules, setRules] = useState<DecisionRule[]>([
    { id: uuidv4(), input_entries: ['""'], output_entries: ['"ok"'], description: '' }
  ]);

  // Test Harness State
  const [testInputs, setTestInputs] = useState<Record<string, string>>({});
  const [testResult, setTestResult] = useState<any>(null);
  const [matchedRules, setMatchedRules] = useState<number[]>([]);
  const [isTesting, setIsTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  useEffect(() => {
    if (existingDef?.decision) {
      const d = existingDef.decision;
      setName(d.name);
      setKey(d.key);
      setHitPolicy(d.hit_policy || 'FIRST');
      setAggregation(d.aggregation || '');
      setRequiredDecisions((d.required_decisions || []).join(', '));
      setInputs(d.inputs || []);
      setOutputs(d.outputs || []);
      setRules((d.rules || []).map((r: any) => ({
        id: r.id,
        input_entries: r.inputs || [],
        output_entries: (r.outputs || []).map((v: any) => String(v)),
        description: r.description || ''
      })));
      
      // Initialize test inputs
      const initialTestInputs: Record<string, string> = {};
      d.inputs?.forEach((input: any) => {
        initialTestInputs[input.expression] = "";
      });
      setTestInputs(initialTestInputs);
    }
  }, [existingDef]);

  const addInput = () => {
    const newId = uuidv4();
    const newExpression = `input${inputs.length + 1}`;
    setInputs([...inputs, { id: newId, label: `Input ${inputs.length + 1}`, expression: newExpression, type: 'string' }]);
    setRules(rules.map(r => ({ ...r, input_entries: [...r.input_entries, '""'] })));
    setTestInputs({ ...testInputs, [newExpression]: "" });
  };
  
  const addOutput = () => {
    setOutputs([...outputs, { id: uuidv4(), label: `Output ${outputs.length + 1}`, name: '', type: 'string' }]);
    setRules(rules.map(r => ({ ...r, output_entries: [...r.output_entries, '""'] })));
  };
  const addRule = () => setRules([...rules, { 
    id: uuidv4(), 
    input_entries: Array(inputs.length).fill('""'), 
    output_entries: Array(outputs.length).fill('""'), 
    description: '' 
  }]);

  const updateRuleInput = (ruleIdx: number, inputIdx: number, val: string) => {
    const newRules = [...rules];
    newRules[ruleIdx].input_entries[inputIdx] = val;
    setRules(newRules);
  };

  const updateRuleOutput = (ruleIdx: number, outputIdx: number, val: string) => {
    const newRules = [...rules];
    newRules[ruleIdx].output_entries[outputIdx] = val;
    setRules(newRules);
  };

  const removeRule = (idx: number) => setRules(rules.filter((_, i) => i !== idx));

  const handleSave = async () => {
    const payload = { 
      name, 
      key, 
      hit_policy: hitPolicy, 
      aggregation: aggregation || undefined,
      required_decisions: requiredDecisions.split(',').map(s => s.trim()).filter(s => s !== ''),
      inputs: inputs.map(i => ({ id: i.id, label: i.label, expression: i.expression, type: i.type })),
      outputs: outputs.map(o => ({ id: o.id, label: o.label, name: o.name, type: o.type })),
      rules: rules.map(r => ({ 
        id: r.id, 
        inputs: r.input_entries, 
        description: r.description,
        outputs: r.output_entries.map(v => {
          if (v === 'true') return true;
          if (v === 'false') return false;
          const num = Number(v);
          return isNaN(num) ? v : num;
        })
      }))
    };
    try {
      if (definitionId) {
        await updateDecision.mutateAsync({ id: definitionId, ...payload });
        notifications.show({ title: 'Success', message: 'Decision table updated', color: 'green' });
      } else {
        await createDecision.mutateAsync(payload);
        notifications.show({ title: 'Success', message: 'Decision table created', color: 'green' });
      }
      navigate({ to: '/models', search: { tab: 'decisions' } });
    } catch (err: any) {
      notifications.show({ title: 'Error', message: err.message, color: 'red' });
    }
  };

  const handleTest = async () => {
    setIsTesting(true);
    setTestError(null);
    setTestResult(null);
    setMatchedRules([]);

    const variables: Record<string, any> = {};
    Object.entries(testInputs).forEach(([k, v]) => {
      try {
        if (v === 'true') variables[k] = true;
        else if (v === 'false') variables[k] = false;
        else if (!isNaN(Number(v)) && v !== '') variables[k] = Number(v);
        else variables[k] = JSON.parse(v);
      } catch {
        variables[k] = v;
      }
    });

    try {
      const res = await evaluateDecision.mutateAsync({ key, variables }) as any;
      if (res.err) {
        setTestError(typeof res.err === 'string' ? res.err : JSON.stringify(res.err));
      } else {
        setTestResult(res.result);
        if (res.matchedRuleIndexes) {
          setMatchedRules(res.matchedRuleIndexes);
        }
      }
    } catch (e: any) {
      setTestError(e.message || "Failed to evaluate decision");
    } finally {
      setIsTesting(false);
    }
  };

  const renderRuleInput = (ruleIdx: number, inputIdx: number, type: string, value: string) => (
    <RuleCell 
      value={value} 
      type={type} 
      onChange={(val) => updateRuleInput(ruleIdx, inputIdx, val)} 
    />
  );

  const renderRuleOutput = (ruleIdx: number, outputIdx: number, type: string, value: string) => (
    <RuleCell 
      value={value} 
      type={type} 
      isOutput 
      onChange={(val) => updateRuleOutput(ruleIdx, outputIdx, val)} 
    />
  );

  return (
    <Stack gap="xl" p="md">
      <PageHeader 
        title={definitionId ? `Edit Decision: ${name}` : 'Create New Decision'} 
        description="Define business rules using DMN-like decision tables."
        actions={
          <Group>
            <MantineTooltip label="Toggle Expert Mode for advanced settings">
              <Switch 
                label="Expert Mode" 
                checked={expertMode} 
                onChange={(e) => setExpertMode(e.currentTarget.checked)}
                color="indigo"
                size="sm"
              />
            </MantineTooltip>
            <Button variant="light" color="gray" leftSection={<ArrowLeft size={16} />} onClick={() => navigate({ to: '/models', search: { tab: 'decisions' } })}>Back</Button>
            <Button color="indigo" leftSection={<Save size={16} />} onClick={handleSave} loading={createDecision.isPending || updateDecision.isPending}>Save Decision</Button>
          </Group>
        }
      />

      <Tabs defaultValue="editor" variant="outline" radius="md">
        <Tabs.List>
          <Tabs.Tab value="editor" leftSection={<Settings size={14} />}>Editor</Tabs.Tab>
          <Tabs.Tab value="test" leftSection={<FlaskConical size={14} />}>Test Harness</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="editor" pt="md">
          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Stack gap="lg">
              <Group grow align="flex-start">
                <TextInput label="Decision Name" placeholder="e.g. Loan Approval" value={name} onChange={(e) => setName(e.currentTarget.value)} required />
                <TextInput label="Decision Key" placeholder="e.g. loan_approval" value={key} onChange={(e) => setKey(e.currentTarget.value)} required />
                {expertMode && (
                  <Select 
                    label="Hit Policy" 
                    value={hitPolicy} 
                    onChange={(val) => setHitPolicy(val || 'FIRST')}
                    data={HIT_POLICIES}
                    renderOption={({ option }) => {
                      const policy = HIT_POLICIES.find(p => p.value === option.value);
                      return (
                        <Stack gap={0}>
                          <Text size="sm" fw={500}>{option.label}</Text>
                          <Text size="xs" c="dimmed">{policy?.description}</Text>
                        </Stack>
                      );
                    }}
                  />
                )}
                {expertMode && hitPolicy === 'COLLECT' && (
                  <Select 
                    label="Aggregation" 
                    value={aggregation} 
                    onChange={(val) => setAggregation(val || '')}
                    data={[
                      { value: '', label: 'None (List)' },
                      { value: 'SUM', label: 'Sum (+)' },
                      { value: 'COUNT', label: 'Count (#)' },
                      { value: 'MIN', label: 'Min (<)' },
                      { value: 'MAX', label: 'Max (>)' },
                    ]}
                  />
                )}
                {expertMode && (
                  <TextInput 
                    label="Required Decisions" 
                    placeholder="key1, key2" 
                    value={requiredDecisions} 
                    onChange={(e) => setRequiredDecisions(e.currentTarget.value)}
                    description="Comma-separated keys of dependent decisions"
                  />
                )}
              </Group>

              <Divider label="Rules Grid" labelPosition="center" />
              
              <ScrollArea scrollbars="x" type="auto">
                <Table withTableBorder withColumnBorders verticalSpacing="sm">
                  <Table.Thead bg="gray.0">
                    <Table.Tr>
                      <Table.Th w={40} ta="center">
                        <MantineTooltip label={`Hit Policy: ${hitPolicy}`}>
                          <Badge size="xs" variant="filled" color="dark">{hitPolicy.charAt(0)}</Badge>
                        </MantineTooltip>
                      </Table.Th>
                      {inputs.map((input, idx) => (
                        <Table.Th key={input.id} bg="blue.0" style={{ borderBottom: '2px solid var(--mantine-color-blue-4)' }}>
                          <Stack gap={4}>
                            <Group justify="space-between" wrap="nowrap">
                              <Group gap={4}>
                                <Text size="xs" fw={700} c="blue.9">IF (INPUT)</Text>
                                <MantineTooltip label={`Condition for ${input.expression} (${input.type}). e.g. > 10, "Value", or [1..10]`}>
                                  <Info size={10} color="var(--mantine-color-blue-6)" />
                                </MantineTooltip>
                              </Group>
                              <ActionIcon aria-label="Delete rule" size="xs" variant="subtle" color="red" onClick={() => setInputs(inputs.filter((_, i) => i !== idx))}><Trash2 size={10} /></ActionIcon>
                            </Group>
                            <TextInput 
                              size="xs" 
                              variant="unstyled" 
                              fw={800} 
                              value={input.label} 
                              placeholder="Label"
                              onChange={(e) => {
                                const next = [...inputs];
                                next[idx].label = e.currentTarget.value;
                                setInputs(next);
                              }} 
                            />
                            {!expertMode && (
                               <Badge size="xs" variant="light" color="blue" fullWidth styles={{ label: { textTransform: 'none' } }}>
                                 {input.expression}
                               </Badge>
                            )}
                            {expertMode && (
                              <Stack gap={2}>
                                <TextInput size="xs" placeholder="Expression" value={input.expression} onChange={(e) => {
                                  const next = [...inputs];
                                  next[idx].expression = e.currentTarget.value;
                                  setInputs(next);
                                }} />
                                <Select size="xs" value={input.type} onChange={(val) => {
                                  const next = [...inputs];
                                  next[idx].type = val || 'string';
                                  setInputs(next);
                                }} data={['string', 'number', 'boolean', 'date']} />
                              </Stack>
                            )}
                          </Stack>
                        </Table.Th>
                      ))}
                      {outputs.map((output, idx) => (
                        <Table.Th key={output.id} bg="teal.0" style={{ borderBottom: '2px solid var(--mantine-color-teal-4)' }}>
                          <Stack gap={4}>
                            <Group justify="space-between" wrap="nowrap">
                              <Group gap={4}>
                                <Text size="xs" fw={700} c="teal.9">THEN (OUTPUT)</Text>
                                <MantineTooltip label={`Result value for ${output.name || 'result'} (${output.type}). e.g. 100, "Approved", or true`}>
                                  <Info size={10} color="var(--mantine-color-teal-6)" />
                                </MantineTooltip>
                              </Group>
                              <ActionIcon aria-label="Delete rule" size="xs" variant="subtle" color="red" onClick={() => setOutputs(outputs.filter((_, i) => i !== idx))}><Trash2 size={10} /></ActionIcon>
                            </Group>
                            <TextInput 
                              size="xs" 
                              variant="unstyled" 
                              fw={800} 
                              value={output.label} 
                              placeholder="Label"
                              onChange={(e) => {
                                const next = [...outputs];
                                next[idx].label = e.currentTarget.value;
                                setOutputs(next);
                              }} 
                            />
                            {!expertMode && (
                               <Badge size="xs" variant="light" color="teal" fullWidth styles={{ label: { textTransform: 'none' } }}>
                                 {output.name || 'result'}
                               </Badge>
                            )}
                            {expertMode && (
                               <Stack gap={2}>
                                <TextInput size="xs" placeholder="Variable Name" value={output.name} onChange={(e) => {
                                  const next = [...outputs];
                                  next[idx].name = e.currentTarget.value;
                                  setOutputs(next);
                                }} />
                                <Select size="xs" value={output.type} onChange={(val) => {
                                  const next = [...outputs];
                                  next[idx].type = val || 'string';
                                  setOutputs(next);
                                }} data={['string', 'number', 'boolean', 'date']} />
                              </Stack>
                            )}
                          </Stack>
                        </Table.Th>
                      ))}
                      <Table.Th bg="gray.0">
                        <Stack gap={4}>
                          <Group gap={4}>
                            <Text size="xs" fw={700} c="gray.7">ANNOTATION</Text>
                            <MantineTooltip label="Internal notes or comments for this rule">
                              <Info size={10} color="var(--mantine-color-gray-6)" />
                            </MantineTooltip>
                          </Group>
                          <Text size="xs">Description</Text>
                        </Stack>
                      </Table.Th>
                      <Table.Th w={50}></Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {rules.map((rule, ruleIdx) => {
                      const isMatched = matchedRules.includes(ruleIdx);
                      return (
                        <Table.Tr key={rule.id} bg={isMatched ? 'orange.0' : undefined} style={isMatched ? { outline: '1px solid var(--mantine-color-orange-4)', zIndex: 1, position: 'relative' } : undefined}>
                          <Table.Td ta="center">
                            {isMatched ? (
                              <Badge color="orange" size="xs" variant="filled">{ruleIdx + 1}</Badge>
                            ) : (
                              <Text size="xs" c="dimmed">{ruleIdx + 1}</Text>
                            )}
                          </Table.Td>
                          {inputs.map((input, inputIdx) => (
                            <Table.Td key={`in-${ruleIdx}-${inputIdx}`} p={0}>
                              {renderRuleInput(ruleIdx, inputIdx, input.type, rule.input_entries[inputIdx])}
                            </Table.Td>
                          ))}
                          {outputs.map((output, outputIdx) => (
                            <Table.Td key={`out-${ruleIdx}-${outputIdx}`} p={0}>
                              {renderRuleOutput(ruleIdx, outputIdx, output.type, rule.output_entries[outputIdx])}
                            </Table.Td>
                          ))}
                          <Table.Td p={0}>
                            <TextInput 
                              variant="unstyled" 
                              px="sm"
                              placeholder="Add a comment..."
                              value={rule.description || ''} 
                              onChange={(e) => {
                                const next = [...rules];
                                next[ruleIdx].description = e.currentTarget.value;
                                setRules(next);
                              }}
                              styles={{ input: { fontSize: rem(12), fontStyle: 'italic' } }}
                            />
                          </Table.Td>
                          <Table.Td>
                            <ActionIcon aria-label="Delete rule" variant="subtle" color="red" size="sm" onClick={() => removeRule(ruleIdx)}>
                              <Trash2 size={14} />
                            </ActionIcon>
                          </Table.Td>
                        </Table.Tr>
                      );
                    })}
                  </Table.Tbody>
                </Table>
              </ScrollArea>

              <Group>
                <Button variant="light" size="xs" leftSection={<Plus size={14} />} onClick={addRule}>Add Rule</Button>
                <Button variant="light" color="blue" size="xs" leftSection={<Plus size={14} />} onClick={addInput}>Add Input Column</Button>
                <Button variant="light" color="teal" size="xs" leftSection={<Plus size={14} />} onClick={addOutput}>Add Output Column</Button>
              </Group>
            </Stack>
          </Paper>
        </Tabs.Panel>

        <Tabs.Panel value="test" pt="md">
          <Group align="flex-start">
            <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ flex: 1 }}>
              <Stack gap="md">
                <Group justify="space-between">
                  <Group gap="xs">
                    <FlaskConical size={18} color="var(--mantine-color-orange-6)" />
                    <Title order={4}>Inputs</Title>
                  </Group>
                </Group>
                <Text size="sm" c="dimmed">Provide sample values for your input variables to simulate the decision evaluation.</Text>
                
                <Stack gap="xs">
                  {inputs.map((input) => (
                    <div key={input.id}>
                      {input.type === 'boolean' ? (
                        <Select 
                          label={input.label}
                          description={expertMode ? `Variable: ${input.expression} (boolean)` : null}
                          value={testInputs[input.expression] || ''}
                          onChange={(val) => setTestInputs({ ...testInputs, [input.expression]: val || '' })}
                          data={[
                            { value: 'true', label: 'true' },
                            { value: 'false', label: 'false' },
                          ]}
                          placeholder="Select boolean"
                        />
                      ) : (
                        <TextInput 
                          label={input.label} 
                          description={expertMode ? `Variable: ${input.expression} (${input.type})` : null}
                          placeholder={input.type === 'number' ? 'e.g. 100' : 'Sample value'} 
                          value={testInputs[input.expression] || ''}
                          onChange={(e) => setTestInputs({ ...testInputs, [input.expression]: e.currentTarget.value })}
                        />
                      )}
                    </div>
                  ))}
                </Stack>
                
                <Button 
                  variant="filled" 
                  color="orange" 
                  leftSection={<Play size={16} />} 
                  onClick={handleTest}
                  loading={isTesting}
                  mt="md"
                >
                  Run Simulation
                </Button>
              </Stack>
            </Paper>

            <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ flex: 1, minHeight: 300 }}>
              <Stack gap="md">
                <Title order={4}>Results</Title>
                <Divider />
                
                {testError && (
                  <Alert icon={<AlertCircle size={16} />} title="Evaluation Error" color="red">
                    {typeof testError === 'string' ? testError : JSON.stringify(testError)}
                  </Alert>
                )}

                {testResult ? (
                  <Stack gap="sm">
                    <Group>
                      <Badge color="green" size="lg" leftSection={<CheckCircle2 size={12} />}>Matched</Badge>
                      <Text fw={700}>Evaluation successful</Text>
                      {matchedRules.length > 0 && (
                        <Badge variant="light" color="orange">Rule {matchedRules.map(i => i + 1).join(', ')}</Badge>
                      )}
                    </Group>
                    <Paper withBorder p="md" bg="gray.0">
                      <Code block>{JSON.stringify(testResult, null, 2)}</Code>
                    </Paper>
                    <Text size="xs" c="dimmed">The result shows the output variables produced by the matching rules based on your hit policy ({hitPolicy}).</Text>
                    <Button variant="subtle" size="xs" leftSection={<ArrowLeft size={14} />} onClick={() => { setTestResult(null); setMatchedRules([]); }}>Clear Results</Button>
                  </Stack>
                ) : !testError && (
                  <Center py={60}>
                    <Stack align="center" gap="xs">
                      <HelpCircle size={40} color="var(--mantine-color-gray-4)" />
                      <Text c="dimmed">Run a simulation to see the results here.</Text>
                    </Stack>
                  </Center>
                )}
              </Stack>
            </Paper>
          </Group>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}
