import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  Grid, 
  Select, 
  Divider, 
  Button, 
  Tooltip, 
  Box, 
  Textarea 
} from '@mantine/core';
import { Code, Terminal, Play } from 'lucide-react';
import { useState } from 'react';
import { VariablePicker } from '../LowCodeComponents';
import { MultiInstanceConfig, ScriptTestModal, SCRIPT_TEMPLATES } from './CommonProperties';
import type { NodeConfigProps } from '../PropertyPanel';

export function ScriptTaskConfig({ data, onUpdate }: NodeConfigProps) {
  const [testModalOpened, setTestModalOpened] = useState(false);

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <Code size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Script Runtime</Text>
        </Group>

        <Grid gap="md">
          <Grid.Col span={{ base: 12, sm: 6 }}>
            <Select
              label="Execution Language"
              description="Programming language used to execute the script"
              size="md"
              data={[
                { value: 'javascript', label: 'JavaScript (Node.js/V8)' },
                { value: 'python', label: 'Python (3.x)' },
                { value: 'groovy', label: 'Groovy' },
              ]}
              value={data.scriptFormat || 'javascript'}
              onChange={(val) => onUpdate({ scriptFormat: val })}
            />
          </Grid.Col>
          <Grid.Col span={{ base: 12, sm: 6 }}>
            <VariablePicker
              label="Result Destination Variable"
              placeholder="e.g. calculationResult"
              description="Map the return value to this process variable"
              value={data.resultVariable || ''}
              onChange={(val) => onUpdate({ resultVariable: val })}
            />
          </Grid.Col>
        </Grid>
      </Stack>

      <Divider variant="dashed" />

      <Stack gap="md">
        <Group justify="space-between">
          <Group gap="xs">
            <ThemeIcon variant="light" color="blue" radius="md">
              <Terminal size={18} />
            </ThemeIcon>
            <Text fw={700} size="md">Source Code</Text>
          </Group>
          <Button 
            size="xs" 
            variant="light" 
            color="indigo" 
            leftSection={<Play size={14} />}
            onClick={() => setTestModalOpened(true)}
            disabled={!data.script}
          >
            Run Test
          </Button>
        </Group>

        <Box>
           <Text size="xs" fw={500} mb={4}>Recipes / Templates:</Text>
           <Group gap="xs" mb="md">
              {SCRIPT_TEMPLATES.map(t => (
                <Tooltip key={t.name} label={t.description}>
                  <Button 
                    variant="default" 
                    size="compact-xs" 
                    radius="xl"
                    onClick={() => {
                      const current = data.script || '';
                      onUpdate({ script: current + (current ? '\n' : '') + t.code });
                    }}
                  >
                    {t.name}
                  </Button>
                </Tooltip>
              ))}
           </Group>
        </Box>

        <Textarea
          label="Script Content"
          description="Write your business logic here. You can use setVar(name, value) to update variables."
          placeholder="// Access variables via their names, e.g. if (amount > 100) ..."
          minRows={12}
          styles={{ 
            input: { 
              fontFamily: 'monospace', 
              fontSize: '11px',
              backgroundColor: 'var(--mantine-color-dark-8)',
              color: 'var(--mantine-color-gray-3)'
            } 
          }}
          value={data.script || ''}
          onChange={(e) => onUpdate({ script: e.target.value })}
        />

        <ScriptTestModal 
          opened={testModalOpened} 
          onClose={() => setTestModalOpened(false)} 
          script={data.script || ''} 
          format={data.scriptFormat || 'javascript'} 
        />
      </Stack>

      <Divider variant="dashed" />
      <MultiInstanceConfig data={data} onUpdate={onUpdate} />
    </Stack>
  );
}
