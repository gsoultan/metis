import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Select, 
  Alert 
} from '@mantine/core';
import { Zap, AlertCircle } from 'lucide-react';
import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';

export function GatewayConfig({
  data,
  onUpdate,
  selectedNode,
  edges = [],
}: NodeConfigProps) {
  // Find outgoing edges from this gateway
  const outgoingEdges = selectedNode ? edges.filter(e => e.source === selectedNode.id) : [];
  const edgeOptions = outgoingEdges.map(e => ({
    value: e.id,
    label: e.label ? `${e.label} (${e.id})` : e.id
  }));

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="orange" radius="md">
            <Zap size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Flow Control</Text>
        </Group>

        <Select
          label="Default Flow Path"
          placeholder="Select default sequence flow"
          description="The flow to use if no other conditions evaluate to true"
          size="md"
          data={edgeOptions}
          value={asText(data.defaultFlow)}
          onChange={(val) => onUpdate({ defaultFlow: val })}
          clearable
          searchable
        />
      </Stack>
      
      {edgeOptions.length === 0 && (
        <Alert color="orange" icon={<AlertCircle size={16} />} py="sm">
          <Text size="xs" fw={500}>No outgoing flows detected. Connect outgoing sequence flows to this gateway to enable default flow selection.</Text>
        </Alert>
      )}
      
      <Text size="xs" c="dimmed" style={{ fontStyle: 'italic' }}>
        Note: Default flows are critical for preventing process stalls in Exclusive (XOR) and Inclusive (OR) gateways.
      </Text>
    </Stack>
  );
}
