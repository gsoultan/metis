import { useState } from 'react';
import { 
  Paper, 
  Stack, 
  Group, 
  Text, 
  SegmentedControl, 
  Grid, 
  Select, 
  TextInput, 
  Box, 
  Autocomplete, 
  Tooltip, 
  ActionIcon,
  Button,
  Code as MantineCode 
} from '@mantine/core';
import { Variable, HelpCircle, Zap, Plus, Trash2 } from 'lucide-react';

export function HelpTooltip({ label, link }: { label: string, link?: string }) {
  return (
    <Tooltip 
      label={
        <Stack gap={4}>
          <Text size="xs">{label}</Text>
          {link && <Text size="xs" c="blue.4" td="underline">Learn More</Text>}
        </Stack>
      } 
      multiline 
      w={220} 
      withArrow
    >
      <ActionIcon 
        variant="subtle" 
        size="xs" 
        color="gray"
        component={link ? 'a' : 'div'}
        href={link}
        target="_blank"
      >
        <HelpCircle size={14} />
      </ActionIcon>
    </Tooltip>
  );
}

export function VariablePicker({ 
  value, 
  onChange, 
  label, 
  description,
  placeholder = "Select or type variable",
  required = false
}: { 
  value: string, 
  onChange: (val: string) => void,
  label: string,
  description?: string,
  placeholder?: string,
  required?: boolean
}) {
  const commonVariables = ['amount', 'status', 'approvalStatus', 'initiator', 'retryCount'];

  return (
    <Autocomplete
      label={
        <Group gap={4} wrap="nowrap">
          <Text size="sm" fw={500}>{label}</Text>
          <HelpTooltip label="Select an existing variable or type a new one." />
        </Group>
      }
      description={description}
      placeholder={placeholder}
      data={commonVariables}
      value={value}
      onChange={onChange}
      required={required}
      leftSection={<Variable size={14} />}
    />
  );
}

export function VisualConditionBuilder({ 
  condition = '', 
  onChange,
  title = "CONDITION BUILDER"
}: { 
  condition?: string, 
  onChange: (c: string) => void,
  title?: string
}) {
  const isJS = condition?.startsWith('js:');
  const raw = isJS ? condition.substring(3) : condition;
  
  const parts = raw?.split(/\s*(==|!=|>|<|>=|<=|=)\s*/) || [];
  const initialVar = parts[0] || '';
  const initialOp = parts[1] || '==';
  const initialVal = parts[2] || '';

  const [variable, setVariable] = useState(initialVar);
  const [operator, setOperator] = useState(initialOp === '=' ? '==' : initialOp);
  const [value, setValue] = useState(initialVal);
  const [mode, setMode] = useState(isJS ? 'js' : 'simple');

  const update = (v: string, o: string, val: string, m: string) => {
    if (m === 'simple' && o === '==') {
       onChange(`${v}=${val}`);
    } else {
       onChange(`js:${v} ${o} ${val}`);
    }
  };

  return (
    <Paper withBorder p="md" bg="gray.0" radius="md">
      <Stack gap="sm">
        <Group justify="space-between">
          <Text size="xs" fw={700} c="dimmed">{title}</Text>
          <SegmentedControl 
            size="xs" 
            data={[{label: 'Basic', value: 'simple'}, {label: 'Advanced (JS)', value: 'js'}]} 
            value={mode}
            onChange={(v) => {
              setMode(v as string);
              update(variable, operator, value, v as string);
            }}
          />
        </Group>
        
        <Grid gap="xs" align="flex-end">
          <Grid.Col span={5}>
            <VariablePicker 
              label="Variable" 
              value={variable} 
              onChange={(v) => {
                setVariable(v);
                update(v, operator, value, mode);
              }}
            />
          </Grid.Col>
          <Grid.Col span={3}>
            <Select
              label="Operator"
              data={[
                {value: '==', label: 'is equal to'},
                {value: '!=', label: 'is not equal to'},
                {value: '>', label: 'is greater than'},
                {value: '<', label: 'is less than'},
                {value: '>=', label: 'is greater or equal'},
                {value: '<=', label: 'is less or equal'},
              ]}
              value={operator}
              onChange={(v) => {
                setOperator(v || '==');
                update(variable, v || '==', value, mode);
              }}
            />
          </Grid.Col>
          <Grid.Col span={4}>
            <TextInput
              label="Value"
              placeholder="e.g. 1000 or 'approved'"
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                update(variable, operator, e.target.value, mode);
              }}
            />
          </Grid.Col>
        </Grid>
        
        <Box p="xs" bg="white" style={{ border: '1px dashed var(--mantine-color-gray-3)', borderRadius: '4px' }}>
          <Text size="xs" c="dimmed">Resulting expression:</Text>
          <MantineCode block>{condition || '(empty)'}</MantineCode>
        </Box>
      </Stack>
    </Paper>
  );
}

export function VisualDataMapper({
  mappings = [],
  onUpdate,
  sourceOptions = [],
  targetOptions = [],
  title = "DATA MAPPING",
  sourceLabel = "Process Variable",
  targetLabel = "Target Field"
}: {
  mappings: any[],
  onUpdate: (m: any[]) => void,
  sourceOptions?: string[],
  targetOptions?: string[],
  title?: string,
  sourceLabel?: string,
  targetLabel?: string
}) {
  const addMapping = () => {
    onUpdate([...mappings, { source: '', target: '' }]);
  };

  const removeMapping = (index: number) => {
    onUpdate(mappings.filter((_, i) => i !== index));
  };

  const updateMapping = (index: number, key: string, value: string) => {
    const next = [...mappings];
    next[index] = { ...next[index], [key]: value };
    onUpdate(next);
  };

  return (
    <Paper withBorder p="md" bg="gray.0" radius="md">
      <Stack gap="sm">
        <Group justify="space-between">
          <Text size="xs" fw={700} c="dimmed">{title}</Text>
          <Button variant="subtle" size="xs" leftSection={<Plus size={14} />} onClick={addMapping}>
            Add Mapping
          </Button>
        </Group>

        {mappings.length === 0 ? (
          <Box py="md" style={{ textAlign: 'center', border: '1px dashed var(--mantine-color-gray-3)', borderRadius: '4px' }}>
            <Text size="xs" c="dimmed">No mappings defined yet.</Text>
          </Box>
        ) : (
          <Stack gap="xs">
            {mappings.map((m, i) => (
              <Group key={i} grow align="flex-end" gap="xs">
                <Autocomplete
                  label={i === 0 ? sourceLabel : null}
                  placeholder="Source variable"
                  data={sourceOptions}
                  value={m.source}
                  onChange={(v) => updateMapping(i, 'source', v)}
                  size="xs"
                />
                <Box style={{ flex: '0 0 20px', textAlign: 'center', marginBottom: '8px' }}>
                   <Zap size={14} color="var(--mantine-color-teal-6)" />
                </Box>
                <Autocomplete
                  label={i === 0 ? targetLabel : null}
                  placeholder="Target field"
                  data={targetOptions}
                  value={m.target}
                  onChange={(v) => updateMapping(i, 'target', v)}
                  size="xs"
                />
                <ActionIcon color="red" variant="subtle" onClick={() => removeMapping(i)} mb={4}>
                  <Trash2 size={14} />
                </ActionIcon>
              </Group>
            ))}
          </Stack>
        )}
      </Stack>
    </Paper>
  );
}
