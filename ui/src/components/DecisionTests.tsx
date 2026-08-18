/**
 * The examples a decision table is expected to get right.
 *
 * A table nobody can test is a spreadsheet with extra steps. It is business
 * policy, it changes often, and the person changing it is rarely the person who
 * knows every case it was written for — so the cases live beside it and are
 * re-run whenever somebody looks.
 *
 * They are written in the same shape as the table: one column per input, one per
 * result, so writing an example is filling in a row rather than learning a
 * format.
 */
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { CircleCheck, CircleX, Play, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { v4 as uuidv4 } from 'uuid';

import type { DecisionInputColumn, DecisionOutputColumn } from '../domain/decisionTable';
import type { DecisionTestRow } from '../domain/decisionTests';
import { useRunDecisionTests } from '../hooks/useDecisions';
import type { ApiDecisionTestResult } from '../services/domains/decisionService';

export function DecisionTests({
  decisionId,
  inputs,
  outputs,
  tests,
  onChange,
  hasUnsavedChanges,
}: {
  /** Null until the table has been saved once — there is nothing to run against. */
  decisionId: string | null;
  inputs: DecisionInputColumn[];
  outputs: DecisionOutputColumn[];
  tests: DecisionTestRow[];
  onChange: (next: DecisionTestRow[]) => void;
  hasUnsavedChanges: boolean;
}) {
  const run = useRunDecisionTests();
  const [results, setResults] = useState<Record<string, ApiDecisionTestResult>>({});

  const addExample = () => {
    onChange([
      ...tests,
      { id: uuidv4(), name: `Example ${tests.length + 1}`, inputs: {}, expected: {} },
    ]);
  };

  const update = (index: number, change: Partial<DecisionTestRow>) => {
    onChange(tests.map((test, i) => (i === index ? { ...test, ...change } : test)));
  };

  const runAll = async () => {
    if (!decisionId) return;
    const outcome = await run.mutateAsync(decisionId);
    const byId: Record<string, ApiDecisionTestResult> = {};
    for (const result of outcome) byId[result.id] = result;
    setResults(byId);
  };

  const passed = Object.values(results).filter((result) => result.passed).length;
  const total = Object.keys(results).length;

  return (
    <Stack gap="sm">
      <Group justify="space-between" wrap="nowrap">
        <div>
          <Text size="sm" fw={600}>
            Examples
          </Text>
          <Text size="xs" c="dimmed">
            Cases this table should get right. Re-run them after every change.
          </Text>
        </div>
        <Group gap="xs">
          {total > 0 && (
            <Badge variant="light" color={passed === total ? 'green' : 'red'}>
              {passed}/{total} passing
            </Badge>
          )}
          <Button size="xs" variant="light" leftSection={<Plus size={14} />} onClick={addExample}>
            Add
          </Button>
          <Tooltip
            label={decisionId ? 'Run every example against the saved table' : 'Save the table first'}
            withArrow
          >
            <Button
              size="xs"
              color="orange"
              leftSection={<Play size={14} />}
              onClick={runAll}
              loading={run.isPending}
              disabled={!decisionId || tests.length === 0}
            >
              Run
            </Button>
          </Tooltip>
        </Group>
      </Group>

      {/* The examples run against what is *saved*, not what is on screen. Saying
          so is the difference between a confusing result and an obvious one. */}
      {hasUnsavedChanges && total > 0 && (
        <Alert variant="light" color="yellow" py={4}>
          <Text size="xs">These results are from the last saved version. Save to re-run against your changes.</Text>
        </Alert>
      )}

      {tests.length === 0 ? (
        <Text size="xs" c="dimmed">
          None yet. An example is a row of inputs and the result you expect from them.
        </Text>
      ) : (
        <Table verticalSpacing={4} horizontalSpacing="xs" withColumnBorders>
          <Table.Thead>
            <Table.Tr>
              <Table.Th w={28} />
              <Table.Th miw={140}>Example</Table.Th>
              {inputs.map((input) => (
                <Table.Th key={input.id} miw={110}>
                  <Text size="xs" fw={700} c="blue.9">
                    GIVEN
                  </Text>
                  <Text size="xs">{input.label || input.expression}</Text>
                </Table.Th>
              ))}
              {outputs.map((output) => (
                <Table.Th key={output.id} miw={110}>
                  <Text size="xs" fw={700} c="teal.9">
                    EXPECT
                  </Text>
                  <Text size="xs">{output.label || output.name}</Text>
                </Table.Th>
              ))}
              <Table.Th w={32} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {tests.map((test, index) => {
              const result = results[test.id];
              return (
                <Table.Tr key={test.id}>
                  <Table.Td>
                    {result && (
                      <Tooltip
                        label={result.err || result.mismatches?.join('; ') || 'Passed'}
                        multiline
                        w={260}
                        withArrow
                      >
                        {result.passed ? (
                          <CircleCheck size={16} color="var(--mantine-color-green-6)" />
                        ) : (
                          <CircleX size={16} color="var(--mantine-color-red-6)" />
                        )}
                      </Tooltip>
                    )}
                  </Table.Td>
                  <Table.Td p={0}>
                    <TextInput
                      variant="unstyled"
                      px="xs"
                      aria-label="Example name"
                      placeholder="What this checks"
                      value={test.name}
                      onChange={(event) => update(index, { name: event.currentTarget.value })}
                      styles={{ input: { fontSize: 12 } }}
                    />
                  </Table.Td>
                  {inputs.map((input) => (
                    <Table.Td key={input.id} p={0}>
                      <TextInput
                        variant="unstyled"
                        px="xs"
                        aria-label={`${input.label || input.expression} for ${test.name}`}
                        value={test.inputs[input.expression] ?? ''}
                        onChange={(event) =>
                          update(index, {
                            inputs: { ...test.inputs, [input.expression]: event.currentTarget.value },
                          })
                        }
                        styles={{ input: { fontSize: 12 } }}
                      />
                    </Table.Td>
                  ))}
                  {outputs.map((output) => (
                    <Table.Td key={output.id} p={0}>
                      <TextInput
                        variant="unstyled"
                        px="xs"
                        aria-label={`Expected ${output.label || output.name} for ${test.name}`}
                        value={test.expected[output.name] ?? ''}
                        onChange={(event) =>
                          update(index, {
                            expected: { ...test.expected, [output.name]: event.currentTarget.value },
                          })
                        }
                        styles={{ input: { fontSize: 12, fontWeight: 600 } }}
                      />
                    </Table.Td>
                  ))}
                  <Table.Td>
                    <ActionIcon
                      aria-label={`Delete the example ${test.name}`}
                      size="sm"
                      variant="subtle"
                      color="red"
                      onClick={() => onChange(tests.filter((_, i) => i !== index))}
                    >
                      <Trash2 size={13} />
                    </ActionIcon>
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      )}

      {Object.values(results).some((result) => !result.passed) && (
        <Stack gap={2}>
          {Object.values(results)
            .filter((result) => !result.passed)
            .map((result) => (
              <Text key={result.id} size="xs" c="red.7">
                {result.name}: {result.err || result.mismatches?.join('; ')}
              </Text>
            ))}
        </Stack>
      )}
    </Stack>
  );
}
