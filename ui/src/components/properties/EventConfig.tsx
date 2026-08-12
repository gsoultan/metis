import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Input, 
  SegmentedControl, 
  Divider, 
  Grid, 
  Select, 
  TextInput,
  Code as MantineCode
} from '@mantine/core';
import { Zap, Globe } from 'lucide-react';
import type { NodeConfigProps } from '../PropertyPanel';

export function EventConfig({ data, onUpdate }: NodeConfigProps) {
  const eventType = data.eventType || (data.duration ? 'timer' : data.signalName ? 'signal' : data.messageName ? 'message' : 'timer');

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="blue" radius="md">
            <Zap size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Event Trigger</Text>
        </Group>

        <Input.Wrapper 
          label="Event Trigger Type" 
          description="Select the mechanism that activates this event"
        >
          <SegmentedControl
            fullWidth
            size="sm"
            mt={5}
            data={[
              { value: 'timer', label: 'Timer' },
              { value: 'signal', label: 'Signal' },
              { value: 'message', label: 'Message' },
            ]}
            value={eventType}
            onChange={(val) => onUpdate({ eventType: val })}
          />
        </Input.Wrapper>
      </Stack>

      <Divider variant="dashed" />

      {eventType === 'timer' && (
        <Grid gap="md">
          <Grid.Col span={{ base: 12, sm: 6 }}>
            <Select
              label="Timer Type"
              description="Select the type of timer event"
              size="md"
              data={[
                { value: 'duration', label: 'Duration (Wait for)' },
                { value: 'date', label: 'Date (Wait until)' },
                { value: 'cycle', label: 'Cycle (Repeatedly)' },
              ]}
              value={data.timerType || 'duration'}
              onChange={(val) => onUpdate({ timerType: val })}
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6 }}>
            <TextInput
              label="Value"
              size="md"
              placeholder={data.timerType === 'date' ? '2026-01-01T12:00:00Z' : 'PT1H'}
              description={data.timerType === 'duration' ? 'ISO 8601 Duration (e.g. PT10M, P1D)' : 'ISO 8601 Date/Time or Expression'}
              value={data.duration || ''}
              onChange={(e) => onUpdate({ duration: e.target.value })}
            />
          </Grid.Col>
        </Grid>
      )}

      {eventType === 'signal' && (
        <TextInput
          label="Signal Identifier"
          size="md"
          placeholder="Global Signal Name"
          description="A unique name for the signal broadcast"
          value={data.signalName || ''}
          onChange={(e) => onUpdate({ signalName: e.target.value })}
          leftSection={<Zap size={14} />}
        />
      )}

      {eventType === 'message' && (
        <Stack gap="md">
          <TextInput
            label="Message Identifier"
            size="md"
            placeholder="OrderReceived"
            description="The name of the message to catch"
            value={data.messageName || ''}
            onChange={(e) => onUpdate({ messageName: e.target.value })}
            leftSection={<Globe size={14} />}
          />
          <TextInput
            label="Correlation Key"
            size="md"
            placeholder="${orderId}"
            description="Expression to match specific process instance"
            value={data.correlationKey || ''}
            onChange={(e) => onUpdate({ correlationKey: e.target.value })}
            leftSection={<MantineCode>KEY</MantineCode>}
          />
        </Stack>
      )}
    </Stack>
  );
}
