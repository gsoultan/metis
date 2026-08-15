import { Alert, Badge, Box, Code, Group, Stack, Text, Tooltip } from '@mantine/core';
import { ArrowDown, CircleHelp, Plus } from 'lucide-react';

import type { NodeDataFlow, Variable } from '../../domain/dataFlow';

interface DataFlowPanelProps {
  flow?: NodeDataFlow;
  /** True when the start event has no sample data to trace. */
  hasSample: boolean;
  /** Gateways read and choose; they never add anything. */
  readsOnly?: boolean;
}

/**
 * What this step receives, what it adds, and what it passes on.
 *
 * "Where did this value come from?" and "why is my variable empty?" were
 * answerable only by reading the documentation and tracing the diagram by hand.
 * The diagram already knows: this reads the same configuration the engine does
 * and says what will be in the bag at this point.
 *
 * The values shown come from the sample data on the start event, so the answer
 * is in the author's own terms rather than as abstract names.
 */
export function DataFlowPanel({ flow, hasSample, readsOnly = false }: DataFlowPanelProps) {
  if (!flow) {
    return (
      <Text size="sm" c="dimmed">
        Connect this step to the rest of the process to see what data reaches it.
      </Text>
    );
  }

  return (
    <Stack gap="lg">
      <VariableGroup
        title="Available here"
        empty="Nothing yet — this is where the process starts."
        variables={flow.before}
        icon={<ArrowDown size={14} />}
        hasSample={hasSample}
      />

      {readsOnly ? (
        <Alert variant="light" color="gray" p="xs">
          <Text size="xs">
            This step chooses which way to go. It reads the values above and adds nothing of its own.
          </Text>
        </Alert>
      ) : (
        <VariableGroup
          title="This step adds"
          empty="Nothing — the data passes through unchanged."
          variables={flow.produces}
          icon={<Plus size={14} />}
          hasSample={hasSample}
          accent="teal"
        />
      )}

      <VariableGroup
        title="Available after"
        empty="Nothing."
        variables={flow.after}
        icon={<ArrowDown size={14} />}
        hasSample={hasSample}
      />

      {!hasSample && (
        <Alert variant="light" color="blue" p="xs">
          <Text size="xs">
            Add sample data to the start event to see the values as well as the names.
          </Text>
        </Alert>
      )}
    </Stack>
  );
}

interface VariableGroupProps {
  title: string;
  empty: string;
  variables: Variable[];
  icon: React.ReactNode;
  hasSample: boolean;
  accent?: string;
}

function VariableGroup({ title, empty, variables, icon, hasSample, accent = 'gray' }: VariableGroupProps) {
  return (
    <Box>
      <Group gap={6} mb={6}>
        {icon}
        <Text size="xs" fw={700} tt="uppercase" c="dimmed">
          {title}
        </Text>
        {variables.length > 0 && (
          <Badge size="xs" variant="light" color={accent} radius="sm">
            {variables.length}
          </Badge>
        )}
      </Group>

      {variables.length === 0 ? (
        <Text size="sm" c="dimmed">
          {empty}
        </Text>
      ) : (
        <Stack gap={4}>
          {variables.map((variable) => (
            <Group key={variable.name} gap="xs" wrap="nowrap" align="baseline">
              <Code>{variable.name}</Code>

              {hasSample && variable.sample !== undefined && (
                <Text size="xs" c="dimmed" lineClamp={1}>
                  {formatSample(variable.sample)}
                </Text>
              )}

              <Text size="xs" c="dimmed" ml="auto" lineClamp={1}>
                from {variable.producedBy}
              </Text>

              {!variable.always && (
                <Tooltip
                  multiline
                  w={240}
                  label="Only some of the paths leading here set this, so it may not be there. Reading it is the commonest cause of an empty variable."
                >
                  <Badge size="xs" variant="light" color="orange" radius="sm" leftSection={<CircleHelp size={10} />}>
                    sometimes
                  </Badge>
                </Tooltip>
              )}
            </Group>
          ))}
        </Stack>
      )}
    </Box>
  );
}

/** A sample value, short enough to sit on one line. */
function formatSample(value: unknown): string {
  if (typeof value === 'string') return `"${value}"`;
  if (value === null) return 'null';
  if (typeof value === 'object') return Array.isArray(value) ? `[${value.length} items]` : '{…}';
  return String(value);
}
