/**
 * The decision table editor.
 *
 * A decision table is the one artifact in this product that a non-programmer is
 * expected to own outright: pricing bands, approval limits, risk tiers. The
 * editor is therefore judged on whether that person can read the table back and
 * believe it — not on how many DMN features it exposes.
 *
 * Three things follow from that, and they are why this looks the way it does:
 *
 *  - The table gets the width. Settings sit in a rail beside it rather than in a
 *    row above it, because the grid is the document and everything else is
 *    about the grid.
 *  - The hit policy is never hidden. It used to live behind Expert Mode, so the
 *    single most consequential thing about a table — what happens when two
 *    lines both apply — was invisible and silently "the first one".
 *  - The table says what it does, in words, at the top. Anything the notation
 *    hides is spelled out: what an empty cell means, which lines can never be
 *    reached, what a ranking policy is missing.
 */
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Center,
  Code,
  Divider,
  Group,
  Kbd,
  Menu,
  Paper,
  Radio,
  ScrollArea,
  Select,
  Stack,
  Switch,
  Table,
  TagsInput,
  Text,
  TextInput,
  Title,
  Tooltip,
  rem,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useNavigate, useSearch } from '@tanstack/react-router';
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeft,
  ChevronDown,
  ChevronsUpDown,
  CircleCheck,
  FlaskConical,
  Info,
  Play,
  Plus,
  Save,
  Trash2,
} from 'lucide-react';
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent as ReactClipboardEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import { v4 as uuidv4 } from 'uuid';

import { DecisionTests } from '../components/DecisionTests';
import { PageHeader } from '../components/PageHeader';
import {
  AGGREGATIONS,
  ANY_VALUE,
  CELL_TEMPLATES,
  COLUMN_TYPES,
  HIT_POLICIES,
  applyPastedGrid,
  describeCell,
  describeTable,
  formatOutputValue,
  hitPolicyOf,
  moveRule,
  newRuleRow,
  parseClipboardGrid,
  validateCell,
  type DecisionInputColumn,
  slugVariable,
  variableFollowsLabel,
  type DecisionOutputColumn,
  type DecisionRuleRow,
} from '../domain/decisionTable';
import { findCoverageGaps, ruleForGap, type CoverageGap, type CoverageReport } from '../domain/decisionCoverage';
import { findProblems } from '../domain/decisionProblems';
import { trialTarget, trialValueOf, trialVariables, withTrialValue, type TrialValues } from '../domain/decisionTrial';
import { decisionPayload, editorStateFrom } from '../domain/decisionSave';
import type { DecisionTestRow } from '../domain/decisionTests';
import { useCreateDecision, useDecision, useDecisionImpact, useEvaluateDecision, useUpdateDecision } from '../hooks/useDecisions';
import { useAppStore } from '../store/useAppStore';

/** A caught value is `unknown`; take its message when it has one. */
function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

const RAIL_WIDTH = 340;

/**
 * What every grid cell needs to behave like a spreadsheet cell.
 *
 * Its position, so the keyboard can find its neighbours, plus the two handlers
 * that make a grid a grid: moving between cells, and accepting a block of them
 * off the clipboard.
 */
interface GridCellProps {
  'data-row': number;
  'data-col': number;
  onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => void;
  onPaste: (event: ReactClipboardEvent<HTMLElement>) => void;
}

/** A condition cell: free text, with the notation available from a menu. */
function ConditionCell({
  value,
  type,
  columnLabel,
  cellProps,
  onChange,
}: {
  value: string;
  type: string;
  columnLabel: string;
  cellProps: GridCellProps;
  onChange: (next: string) => void;
}) {
  const templates = CELL_TEMPLATES[type] ?? CELL_TEMPLATES.string;
  const problem = validateCell(value);
  const meaning = problem ?? describeCell(value, columnLabel);

  return (
    <Group gap={0} wrap="nowrap" align="center">
      <Tooltip label={meaning} openDelay={problem ? 0 : 400} position="top-start" withArrow color={problem ? 'red' : undefined}>
        <TextInput
          {...cellProps}
          variant="unstyled"
          px="sm"
          aria-label={`${columnLabel} condition`}
          aria-invalid={problem ? true : undefined}
          placeholder={ANY_VALUE}
          value={value}
          onChange={(event) => onChange(event.currentTarget.value)}
          styles={{
            input: {
              fontSize: rem(13),
              // A wavy underline rather than a red box: the cell is still being
              // typed, and a box round every half-written condition is noise.
              textDecoration: problem ? 'underline wavy var(--mantine-color-red-6)' : undefined,
            },
            root: { flex: 1 },
          }}
        />
      </Tooltip>
      <Menu position="bottom-end" shadow="md" width={260}>
        <Menu.Target>
          <ActionIcon aria-label={`Condition choices for ${columnLabel}`} size="xs" variant="subtle" color="gray" mr={4}>
            <ChevronDown size={12} />
          </ActionIcon>
        </Menu.Target>
        <Menu.Dropdown>
          <Menu.Label>{columnLabel}</Menu.Label>
          {templates.map((template) => (
            <Menu.Item key={template.value} onClick={() => onChange(template.value)}>
              <Group justify="space-between" wrap="nowrap" gap="sm">
                <Text size="xs">{template.label}</Text>
                <Code fz={10}>{template.value}</Code>
              </Group>
            </Menu.Item>
          ))}
        </Menu.Dropdown>
      </Menu>
    </Group>
  );
}

/**
 * A result cell.
 *
 * Typed rather than free-form: a result is a value the process receives, not an
 * expression, so a text column is a text box and a yes/no column is a pair of
 * choices. Nobody should have to know that `Approved` needs quotes — and under
 * the old editor, which sent the cell text through Number(), the quotes ended up
 * in the value.
 */
function ResultCell({
  value,
  type,
  columnLabel,
  allowed,
  cellProps,
  onChange,
}: {
  value: string;
  type: string;
  columnLabel: string;
  allowed?: string[];
  cellProps: GridCellProps;
  onChange: (next: string) => void;
}) {
  if (type === 'boolean') {
    return (
      <Select
        {...cellProps}
        variant="unstyled"
        size="xs"
        aria-label={`${columnLabel} result`}
        value={value === 'true' ? 'true' : value === 'false' ? 'false' : ''}
        onChange={(next) => onChange(next ?? '')}
        data={[
          { value: 'true', label: 'Yes' },
          { value: 'false', label: 'No' },
        ]}
        placeholder="—"
        styles={{ input: { textAlign: 'center', fontWeight: 600, fontSize: rem(13) } }}
      />
    );
  }

  // When the column declares its allowed values, offer exactly those: it is the
  // list the ranking policies sort by, so a value outside it ranks last and is
  // almost always a typo.
  if (allowed?.length) {
    return (
      <Select
        {...cellProps}
        variant="unstyled"
        size="xs"
        searchable
        aria-label={`${columnLabel} result`}
        value={allowed.includes(value) ? value : null}
        onChange={(next) => onChange(next ?? '')}
        data={allowed}
        placeholder="Choose"
        styles={{ input: { fontWeight: 600, fontSize: rem(13) } }}
      />
    );
  }

  return (
    <TextInput
      {...cellProps}
      variant="unstyled"
      px="sm"
      aria-label={`${columnLabel} result`}
      placeholder={type === 'number' ? '0' : 'Result'}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      styles={{ input: { fontSize: rem(13), fontWeight: 600 } }}
    />
  );
}

export function DecisionEditor({ definitionId }: { definitionId?: string }) {
  const navigate = useNavigate();
  const search = useSearch({ from: '/_authenticated/decision-editor' });
  const { expertMode, setExpertMode } = useAppStore();
  const { data: existingDef } = useDecision(definitionId || null);
  const createDecision = useCreateDecision();
  const updateDecision = useUpdateDecision();
  const evaluateDecision = useEvaluateDecision();
  const { data: impact } = useDecisionImpact(definitionId || null);
  const target = trialTarget(existingDef?.decision);

  const [name, setName] = useState(search.name || 'New Decision');
  const [key, setKey] = useState(search.key || 'new_decision');
  const [hitPolicy, setHitPolicy] = useState('FIRST');
  const [aggregation, setAggregation] = useState('');
  const [requiredDecisions, setRequiredDecisions] = useState('');
  const [inputs, setInputs] = useState<DecisionInputColumn[]>([
    { id: uuidv4(), label: 'Amount', expression: 'amount', type: 'number' },
  ]);
  const [outputs, setOutputs] = useState<DecisionOutputColumn[]>([
    { id: uuidv4(), label: 'Result', name: 'result', type: 'string' },
  ]);
  const [rules, setRules] = useState<DecisionRuleRow[]>([newRuleRow(uuidv4(), 1, 1)]);
  const [tests, setTests] = useState<DecisionTestRow[]>([]);
  // What the server holds, as the save would send it: set on load and on save.
  const [savedPayload, setSavedPayload] = useState<string | null>(null);

  const [testInputs, setTestInputs] = useState<TrialValues>({});
  const [testResult, setTestResult] = useState<Record<string, unknown> | null>(null);
  const [matchedRules, setMatchedRules] = useState<number[]>([]);
  const [isTesting, setIsTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  const gridRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const decision = existingDef?.decision;
    if (!decision) return;

    const loaded = editorStateFrom(decision);
    setName(loaded.name);
    setKey(loaded.key);
    setHitPolicy(loaded.hitPolicy);
    setAggregation(loaded.aggregation);
    setRequiredDecisions(loaded.requiredDecisions);
    setInputs(loaded.inputs);
    setOutputs(loaded.outputs);
    setRules(loaded.rules);
    setTests(loaded.tests);
    setSavedPayload(JSON.stringify(decisionPayload(loaded)));
  }, [existingDef]);

  const policy = hitPolicyOf(hitPolicy);
  const problems = useMemo(
    () => findProblems(hitPolicy, inputs, outputs, rules),
    [hitPolicy, inputs, outputs, rules],
  );
  const blocking = problems.filter((problem) => problem.severity === 'error');
  const coverage = useMemo(() => findCoverageGaps(inputs, rules), [inputs, rules]);
  const summary = describeTable(hitPolicy, aggregation, inputs, outputs, rules.length);

  const addInput = () => {
    const label = `Condition ${inputs.length + 1}`;
    const expression = slugVariable(label);
    setInputs([...inputs, { id: uuidv4(), label, expression, type: 'string' }]);
    setRules(rules.map((rule) => ({ ...rule, input_entries: [...rule.input_entries, ANY_VALUE] })));
  };

  const addOutput = () => {
    // Born with a name derived from its heading. It used to be created empty,
    // which is an error that disables Save, fixable only in Expert mode.
    const label = `Result ${outputs.length + 1}`;
    setOutputs([...outputs, { id: uuidv4(), label, name: slugVariable(label), type: 'string' }]);
    setRules(rules.map((rule) => ({ ...rule, output_entries: [...rule.output_entries, ''] })));
  };

  const addRule = () => setRules([...rules, newRuleRow(uuidv4(), inputs.length, outputs.length)]);
  const addRuleForGap = (gap: CoverageGap) => setRules([...rules, ruleForGap(gap, uuidv4(), outputs.length)]);

  const removeInput = (index: number) => {
    setInputs(inputs.filter((_, i) => i !== index));
    setRules(rules.map((rule) => ({ ...rule, input_entries: rule.input_entries.filter((_, i) => i !== index) })));
  };

  const removeOutput = (index: number) => {
    setOutputs(outputs.filter((_, i) => i !== index));
    setRules(rules.map((rule) => ({ ...rule, output_entries: rule.output_entries.filter((_, i) => i !== index) })));
  };

  // The grid is addressed by a single column number running left to right
  // across the conditions and then the results — the order the line is read in,
  // and the order a spreadsheet pastes in.
  const columnCount = inputs.length + outputs.length;

  const setCell = useCallback(
    (row: number, column: number, value: string) => {
      setRules((current) =>
        current.map((rule, index) => {
          if (index !== row) return rule;
          if (column < inputs.length) {
            const cells = [...rule.input_entries];
            cells[column] = value;
            return { ...rule, input_entries: cells };
          }
          const cells = [...rule.output_entries];
          cells[column - inputs.length] = value;
          return { ...rule, output_entries: cells };
        }),
      );
    },
    [inputs.length],
  );

  const cellValue = useCallback(
    (row: number, column: number): string => {
      const rule = rules[row];
      if (!rule) return '';
      return column < inputs.length
        ? (rule.input_entries[column] ?? '')
        : (rule.output_entries[column - inputs.length] ?? '');
    },
    [rules, inputs.length],
  );

  const focusCell = useCallback((row: number, column: number) => {
    const cell = gridRef.current?.querySelector<HTMLElement>(`[data-row="${row}"][data-col="${column}"]`);
    cell?.focus();
    if (cell instanceof HTMLInputElement) cell.select();
  }, []);

  /**
   * Cell behaviour.
   *
   * Left and right arrows are left alone: inside a cell they move the caret,
   * and a grid that jumps columns while you are editing a range is worse than
   * one that does not move at all. Up, down and Enter move between lines, which
   * is the direction a table is read in; Alt with left or right moves columns
   * for anyone who wants it.
   */
  const cellPropsFor = useCallback(
    (row: number, column: number): GridCellProps => ({
      'data-row': row,
      'data-col': column,
      onKeyDown: (event) => {
        const go = (nextRow: number, nextColumn: number) => {
          event.preventDefault();
          focusCell(nextRow, nextColumn);
        };
        switch (event.key) {
          case 'ArrowUp':
            if (row > 0) go(row - 1, column);
            break;
          case 'ArrowDown':
            if (row < rules.length - 1) go(row + 1, column);
            break;
          case 'Enter':
            if (event.shiftKey) {
              if (row > 0) go(row - 1, column);
            } else if (row < rules.length - 1) {
              go(row + 1, column);
            }
            break;
          case 'ArrowLeft':
            if (event.altKey && column > 0) go(row, column - 1);
            break;
          case 'ArrowRight':
            if (event.altKey && column < columnCount - 1) go(row, column + 1);
            break;
          case 'd':
          case 'D':
            // Fill down, the one spreadsheet shortcut people reach for without
            // being told it exists.
            if ((event.metaKey || event.ctrlKey) && row > 0) {
              event.preventDefault();
              setCell(row, column, cellValue(row - 1, column));
            }
            break;
          default:
            break;
        }
      },
      onPaste: (event) => {
        const text = event.clipboardData.getData('text/plain');
        if (!text.includes('\t') && !text.includes('\n')) return; // an ordinary paste
        event.preventDefault();
        setRules((current) =>
          applyPastedGrid(current, row, column, parseClipboardGrid(text), inputs.length, outputs.length, () => uuidv4()),
        );
      },
    }),
    [cellValue, columnCount, focusCell, inputs.length, outputs.length, rules.length, setCell],
  );

  const payload = useMemo(
    () => decisionPayload({ name, key, hitPolicy, aggregation, requiredDecisions, inputs, outputs, rules, tests }),
    [name, key, hitPolicy, aggregation, requiredDecisions, inputs, outputs, rules, tests],
  );
  const hasUnsavedChanges = JSON.stringify(payload) !== savedPayload;

  const handleSave = async () => {
    if (blocking.length > 0) {
      notifications.show({
        title: 'The table cannot be saved yet',
        message: blocking[0].message,
        color: 'red',
      });
      return;
    }

    try {
      if (definitionId) {
        await updateDecision.mutateAsync({ id: definitionId, ...payload });
        setSavedPayload(JSON.stringify(payload));
        notifications.show({ title: 'Saved', message: `${name} updated. Try it below.`, color: 'green' });
      } else {
        const created = await createDecision.mutateAsync(payload);
        notifications.show({ title: 'Saved', message: `${name} created. Try it below.`, color: 'green' });
        /*
         * Stay in the editor, now editing the table that was just created.
         *
         * Saving used to navigate back to the list. "Try it" only runs the
         * *saved* table, so the loop everybody actually wants — type a rule, try
         * it, fix it, try again — was impossible: you saved, lost your place,
         * and had to find the table and reopen it. Adopting the new id keeps
         * the page and makes the next save an update.
         */
        if (created?.id) {
          navigate({ to: '/decision-editor', search: { id: created.id }, replace: true });
        }
      }
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not save',
        message: errorMessage(err, 'Could not save the decision'),
        color: 'red',
      });
    }
  };

  const handleTest = async () => {
    if (!target) return;
    setIsTesting(true);
    setTestError(null);
    setTestResult(null);
    setMatchedRules([]);

    const variables = trialVariables(inputs, testInputs);

    try {
      const response = await evaluateDecision.mutateAsync({ key: target.key, version: target.version, variables });
      if (response.err) {
        setTestError(typeof response.err === 'string' ? response.err : JSON.stringify(response.err));
      } else {
        setTestResult(response.result?.values ?? {});
        setMatchedRules(response.matchedRules ?? []);
      }
    } catch (err: unknown) {
      setTestError(errorMessage(err, 'Could not evaluate the decision'));
    } finally {
      setIsTesting(false);
    }
  };

  const orderMatters = policy?.ordered ?? false;

  return (
    <Stack gap="lg" p="md">
      <PageHeader
        title={definitionId ? name : 'New decision table'}
        description={summary}
        meta={
          <Group gap="xs">
            <Badge variant="light" color="gray" styles={{ label: { textTransform: 'none' } }}>
              {key}
            </Badge>
            {existingDef?.decision?.version ? (
              <Badge variant="light" color="indigo">
                v{existingDef.decision.version}
              </Badge>
            ) : null}
          </Group>
        }
        actions={
          <Group gap="sm">
            <Button
              variant="subtle"
              color="gray"
              leftSection={<ArrowLeft size={16} />}
              onClick={() => navigate({ to: '/models', search: { tab: 'decisions' } })}
            >
              Back
            </Button>
            <Button
              leftSection={<Save size={16} />}
              onClick={handleSave}
              loading={createDecision.isPending || updateDecision.isPending}
              disabled={blocking.length > 0}
            >
              Save
            </Button>
          </Group>
        }
      />

      {problems.length > 0 && (
        <Stack gap="xs">
          {problems.map((problem) => (
            <Alert
              key={problem.message}
              variant="light"
              color={problem.severity === 'error' ? 'red' : 'yellow'}
              icon={problem.severity === 'error' ? <AlertCircle size={16} /> : <AlertTriangle size={16} />}
              py="xs"
            >
              <Text size="sm">{problem.message}</Text>
            </Alert>
          ))}
        </Stack>
      )}

      <Group align="flex-start" gap="lg" wrap="wrap">
        {/* The table is the document; it gets whatever width is left. */}
        <Stack gap="sm" style={{ flex: '1 1 640px', minWidth: 0 }}>
          <Paper radius="md" withBorder p={0} style={{ overflow: 'hidden' }}>
            <ScrollArea scrollbars="x" type="auto" ref={gridRef}>
              <Table withColumnBorders verticalSpacing={2} stickyHeader>
                <Table.Thead bg="var(--mantine-color-gray-0)">
                  <Table.Tr>
                    <Table.Th w={orderMatters ? 76 : 44} ta="center">
                      <Tooltip label={policy?.label ?? hitPolicy} withArrow>
                        <Badge size="xs" variant="filled" color="dark">
                          {hitPolicy.charAt(0)}
                        </Badge>
                      </Tooltip>
                    </Table.Th>

                    {inputs.map((input, index) => (
                      <ColumnHeader
                        key={input.id}
                        kind="condition"
                        label={input.label}
                        variable={input.expression}
                        type={input.type}
                        expert={expertMode}
                        onLabel={(next) =>
                          setInputs(inputs.map((c, i) =>
                            i === index
                              ? { ...c, label: next, expression: variableFollowsLabel(c.expression, c.label) ? slugVariable(next) : c.expression }
                              : c,
                          ))
                        }
                        onVariable={(next) =>
                          setInputs(inputs.map((c, i) => (i === index ? { ...c, expression: next } : c)))
                        }
                        onType={(next) => setInputs(inputs.map((c, i) => (i === index ? { ...c, type: next } : c)))}
                        onRemove={inputs.length > 1 ? () => removeInput(index) : undefined}
                      />
                    ))}

                    {outputs.map((output, index) => (
                      <ColumnHeader
                        key={output.id}
                        kind="result"
                        label={output.label}
                        variable={output.name}
                        type={output.type}
                        expert={expertMode}
                        onLabel={(next) =>
                          setOutputs(outputs.map((c, i) =>
                            i === index
                              ? { ...c, label: next, name: variableFollowsLabel(c.name, c.label) ? slugVariable(next) : c.name }
                              : c,
                          ))
                        }
                        onVariable={(next) => setOutputs(outputs.map((c, i) => (i === index ? { ...c, name: next } : c)))}
                        onType={(next) => setOutputs(outputs.map((c, i) => (i === index ? { ...c, type: next } : c)))}
                        onRemove={outputs.length > 1 ? () => removeOutput(index) : undefined}
                      />
                    ))}

                    <Table.Th miw={180}>
                      <Text size={rem(10)} fw={700} c="dimmed">
                        NOTE
                      </Text>
                    </Table.Th>
                    <Table.Th w={44} />
                  </Table.Tr>
                </Table.Thead>

                <Table.Tbody>
                  {rules.map((rule, ruleIndex) => {
                    const matched = matchedRules.includes(ruleIndex);
                    return (
                      <Table.Tr key={rule.id} bg={matched ? 'var(--mantine-color-orange-0)' : undefined}>
                        <Table.Td ta="center">
                          <Group gap={2} wrap="nowrap" justify="center">
                            {matched ? (
                              <Badge color="orange" size="xs" variant="filled">
                                {ruleIndex + 1}
                              </Badge>
                            ) : (
                              <Text size="xs" c="dimmed">
                                {ruleIndex + 1}
                              </Text>
                            )}
                            {orderMatters && (
                              <Stack gap={0}>
                                <ActionIcon
                                  aria-label={`Move line ${ruleIndex + 1} up`}
                                  size={14}
                                  variant="subtle"
                                  color="gray"
                                  disabled={ruleIndex === 0}
                                  onClick={() => setRules(moveRule(rules, ruleIndex, ruleIndex - 1))}
                                >
                                  <ChevronsUpDown size={10} style={{ transform: 'rotate(180deg)' }} />
                                </ActionIcon>
                                <ActionIcon
                                  aria-label={`Move line ${ruleIndex + 1} down`}
                                  size={14}
                                  variant="subtle"
                                  color="gray"
                                  disabled={ruleIndex === rules.length - 1}
                                  onClick={() => setRules(moveRule(rules, ruleIndex, ruleIndex + 1))}
                                >
                                  <ChevronsUpDown size={10} />
                                </ActionIcon>
                              </Stack>
                            )}
                          </Group>
                        </Table.Td>

                        {inputs.map((input, columnIndex) => (
                          <Table.Td key={`${rule.id}-in-${input.id}`} p={0}>
                            <ConditionCell
                              value={rule.input_entries[columnIndex] ?? ''}
                              type={input.type}
                              columnLabel={input.label || input.expression}
                              cellProps={cellPropsFor(ruleIndex, columnIndex)}
                              onChange={(next) => setCell(ruleIndex, columnIndex, next)}
                            />
                          </Table.Td>
                        ))}

                        {outputs.map((output, columnIndex) => (
                          <Table.Td key={`${rule.id}-out-${output.id}`} p={0} bg="var(--mantine-color-teal-0)">
                            <ResultCell
                              value={rule.output_entries[columnIndex] ?? ''}
                              type={output.type}
                              columnLabel={output.label || output.name}
                              allowed={output.values}
                              cellProps={cellPropsFor(ruleIndex, inputs.length + columnIndex)}
                              onChange={(next) => setCell(ruleIndex, inputs.length + columnIndex, next)}
                            />
                          </Table.Td>
                        ))}

                        <Table.Td p={0}>
                          <TextInput
                            variant="unstyled"
                            px="sm"
                            aria-label={`Note on line ${ruleIndex + 1}`}
                            placeholder="Why this line exists…"
                            value={rule.description ?? ''}
                            onChange={(event) =>
                              setRules(
                                rules.map((r, i) =>
                                  i === ruleIndex ? { ...r, description: event.currentTarget.value } : r,
                                ),
                              )
                            }
                            styles={{ input: { fontSize: rem(12), fontStyle: 'italic' } }}
                          />
                        </Table.Td>

                        <Table.Td ta="center">
                          <ActionIcon
                            aria-label={`Delete line ${ruleIndex + 1}`}
                            variant="subtle"
                            color="red"
                            size="sm"
                            onClick={() => setRules(rules.filter((_, i) => i !== ruleIndex))}
                          >
                            <Trash2 size={14} />
                          </ActionIcon>
                        </Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>
            </ScrollArea>

            {rules.length === 0 && (
              <Center py="xl">
                <Text size="sm" c="dimmed">
                  No lines yet. Add one to start deciding something.
                </Text>
              </Center>
            )}
          </Paper>

          <Group gap="xs">
            <Button size="xs" leftSection={<Plus size={14} />} onClick={addRule}>
              Add line
            </Button>
            <Button size="xs" variant="light" leftSection={<Plus size={14} />} onClick={addInput}>
              Add condition
            </Button>
            <Button size="xs" variant="light" color="teal" leftSection={<Plus size={14} />} onClick={addOutput}>
              Add result
            </Button>
            {orderMatters && (
              <Text size="xs" c="dimmed" ml="sm">
                Order matters here — lines are read top to bottom.
              </Text>
            )}
          </Group>

          {/* Shortcuts nobody discovers unless they are written down. */}
          <Group gap="lg" wrap="wrap">
            <Text size="xs" c="dimmed">
              <Kbd>↑</Kbd> <Kbd>↓</Kbd> move between lines
            </Text>
            <Text size="xs" c="dimmed">
              <Kbd>⌘</Kbd>+<Kbd>D</Kbd> copy the cell above
            </Text>
            <Text size="xs" c="dimmed">
              Paste a block from a spreadsheet to fill the table
            </Text>
          </Group>
        </Stack>

        {/* Everything about the table, beside the table. */}
        <Stack gap="md" style={{ flex: `0 0 ${RAIL_WIDTH}px`, maxWidth: '100%' }}>
          <Paper radius="md" withBorder p="md">
            <Stack gap="sm">
              <Title order={6}>Name</Title>
              <TextInput
                label="What this decides"
                placeholder="Loan approval"
                value={name}
                onChange={(event) => setName(event.currentTarget.value)}
                required
              />
              <TextInput
                label="Key"
                description="How a process refers to this table"
                placeholder="loan_approval"
                value={key}
                onChange={(event) => setKey(event.currentTarget.value)}
                required
              />
            </Stack>
          </Paper>

          <Paper radius="md" withBorder p="md">
            <Stack gap="sm">
              <Group gap={6}>
                <Title order={6}>When several lines match</Title>
                <Tooltip
                  label="Two lines can both apply to the same input. This is what happens then."
                  multiline
                  w={240}
                  withArrow
                >
                  <Info size={13} color="var(--mantine-color-dimmed)" />
                </Tooltip>
              </Group>

              <Radio.Group value={hitPolicy} onChange={setHitPolicy}>
                <Stack gap={6}>
                  {HIT_POLICIES.map((option) => (
                    <Radio.Card key={option.value} value={option.value} p="xs" radius="sm">
                      <Group align="flex-start" gap="xs" wrap="nowrap">
                        <Radio.Indicator size="xs" mt={2} />
                        <Stack gap={2}>
                          <Text size="sm" fw={500}>
                            {option.label}
                          </Text>
                          {hitPolicy === option.value && (
                            <>
                              <Text size="xs" c="dimmed">
                                {option.description}
                              </Text>
                              {expertMode && (
                                <Text size="xs" c="dimmed" ff="monospace">
                                  {option.dmn}
                                </Text>
                              )}
                            </>
                          )}
                        </Stack>
                      </Group>
                    </Radio.Card>
                  ))}
                </Stack>
              </Radio.Group>

              {hitPolicy === 'COLLECT' && (
                <Select
                  label="Then"
                  value={aggregation}
                  onChange={(next) => setAggregation(next ?? '')}
                  data={AGGREGATIONS}
                  allowDeselect={false}
                />
              )}

              {policy?.needsValueList && (
                <Stack gap={4}>
                  <TagsInput
                    label={`Ranking for ${outputs[0]?.label || 'the first result'}`}
                    description="Most important first. This is what the policy sorts by."
                    placeholder="Add a value and press Enter"
                    value={outputs[0]?.values ?? []}
                    onChange={(values) => setOutputs(outputs.map((c, i) => (i === 0 ? { ...c, values } : c)))}
                    error={outputs[0]?.values?.length ? undefined : 'Required by this policy'}
                  />
                </Stack>
              )}
            </Stack>
          </Paper>

          <CoverageCard report={coverage} ruleCount={rules.length} onAddLine={addRuleForGap} />

          <Paper radius="md" withBorder p="md">
            <Stack gap="sm">
              <Group gap={6}>
                <FlaskConical size={15} color="var(--mantine-color-orange-6)" />
                <Title order={6}>Try it</Title>
              </Group>
              <Text size="xs" c="dimmed">
                {target
                  ? `Runs the saved version (v${target.version}) and highlights the lines that matched.`
                  : 'Save the table first: Try it runs the saved version.'}
              </Text>

              {inputs.map((input) =>
                input.type === 'boolean' ? (
                  <Select
                    key={input.id}
                    size="xs"
                    label={input.label}
                    value={trialValueOf(testInputs, input)}
                    onChange={(next) => setTestInputs(withTrialValue(testInputs, input, next ?? ''))}
                    data={[
                      { value: 'true', label: 'Yes' },
                      { value: 'false', label: 'No' },
                    ]}
                  />
                ) : (
                  <TextInput
                    key={input.id}
                    size="xs"
                    label={input.label}
                    placeholder={input.type === 'number' ? '100' : 'Sample value'}
                    value={trialValueOf(testInputs, input)}
                    onChange={(event) => setTestInputs(withTrialValue(testInputs, input, event.currentTarget.value))}
                  />
                ),
              )}

              <Button
                size="xs"
                color="orange"
                leftSection={<Play size={14} />}
                onClick={handleTest}
                loading={isTesting}
                disabled={!target}
              >
                Run
              </Button>

              {testError && (
                <Alert variant="light" color="red" icon={<AlertCircle size={14} />} py="xs">
                  <Text size="xs">{testError}</Text>
                </Alert>
              )}

              {testResult && (
                <Card
                  withBorder
                  radius="sm"
                  p="xs"
                  bg={matchedRules.length === 0 ? 'var(--mantine-color-yellow-0)' : 'var(--mantine-color-gray-0)'}
                >
                  <Stack gap={6}>
                    {/*
                      Nothing matching is not a success. It used to be reported
                      under a green tick beside an empty result, so a table that
                      quietly decides nothing looked like a table that worked —
                      and the process carries on with the variable unset.
                    */}
                    <Group gap={6}>
                      {matchedRules.length === 0 ? (
                        <AlertCircle size={14} color="var(--mantine-color-yellow-7)" />
                      ) : (
                        <CircleCheck size={14} color="var(--mantine-color-green-6)" />
                      )}
                      <Text size="xs" fw={600}>
                        {matchedRules.length === 0
                          ? 'No line matched'
                          : `Line ${matchedRules.map((index) => index + 1).join(', ')} matched`}
                      </Text>
                    </Group>

                    {matchedRules.length === 0 ? (
                      <Text size="xs" c="dimmed">
                        The process would get no value for{' '}
                        {outputs.map((output) => output.label).filter(Boolean).join(', ') || 'this table'}
                        . Add a line that catches this case, or a catch-all line at the bottom.
                      </Text>
                    ) : (
                      /* Results named the way the columns are, rather than raw JSON. */
                      <Stack gap={2}>
                        {outputs.map((output) => (
                          <Group key={output.id} gap={6} wrap="nowrap">
                            <Text size="xs" c="dimmed">{output.label || output.name}:</Text>
                            <Text size="xs" fw={600}>
                              {formatOutputValue(testResult[output.name])}
                            </Text>
                          </Group>
                        ))}
                      </Stack>
                    )}
                  </Stack>
                </Card>
              )}
            </Stack>
          </Paper>

          {/* The examples were stored with the table and shown nowhere, and
              every save wiped them. They are saved with the table now, and run
              against the saved version. */}
          <Paper radius="md" withBorder p="md">
            <DecisionTests
              decisionId={definitionId ?? null}
              inputs={inputs}
              outputs={outputs}
              tests={tests}
              onChange={setTests}
              hasUnsavedChanges={hasUnsavedChanges}
            />
          </Paper>

          {/* Who else this table decides for. A threshold changed with three
              hundred instances part-way through the process that reads it is a
              different act from one changed on a table nothing uses, and the
              difference belongs on screen before the change, not after. */}
          {impact && (impact.processes?.length ?? 0) > 0 && (
            <Paper radius="md" withBorder p="md">
              <Stack gap="xs">
                <Title order={6}>Used by</Title>
                {impact.processes?.map((process) => (
                  <Group key={process.definition_id} justify="space-between" wrap="nowrap" gap="xs">
                    <div style={{ minWidth: 0 }}>
                      <Text size="sm" truncate>
                        {process.definition_name || process.definition_key}
                      </Text>
                      {process.steps?.length ? (
                        <Text size="xs" c="dimmed" truncate>
                          at {process.steps.join(', ')}
                        </Text>
                      ) : null}
                    </div>
                    {process.running_instances > 0 && (
                      <Badge size="xs" variant="light" color="orange" style={{ flexShrink: 0 }}>
                        {process.running_instances} running
                      </Badge>
                    )}
                  </Group>
                ))}
                {impact.running_instances > 0 && (
                  <Text size="xs" c="dimmed">
                    {impact.running_instances} process{impact.running_instances === 1 ? '' : 'es'} started under the
                    current version and not yet finished.
                  </Text>
                )}
              </Stack>
            </Paper>
          )}

          <Paper radius="md" withBorder p="md">
            <Stack gap="sm">
              <Group justify="space-between">
                <Title order={6}>Advanced</Title>
                <Switch
                  size="xs"
                  label="Expert mode"
                  checked={expertMode}
                  onChange={(event) => setExpertMode(event.currentTarget.checked)}
                />
              </Group>
              <Divider />
              <TextInput
                size="xs"
                label="Depends on"
                description="Other decision keys this one needs, comma separated"
                placeholder="risk_band, region"
                value={requiredDecisions}
                onChange={(event) => setRequiredDecisions(event.currentTarget.value)}
              />
            </Stack>
          </Paper>
        </Stack>
      </Group>
    </Stack>
  );
}

/** One column heading: its name, and — in expert mode — its wiring. */
function ColumnHeader({
  kind,
  label,
  variable,
  type,
  expert,
  onLabel,
  onVariable,
  onType,
  onRemove,
}: {
  kind: 'condition' | 'result';
  label: string;
  variable: string;
  type: string;
  expert: boolean;
  onLabel: (next: string) => void;
  onVariable: (next: string) => void;
  onType: (next: string) => void;
  onRemove?: () => void;
}) {
  const isCondition = kind === 'condition';
  const accent = isCondition ? 'blue' : 'teal';

  return (
    <Table.Th
      bg={`var(--mantine-color-${accent}-0)`}
      miw={170}
      style={{ borderBottom: `2px solid var(--mantine-color-${accent}-4)` }}
    >
      <Stack gap={4}>
        <Group justify="space-between" wrap="nowrap" gap={4}>
          <Text size={rem(10)} fw={700} c={`${accent}.9`}>
            {isCondition ? 'IF' : 'THEN'}
          </Text>
          {onRemove && (
            <ActionIcon aria-label={`Remove the ${label} column`} size="xs" variant="subtle" color="red" onClick={onRemove}>
              <Trash2 size={10} />
            </ActionIcon>
          )}
        </Group>

        <TextInput
          variant="unstyled"
          size="xs"
          fw={700}
          aria-label={`${isCondition ? 'Condition' : 'Result'} column name`}
          placeholder="Name this column"
          value={label}
          onChange={(event) => onLabel(event.currentTarget.value)}
        />

        {/*
          The process variable is never hidden.
          
          It used to appear only in Expert mode, as a badge reading "unnamed"
          otherwise. But an unnamed result column is an *error* that disables
          Save, and the only control that fixed it was behind Advanced → Expert:
          a novice adding their first result column reached a dead end with no
          way out of it. It fills itself in from the column name, so most people
          never have to think about it.
        */}
        <Stack gap={2}>
          <TextInput
            size="xs"
            aria-label={`Process variable for the ${label || 'new'} column`}
            placeholder="named_automatically"
            description={expert ? undefined : 'The name the process uses'}
            value={variable}
            onChange={(event) => onVariable(event.currentTarget.value)}
            error={variable.trim() === '' ? 'Needed' : undefined}
          />
          <Select
            size="xs"
            aria-label={`Type of the ${label || 'new'} column`}
            value={type}
            onChange={(next) => onType(next ?? 'string')}
            data={COLUMN_TYPES}
            allowDeselect={false}
          />
        </Stack>
      </Stack>
    </Table.Th>
  );
}

/**
 * The cases no line decides.
 *
 * A table that leaves a case undecided returns nothing for it, and the process
 * carries on with the variable unset until something downstream fails for a
 * reason that looks unrelated. The analysis was written and never shown; this
 * is where it is shown, with a way to close each gap.
 */
function CoverageCard({
  report,
  ruleCount,
  onAddLine,
}: {
  report: CoverageReport;
  ruleCount: number;
  onAddLine: (gap: CoverageGap) => void;
}) {
  return (
    <Paper radius="md" withBorder p="md">
      <Stack gap="sm">
        <Group gap={6}>
          <Title order={6}>Cases nothing decides</Title>
          <Tooltip
            label="Combinations of the values this table mentions that no line applies to. The process gets no value for them."
            multiline
            w={240}
            withArrow
          >
            <Info size={13} color="var(--mantine-color-dimmed)" />
          </Tooltip>
        </Group>
        <CoverageFindings report={report} ruleCount={ruleCount} onAddLine={onAddLine} />
      </Stack>
    </Paper>
  );
}

function CoverageFindings({
  report,
  ruleCount,
  onAddLine,
}: {
  report: CoverageReport;
  ruleCount: number;
  onAddLine: (gap: CoverageGap) => void;
}) {
  if (ruleCount === 0) {
    return (
      <Text size="xs" c="dimmed">
        Nothing to check until the table has a line.
      </Text>
    );
  }

  // Refusing to guess is reported as such, not as a clean bill of health.
  if (report.notAnalysed.length > 0) {
    return (
      <Text size="xs" c="dimmed">
        Not checked: {report.notAnalysed.join(', ')} uses a condition this check cannot read, so it cannot tell whether
        every case is decided.
      </Text>
    );
  }

  if (report.gaps.length === 0) {
    return report.truncated ? (
      <Text size="xs" c="dimmed">
        No undecided case turned up, but the table has more combinations than this check tries.
      </Text>
    ) : (
      <Group gap={6} wrap="nowrap">
        <CircleCheck size={14} color="var(--mantine-color-green-6)" />
        <Text size="xs">Every case has a line that decides it.</Text>
      </Group>
    );
  }

  return (
    <Stack gap="xs">
      {report.gaps.map((gap) => (
        <Group key={gap.description} justify="space-between" wrap="nowrap" gap="xs" align="flex-start">
          <Text size="xs">{gap.description}.</Text>
          <Button
            size="compact-xs"
            variant="light"
            leftSection={<Plus size={12} />}
            aria-label={`Add a line for this case: ${gap.description}`}
            onClick={() => onAddLine(gap)}
            style={{ flexShrink: 0 }}
          >
            Add line
          </Button>
        </Group>
      ))}
      {report.truncated && (
        <Text size="xs" c="dimmed">
          There may be more: the check stopped before trying every combination.
        </Text>
      )}
    </Stack>
  );
}
