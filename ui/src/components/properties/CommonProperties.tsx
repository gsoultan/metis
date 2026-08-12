import { 
  Stack, 
  Group, 
  Text, 
  ThemeIcon, 
  TextInput, 
  Textarea,
  ActionIcon, 
  Table, 
  Button, 
  Alert,
  Card,
  Checkbox,
  Box,
  Paper,
  ScrollArea,
  Modal,
  Code as MantineCode,
  CopyButton,
} from '@mantine/core';
import { 
  Plus, 
  Trash2, 
  RefreshCw, 
  Zap, 
  AlertCircle,
  Play,
  Terminal,
  Info,
} from 'lucide-react';
import { useState } from 'react';
import { useConnectors, useExecuteConnector, useExecuteScript } from '../../hooks/useProcess';

export function MappingTable({ 
  title, 
  mapping, 
  onUpdate 
}: { 
  title: string, 
  mapping: Record<string, string>, 
  onUpdate: (m: Record<string, string>) => void 
}) {
  const [newKey, setNewKey] = useState('');
  const [newVal, setNewVal] = useState('');

  const add = () => {
    if (newKey) {
      onUpdate({ ...mapping, [newKey]: newVal });
      setNewKey('');
      setNewVal('');
    }
  };

  const remove = (key: string) => {
    const next = { ...mapping };
    delete next[key];
    onUpdate(next);
  };

  return (
    <Stack gap="xs">
      <Text fw={700} size="sm">{title}</Text>
      <Table withTableBorder withColumnBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Target Key</Table.Th>
            <Table.Th>Source (JS Expression)</Table.Th>
            <Table.Th w={50}></Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {Object.entries(mapping).map(([k, v]) => (
            <Table.Tr key={k}>
              <Table.Td><Text size="xs" fw={700}>{k}</Text></Table.Td>
              <Table.Td>
                <TextInput 
                  size="xs" 
                  value={v} 
                  onChange={(e) => onUpdate({ ...mapping, [k]: e.target.value })} 
                />
              </Table.Td>
              <Table.Td>
                <ActionIcon variant="subtle" color="red" size="sm" onClick={() => remove(k)}>
                  <Trash2 size={14} />
                </ActionIcon>
              </Table.Td>
            </Table.Tr>
          ))}
          <Table.Tr>
            <Table.Td>
              <TextInput 
                placeholder="key" 
                size="xs" 
                value={newKey} 
                onChange={(e) => setNewKey(e.target.value)} 
              />
            </Table.Td>
            <Table.Td>
              <TextInput 
                placeholder="expression" 
                size="xs" 
                value={newVal} 
                onChange={(e) => setNewVal(e.target.value)} 
              />
            </Table.Td>
            <Table.Td>
              <ActionIcon variant="light" size="sm" onClick={add}>
                <Plus size={14} />
              </ActionIcon>
            </Table.Td>
          </Table.Tr>
        </Table.Tbody>
      </Table>
    </Stack>
  );
}

export function MultiInstanceConfig({ data, onUpdate }: { data: any, onUpdate: (d: any) => void }) {
  const isMulti = !!data.loopCharacteristics;
  const characteristics = data.loopCharacteristics || {};

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Group gap="xs">
          <ThemeIcon variant="light" color="indigo" radius="md">
            <RefreshCw size={18} />
          </ThemeIcon>
          <Text fw={700} size="md">Loop Characteristics</Text>
        </Group>
        <Checkbox 
          label="Multi-instance" 
          checked={isMulti} 
          onChange={(e) => {
            if (e.currentTarget.checked) {
              onUpdate({ loopCharacteristics: { isSequential: false, collection: 'items', elementVariable: 'item' } });
            } else {
              onUpdate({ loopCharacteristics: undefined });
            }
          }} 
        />
      </Group>

      {isMulti && (
        <Stack gap="sm" pl="xl">
          <Checkbox 
            label="Sequential Execution" 
            checked={characteristics.isSequential} 
            onChange={(e) => onUpdate({ loopCharacteristics: { ...characteristics, isSequential: e.currentTarget.checked } })} 
          />
          <TextInput 
            label="Collection" 
            placeholder="e.g. users" 
            description="Process variable containing a list"
            size="sm"
            value={characteristics.collection || ''}
            onChange={(e) => onUpdate({ loopCharacteristics: { ...characteristics, collection: e.target.value } })} 
          />
          <TextInput 
            label="Element Variable" 
            placeholder="e.g. user" 
            description="Variable name for current item"
            size="sm"
            value={characteristics.elementVariable || ''}
            onChange={(e) => onUpdate({ loopCharacteristics: { ...characteristics, elementVariable: e.target.value } })} 
          />
          <TextInput 
            label="Completion Condition" 
            placeholder="e.g. nrOfCompletedInstances == nrOfInstances" 
            size="sm"
            value={characteristics.completionCondition || ''}
            onChange={(e) => onUpdate({ loopCharacteristics: { ...characteristics, completionCondition: e.target.value } })} 
          />
        </Stack>
      )}
    </Stack>
  );
}

export function ConnectorCatalog({ onSelect }: { onSelect: (connector: any) => void }) {
  const { data: connectorsData } = useConnectors();
  const connectors = (connectorsData as any)?.connectors || [];

  return (
    <Stack gap="md">
      <Text fw={700} size="sm">Choose a Connector</Text>
      <ScrollArea h={300}>
        <Stack gap="xs">
          {connectors.map((c: any) => (
            <Paper 
              key={c.id} 
              withBorder 
              p="sm" 
              onClick={() => onSelect(c)} 
              style={{ cursor: 'pointer' }}
              className="hover-bg-gray"
            >
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon size="lg" radius="md" color="yellow" variant="light">
                  <Zap size={20} />
                </ThemeIcon>
                <Box style={{ flex: 1 }}>
                  <Text size="sm" fw={700}>{c.name}</Text>
                  <Text size="xs" c="dimmed" lineClamp={1}>{c.description}</Text>
                </Box>
              </Group>
            </Paper>
          ))}
          {connectors.length === 0 && (
            <Text size="xs" c="dimmed" ta="center" py="xl">No connectors available in the catalog.</Text>
          )}
        </Stack>
      </ScrollArea>
    </Stack>
  );
}

export function NodeTestModal({ 
  nodeId: _nodeId, 
  data, 
  opened, 
  onClose 
}: { 
  nodeId: string, 
  data: any, 
  opened: boolean, 
  onClose: () => void 
}) {
  const executeConnector = useExecuteConnector();
  const executeScript = useExecuteScript();
  const [testVars, setTestVars] = useState('{}');
  const [result, setResult] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);

  const runTest = async () => {
    try {
      const variables = JSON.parse(testVars);
      setError(null);
      setResult(null);

      if (data.implementation === 'connector') {
        const res = await executeConnector.mutateAsync({
          connectorKey: data.connector_id,
          config: data.inputs || {},
          payload: variables,
        });
        setResult(res);
      } else if (data.implementation === 'script') {
         const res = await executeScript.mutateAsync({
           script: data.script,
           scriptFormat: data.scriptFormat || 'javascript',
           variables: { ...variables },
         });
         setResult(res);
      }
    } catch (e: any) {
      setError(e.message || 'Test execution failed');
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title="Test Execution" size="lg">
      <Stack gap="md">
        <Alert color="blue" icon={<Info size={16} />}>
          <Text size="xs">This will execute the logic in an isolated sandbox with the provided variables.</Text>
        </Alert>

        <TextInput 
          label="Test Variables (JSON)" 
          placeholder='{"key": "value"}'
          value={testVars}
          onChange={(e) => setTestVars(e.target.value)}
        />

        <Button 
          fullWidth 
          onClick={runTest} 
          loading={executeConnector.isPending || executeScript.isPending}
          leftSection={<Play size={16} />}
          color="indigo"
        >
          Run Script
        </Button>

        {error && (
          <Alert color="red" title="Error" icon={<AlertCircle size={16} />}>
            <Text size="xs">{error}</Text>
          </Alert>
        )}

        {result && (
          <Box>
            <Text size="xs" fw={700} mb={4}>Resulting Variables:</Text>
            <MantineCode block color="green" style={{ maxHeight: '200px', overflow: 'auto' }}>
              {JSON.stringify(result, null, 2)}
            </MantineCode>
          </Box>
        )}
      </Stack>
    </Modal>
  );
}

export function ApiExample({ type, id, data }: { type: string, id: string, data: any }) {
  let snippet = "";
  let title = "API Usage Example";
  const description = "Execute this node using the gobpm API or client library.";

  switch (type) {
    case 'userTask':
      title = "Complete User Task";
      snippet = `// Using REST API
POST /v1/tasks/${id}/complete
{
  "variables": {
    "approval_status": "approved",
    "comments": "Looks good!"
  }
}

// Using Go Client
err := client.CompleteTask(ctx, uuid.MustParse("${id}"), map[string]any{
    "approval_status": "approved",
    "comments": "Looks good!",
})`;
      break;
    case 'startEvent':
      title = "Start Process Instance";
      snippet = `// Using REST API
POST /v1/projects/{projectId}/processes/${data.key || 'process_key'}/start
{
  "variables": {
    "initiator": "admin",
    "request_data": "..."
  }
}

// Using Go Client
resp, err := client.StartProcess(ctx, "${data.key || 'process_key'}", map[string]any{
    "initiator": "admin",
})
`;
      break;
    case 'serviceTask':
      title = "External Worker Logic";
      snippet = `// For 'external' worker implementation
client.Subscribe("topic_name", func(task Task) (map[string]any, error) {
    // Implement logic here
    return map[string]any{"status": "ok"}, nil
})
`;
      break;
    default:
      snippet = `// Generic API details for ${type}
GET /v1/instances/{instanceId}/nodes/${id}
`;
  }

  return (
    <Card withBorder radius="md" p="xl" shadow="sm">
      <Stack gap="md">
        <Group justify="space-between" wrap="nowrap">
          <Group gap="xs">
            <ThemeIcon variant="light" color="indigo" radius="md">
              <Terminal size={18} />
            </ThemeIcon>
            <Box>
              <Text fw={700} size="sm">{title}</Text>
              <Text size="xs" c="dimmed">{description}</Text>
            </Box>
          </Group>
          <CopyButton value={snippet} timeout={2000}>
            {({ copied, copy }) => (
              <Button size="xs" color={copied ? 'teal' : 'indigo'} variant="light" onClick={copy}>
                {copied ? 'Copied' : 'Copy'}
              </Button>
            )}
          </CopyButton>
        </Group>
        
        <MantineCode block style={{ fontSize: '11px', lineHeight: 1.4 }}>
          {snippet}
        </MantineCode>
      </Stack>
    </Card>
  );
}

export const SCRIPT_TEMPLATES = [
  {
    name: "Set Variable",
    description: "Update a process variable",
    code: "setVar('status', 'approved');"
  },
  {
    name: "Conditional Logic",
    description: "If/Else block",
    code: "if (amount > 1000) {\n  setVar('isLargeOrder', true);\n} else {\n  setVar('isLargeOrder', false);\n}"
  },
  {
    name: "Math Calculation",
    description: "Perform arithmetic",
    code: "const total = amount * 1.1; // Add 10% tax\nsetVar('totalWithTax', total);"
  },
  {
    name: "String Manipulation",
    description: "Format strings",
    code: "const greeting = 'Hello, ' + (firstName || 'User');\nsetVar('fullGreeting', greeting);"
  },
  {
    name: "Date Formatting",
    description: "Current date/time",
    code: "const now = new Date().toISOString();\nsetVar('processedAt', now);"
  }
];

export function ScriptTestModal({ 
  opened, 
  onClose, 
  script,
  format
}: { 
  opened: boolean, 
  onClose: () => void, 
  script: string,
  format: string
}) {
  const [variables, setVariables] = useState('{\n  "amount": 1200,\n  "firstName": "John",\n  "lastName": "Doe"\n}');
  const [result, setResult] = useState<any>(null);
  const [error, setError] = useState<string | null>(null);
  
  const execute = useExecuteScript();

  const handleTest = async () => {
    try {
      setError(null);
      setResult(null);
      const parsedVars = JSON.parse(variables);
      const res = await execute.mutateAsync({
        script,
        scriptFormat: format,
        variables: parsedVars
      });
      setResult(res);
    } catch (e: any) {
      setError(e.message || 'Execution failed');
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title="Test Script" size="lg">
      <Stack gap="md">
        <Textarea
          label="Input Variables (JSON)"
          description="Simulate process variables for this test"
          minRows={5}
          styles={{ input: { fontFamily: 'monospace', fontSize: '11px' } }}
          value={variables}
          onChange={(e) => setVariables(e.target.value)}
        />

        <Button 
          leftSection={<Play size={16} />} 
          onClick={handleTest} 
          loading={execute.isPending}
          fullWidth
          color="indigo"
        >
          Run Script
        </Button>

        {error && (
          <Alert color="red" title="Error" icon={<AlertCircle size={16} />}>
            <Text size="xs">{error}</Text>
          </Alert>
        )}

        {result && (
          <Box>
            <Text size="xs" fw={700} mb={4}>Resulting Variables:</Text>
            <MantineCode block color="green" style={{ maxHeight: '200px', overflow: 'auto' }}>
              {JSON.stringify(result, null, 2)}
            </MantineCode>
          </Box>
        )}
      </Stack>
    </Modal>
  );
}

export function KeyValueEditor({ 
  pairs = {}, 
  onChange, 
  title, 
  description,
  keyPlaceholder = "Key",
  valuePlaceholder = "Value"
}: { 
  pairs?: Record<string, string>, 
  onChange: (p: Record<string, string>) => void,
  title: string,
  description?: string,
  keyPlaceholder?: string,
  valuePlaceholder?: string
}) {
  const entries = Object.entries(pairs || {});

  const addRow = () => {
    onChange({ ...(pairs || {}), '': '' });
  };

  const removeRow = (key: string) => {
    const next = { ...pairs };
    delete next[key];
    onChange(next);
  };

  const updateKey = (oldKey: string, newKey: string) => {
    if (oldKey === newKey) return;
    const next = { ...pairs };
    const value = next[oldKey];
    delete next[oldKey];
    next[newKey] = value;
    onChange(next);
  };

  const updateValue = (key: string, value: string) => {
    onChange({ ...(pairs || {}), [key]: value });
  };

  return (
    <Stack gap="xs">
      <Group justify="space-between" align="center">
        <Box>
            <Text size="xs" fw={700}>{title}</Text>
            {description && <Text size="10px" c="dimmed">{description}</Text>}
        </Box>
        <ActionIcon size="xs" variant="light" onClick={addRow}>
          <Plus size={12} />
        </ActionIcon>
      </Group>

      {entries.map(([k, v], i) => (
        <Group key={i} gap={4} wrap="nowrap" align="flex-start">
          <TextInput
            placeholder={keyPlaceholder}
            size="xs"
            style={{ flex: 1 }}
            value={k}
            onChange={(e) => updateKey(k, e.target.value)}
          />
          <TextInput
            placeholder={valuePlaceholder}
            size="xs"
            style={{ flex: 1 }}
            value={v}
            onChange={(e) => updateValue(k, e.target.value)}
          />
          <ActionIcon color="red" variant="subtle" size="xs" mt={4} onClick={() => removeRow(k)}>
            <Trash2 size={12} />
          </ActionIcon>
        </Group>
      ))}
      {entries.length === 0 && (
          <Text size="10px" c="dimmed" ta="center" py="xs">No entries. Click + to add.</Text>
      )}
    </Stack>
  );
}
