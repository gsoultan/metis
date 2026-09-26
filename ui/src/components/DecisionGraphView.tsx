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
import { Alert, Card, Group, Loader, Stack, Text, Title } from '@mantine/core';
import { AlertTriangle } from 'lucide-react';
import { useMemo } from 'react';

import { buildDecisionGraph, type DecisionGraph, type GraphDecision, type GraphNode } from '../domain/decisionGraph';
import { DecisionGraphLayer } from './decisions/DecisionGraphLayer';

/**
 * The graph laid out for drawing: its columns, and who requires each decision.
 *
 * Built once per list of decisions rather than on every render. It was rebuilt
 * on every keystroke in the list's search box, and each card searched every
 * edge for its own.
 */
function layoutOf(decisions: GraphDecision[]) {
  const graph = buildDecisionGraph(decisions);
  const deepest = Math.max(0, ...graph.nodes.map((node) => node.layer));
  const layers: GraphNode[][] = Array.from({ length: deepest + 1 }, () => []);
  for (const node of graph.nodes) layers[node.layer].push(node);
  const requiredBy = new Map<string, string[]>();
  for (const edge of graph.edges) requiredBy.set(edge.from, [...(requiredBy.get(edge.from) ?? []), edge.to]);
  return { graph, layers, requiredBy };
}

export function DecisionGraphView({
  decisions,
  onOpen,
}: {
  decisions: GraphDecision[];
  onOpen?: (id: string) => void;
}) {
  const { graph, layers, requiredBy } = useMemo(() => layoutOf(decisions), [decisions]);

  // Nothing depends on anything: the list is the whole truth, and a diagram of
  // unconnected boxes tells nobody anything.
  if (graph.edges.length === 0 && graph.cycles.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        No decision depends on another. When one does — a risk band feeding a price, say — the chain appears here.
      </Text>
    );
  }

  return (
    <Stack gap="md">
      <GraphWarnings graph={graph} />

      {/*
        Laid out as columns, left to right, in the order the engine evaluates
        them. A force-directed graph would look more like a diagram and tell
        somebody less: the one thing a reader wants from this picture is what
        happens before what.
      */}
      <Group align="flex-start" gap="xl" wrap="nowrap" style={{ overflowX: 'auto' }}>
        {layers.map((nodes, layer) => (
          <DecisionGraphLayer
            key={layer}
            heading={layer === 0 ? 'Decided first' : 'Then, using the above'}
            nodes={nodes}
            requiredBy={requiredBy}
            onOpen={onOpen}
          />
        ))}
      </Group>
    </Stack>
  );
}

/** Loops, and requirements nothing answers to: the two things that fail at runtime. */
function GraphWarnings({ graph }: { graph: DecisionGraph }) {
  const missingAnywhere = [...new Set(graph.nodes.flatMap((node) => node.missing))];
  return (
    <>
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
            {missingAnywhere.length === 1 ? 'A decision requires' : 'Decisions require'} {missingAnywhere.join(', ')},
            which nothing here answers to.
          </Text>
        </Alert>
      )}
    </>
  );
}

/** The graph with a heading, for a page that wants to drop it in. */
export function DecisionGraphSection({
  decisions,
  onOpen,
  loading = false,
  failed = false,
  total,
}: {
  decisions: GraphDecision[];
  onOpen?: (id: string) => void;
  loading?: boolean;
  failed?: boolean;
  /** How many decisions the project has, when that is more than were loaded. */
  total?: number;
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
        <GraphBody decisions={decisions} onOpen={onOpen} loading={loading} failed={failed} total={total} />
      </Stack>
    </Card>
  );
}

/**
 * The graph, or why there is none yet. An empty graph while the decisions are
 * still loading, or when they could not be loaded, would read as "nothing
 * depends on anything".
 */
function GraphBody({
  decisions,
  onOpen,
  loading,
  failed,
  total,
}: {
  decisions: GraphDecision[];
  onOpen?: (id: string) => void;
  loading: boolean;
  failed: boolean;
  total?: number;
}) {
  if (loading) {
    return (
      <Group gap="xs">
        <Loader size="xs" />
        <Text size="sm" c="dimmed">
          Loading the decisions…
        </Text>
      </Group>
    );
  }
  if (failed) {
    return (
      <Text size="sm" c="red.7">
        The decisions could not be loaded, so how they fit together cannot be shown.
      </Text>
    );
  }
  return (
    <Stack gap="md">
      {total !== undefined && (
        <Alert variant="light" color="yellow" icon={<AlertTriangle size={16} />} py="xs">
          <Text size="sm">
            This project has {total} decisions and the graph shows the first {decisions.length} of them, in order of
            key. A dependency on one of the others shows as missing here, and a loop through them is not found.
          </Text>
        </Alert>
      )}
      <DecisionGraphView decisions={decisions} onOpen={onOpen} />
    </Stack>
  );
}
