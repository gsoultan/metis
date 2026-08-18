/**
 * Decisions and what they depend on, drawn.
 *
 * A decision may require others: eligibility feeds risk band, risk band feeds
 * price. As a list that relationship is invisible — the list is alphabetical and
 * says nothing about which is decided first — and the two failures it hides are
 * both expensive. A cycle makes every process touching it fail at runtime. A
 * dependency on a decision that no longer exists does the same.
 *
 * Both are visible here before anything runs.
 */
import { Alert, Badge, Card, Group, Stack, Text, Title } from '@mantine/core';
import { AlertTriangle, ArrowRight } from 'lucide-react';

import { buildDecisionGraph, type GraphDecision } from '../domain/decisionGraph';

export function DecisionGraphView({
  decisions,
  onOpen,
}: {
  decisions: GraphDecision[];
  onOpen?: (id: string) => void;
}) {
  const graph = buildDecisionGraph(decisions);

  // Nothing depends on anything: the list is the whole truth, and a diagram of
  // unconnected boxes tells nobody anything.
  if (graph.edges.length === 0 && graph.cycles.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No decision depends on another. When one does — a risk band feeding a price, say — the chain appears here.
      </Text>
    );
  }

  const deepest = Math.max(...graph.nodes.map((node) => node.layer));
  const layers = Array.from({ length: deepest + 1 }, (_, layer) =>
    graph.nodes.filter((node) => node.layer === layer),
  );

  const requiredBy = (key: string) => graph.edges.filter((edge) => edge.from === key).map((edge) => edge.to);
  const missingAnywhere = graph.nodes.flatMap((node) => node.missing);

  return (
    <Stack gap="md">
      {graph.cycles.length > 0 && (
        <Alert variant="light" color="red" icon={<AlertTriangle size={16} />}>
          <Stack gap={2}>
            <Text size="sm" fw={500}>
              {graph.cycles.length === 1 ? 'A decision depends on itself' : 'Some decisions depend on themselves'}
            </Text>
            {graph.cycles.map((cycle) => (
              <Text key={cycle.join()} size="xs">
                {cycle.join(' → ')} → {cycle[0]}
              </Text>
            ))}
            <Text size="xs" c="dimmed">
              The engine refuses to evaluate a loop, so every process reaching any of these fails.
            </Text>
          </Stack>
        </Alert>
      )}

      {missingAnywhere.length > 0 && (
        <Alert variant="light" color="yellow" icon={<AlertTriangle size={16} />} py="xs">
          <Text size="sm">
            {missingAnywhere.length === 1 ? 'A decision requires' : 'Decisions require'}{' '}
            {[...new Set(missingAnywhere)].join(', ')}, which nothing here answers to.
          </Text>
        </Alert>
      )}

      {/*
        Laid out as columns, left to right, in the order the engine evaluates
        them. A force-directed graph would look more like a diagram and tell
        somebody less: the one thing a reader wants from this picture is what
        happens before what.
      */}
      <Group align="flex-start" gap="xl" wrap="nowrap" style={{ overflowX: 'auto' }}>
        {layers.map((nodes, layer) => (
          <Stack key={layer} gap="xs" style={{ minWidth: 200 }}>
            <Text size="xs" c="dimmed" fw={600}>
              {layer === 0 ? 'Decided first' : `Then, using the above`}
            </Text>
            {nodes.map((node) => (
              <Card
                key={node.id}
                withBorder
                radius="md"
                p="sm"
                onClick={() => onOpen?.(node.id)}
                style={{ cursor: onOpen ? 'pointer' : undefined }}
                bg={node.inCycle ? 'var(--mantine-color-red-0)' : undefined}
              >
                <Stack gap={4}>
                  <Group gap="xs" wrap="nowrap" justify="space-between">
                    <Text size="sm" fw={500} truncate>
                      {node.label}
                    </Text>
                    {node.inCycle && (
                      <Badge size="xs" color="red" variant="light">
                        loop
                      </Badge>
                    )}
                  </Group>
                  {requiredBy(node.key).length > 0 && (
                    <Group gap={4} wrap="nowrap">
                      <ArrowRight size={11} color="var(--mantine-color-dimmed)" />
                      <Text size="xs" c="dimmed" truncate>
                        {requiredBy(node.key).join(', ')}
                      </Text>
                    </Group>
                  )}
                  {node.missing.length > 0 && (
                    <Text size="xs" c="yellow.8">
                      needs {node.missing.join(', ')}
                    </Text>
                  )}
                </Stack>
              </Card>
            ))}
          </Stack>
        ))}
      </Group>
    </Stack>
  );
}

/** The graph with a heading, for a page that wants to drop it in. */
export function DecisionGraphSection({
  decisions,
  onOpen,
}: {
  decisions: GraphDecision[];
  onOpen?: (id: string) => void;
}) {
  return (
    <Card withBorder radius="lg" p="xl">
      <Stack gap="md">
        <div>
          <Title order={4}>How these decisions fit together</Title>
          <Text size="xs" c="dimmed">
            A decision may require others. This is the order they are decided in.
          </Text>
        </div>
        <DecisionGraphView decisions={decisions} onOpen={onOpen} />
      </Stack>
    </Card>
  );
}
