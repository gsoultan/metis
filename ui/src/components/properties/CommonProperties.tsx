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
import type { ApiConnector } from '../../services/types';
import { asText, asTextMap, type BPMNNodeData } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';

/** A caught value is `unknown`; take its message when it has one. */
function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

/**
 * Pairs of names: what a value is called here, and what it is called there.
 *
 * The columns were headed "Target Key" and "Source (JS Expression)". Neither is
 * what goes in them — the second is a variable name, not an expression — and
 * neither says which side is yours. Each caller now names its own two sides,
 * because "the table's input" and "the endpoint's field" are different things
 * that were both called Target.
 */
export function MappingTable({
  title,
  mapping,
  onUpdate,
  sourceLabel = 'Name here',
  targetLabel = 'Name there',
}: {
  title: string,
  mapping: Record<string, string>,
  onUpdate: (m: Record<string, string>) => void,
  sourceLabel?: string,
  targetLabel?: string,
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
      <Text size="xs" fw={700} tt="uppercase" c="dimmed">{title}</Text>
      <Table withTableBorder withColumnBorders>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>{sourceLabel}</Table.Th>
            <Table.Th>{targetLabel}</Table.Th>
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
                <ActionIcon aria-label="Delete" variant="subtle" color="red" size="sm" onClick={() => remove(k)}>
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
              <ActionIcon aria-label="Add" variant="light" size="sm" onClick={add}>
                <Plus size={14} />
              </ActionIcon>
            </Table.Td>
          </Table.Tr>
        </Table.Tbody>
      </Table>
    </Stack>
  );
}

/**
 * "Do this once for each item in a list."
 *
 * The settings are flat on a node — multiInstanceType, collection,
 * elementVariable, completionCondition — which is how the domain, the mapper
 * and the engine all name them. This editor used to keep them nested under a
 * `loopCharacteristics` object of its own invention, with a boolean
 * `isSequential` in place of the type, so nothing it wrote was ever read: a
 * task set to run once per item ran exactly once.
 */
export function MultiInstanceConfig({ data, onUpdate }: NodeConfigProps) {
  const multiInstanceType = asText(data.multiInstanceType, 'none');
  const isMulti = multiInstanceType === 'parallel' || multiInstanceType === 'sequential';

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
              onUpdate({ multiInstanceType: 'parallel', collection: 'items', elementVariable: 'item' });
            } else {
              onUpdate({ multiInstanceType: 'none', collection: '', elementVariable: '', completionCondition: '' });
            }
          }}
        />
      </Group>

      {isMulti && (
        <Stack gap="sm" pl="xl">
          <Checkbox
            label="Sequential Execution"
            description="One at a time, in order, rather than all at once"
            checked={multiInstanceType === 'sequential'}
            onChange={(e) => onUpdate({ multiInstanceType: e.currentTarget.checked ? 'sequential' : 'parallel' })}
          />
          <TextInput
            label="Collection"
            placeholder="e.g. users"
            description="Process variable containing a list"
            size="sm"
            value={asText(data.collection)}
            onChange={(e) => onUpdate({ collection: e.target.value })}
          />
          <TextInput
            label="Element Variable"
            placeholder="e.g. user"
            description="Variable name for current item"
            size="sm"
            value={asText(data.elementVariable)}
            onChange={(e) => onUpdate({ elementVariable: e.target.value })}
          />
          <TextInput
            label="Completion Condition"
            placeholder="e.g. nrOfCompletedInstances == nrOfInstances"
            size="sm"
            value={asText(data.completionCondition)}
            onChange={(e) => onUpdate({ completionCondition: e.target.value })}
          />
        </Stack>
      )}
    </Stack>
  );
}

export function ConnectorCatalog({ onSelect }: { onSelect: (connector: ApiConnector) => void }) {
  const { data: connectorsData } = useConnectors();
  const connectors = connectorsData?.connectors ?? [];

  return (
    <Stack gap="md">
      <Text fw={700} size="sm">Choose a Connector</Text>
      <ScrollArea h={300}>
        <Stack gap="xs">
          {connectors.map((c) => (
            <Paper 
              key={c.id} 
              withBorder 
              p="sm" 
              onClick={() => onSelect(c)} 
              style={{ cursor: 'pointer' }}
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
  data: BPMNNodeData, 
  opened: boolean, 
  onClose: () => void 
}) {
  const executeConnector = useExecuteConnector();
  const executeScript = useExecuteScript();
  const [testVars, setTestVars] = useState('{}');
  const [result, setResult] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  const runTest = async () => {
    try {
      const variables = JSON.parse(testVars);
      setError(null);
      setResult(null);

      if (data.implementation === 'connector') {
        const res = await executeConnector.mutateAsync({
          connectorKey: asText(data.connector_id),
          config: asTextMap(data.inputs),
          payload: variables,
        });
        setResult(res);
      } else if (data.implementation === 'script') {
         const res = await executeScript.mutateAsync({
           script: asText(data.script),
           scriptFormat: asText(data.scriptFormat, 'javascript'),
           variables: { ...variables },
         });
         setResult(res);
      }
    } catch (e: unknown) {
      setError(errorMessage(e, 'Test execution failed'));
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

        {result != null && (
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

/**
 * Per-node API guidance: how an outside application drives exactly this node,
 * as curl and as the Go SDK, with variables taken from the node where it has
 * any. Every snippet here is the real wire contract — the paths and bodies
 * are the ones sdk/examples/quickstart exercises against a live server, so if
 * this drifts the quickstart is the test that catches the API side of it.
 */
export function ApiExample({ type, data }: { type: string, id: string, data: BPMNNodeData }) {
  let snippet = '';
  let title = 'API usage';
  let description = 'How another application drives this step.';

  const topic = data.externalTopic || 'your-topic';
  const message = data.messageName || 'your-message';
  const signal = data.signalName || 'your-signal';

  switch (type) {
    case 'startEvent':
      title = 'Start this process from your application';
      description = 'Deploy once, then every call starts a fresh instance with its own variables.';
      snippet = `# curl
curl -X POST $GOBPM/api/v1/process/start \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"project_id":"<project-uuid>","definition_key":"<process-key>",
       "variables":{"amount":42.5}}'

// Go SDK  (go get github.com/gsoultan/gobpm/sdk)
instanceID, err := client.StartProcess(ctx, projectID, "<process-key>",
    gobpm.Variables{"amount": 42.5})`;
      break;

    case 'userTask':
      title = 'Work this task from your own inbox UI';
      description = 'List, claim so nobody works it twice, then complete with the decision.';
      snippet = `# curl
curl -H "Authorization: Bearer $TOKEN" "$GOBPM/api/v1/tasks?page_size=50"
curl -X POST $GOBPM/api/v1/tasks/<task-id>/claim \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"user_id":"alice"}'
curl -X POST $GOBPM/api/v1/tasks/<task-id>/complete \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"user_id":"alice","variables":{"approved":true}}'

// Go SDK
tasks, _, err := client.ListTasks(ctx, gobpm.ListTasksOptions{})
err = client.ClaimTask(ctx, task.ID, "alice")
err = client.CompleteTask(ctx, task.ID, "alice", gobpm.Variables{"approved": true})`;
      break;

    case 'serviceTask':
      if (data.externalTopic) {
        title = `Serve topic "${topic}" with a worker`;
        description = 'The engine publishes this step as work; your service pulls it, does the job in your own runtime, and reports back.';
        snippet = `// Go SDK — a long-polling worker
worker := gobpm.NewWorker(client, "${topic}", "my-worker",
    gobpm.WorkerOptions{},
    func(ctx context.Context, task *gobpm.ExternalTask) (gobpm.Variables, error) {
        // your integration logic; task.Variables carries the process data
        return gobpm.Variables{"done": true}, nil
    })
go worker.Run(ctx)

# or raw HTTP
curl -X POST $GOBPM/api/v1/external-tasks/fetch-and-lock \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"topic":"${topic}","worker_id":"my-worker","max_tasks":5,"lock_duration_ms":60000}'
curl -X POST $GOBPM/api/v1/external-tasks/<task-id>/complete \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"my-worker","variables":{"done":true}}'`;
      } else {
        title = 'This step calls out by itself';
        description = 'A connector or HTTP call configured here runs inside the engine — nothing to integrate. Set an external topic instead if your own service should do the work.';
        snippet = `# Watch it run: the instance timeline shows the call and its outcome
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/<instance-id>/audit`;
      }
      break;

    case 'intermediateCatchEvent':
    case 'boundaryEvent':
      title = `Deliver "${message}" from your application`;
      description = 'The instance waits here until your system sends the message; the correlation key picks which instance.';
      snippet = `# curl
curl -X POST $GOBPM/api/v1/processes/message \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"project_id":"<project-uuid>","message_name":"${message}",
       "correlation_key":"<order-id>","variables":{"paid":true}}'

// Go SDK
err := client.SendMessage(ctx, projectID, "${message}", "<order-id>",
    gobpm.Variables{"paid": true})`;
      break;

    case 'intermediateThrowEvent':
      title = `Broadcast "${signal}" from your application`;
      description = 'A signal reaches every instance in the project waiting on it — no single addressee.';
      snippet = `# curl
curl -X POST $GOBPM/api/v1/processes/signal \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"project_id":"<project-uuid>","signal_name":"${signal}","variables":{}}'

// Go SDK
err := client.BroadcastSignal(ctx, projectID, "${signal}", nil)`;
      break;

    case 'businessRuleTask':
      title = 'Try the decision this step consults';
      description = 'Evaluate the decision table directly with sample inputs before running the whole process.';
      snippet = `# curl
curl -X POST $GOBPM/api/v1/decisions/evaluate \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"key":"<decision-key>","variables":{"amount":900}}'`;
      break;

    default:
      title = 'Watch this step from your application';
      description = 'Every step lands in the instance audit trail as it happens.';
      snippet = `# curl
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/<instance-id>
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/<instance-id>/audit

// Go SDK
instance, err := client.GetInstance(ctx, instanceID)
entries, err := client.GetTimeline(ctx, instanceID)`;
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
              <Text size="xs" fw={700} tt="uppercase" c="dimmed">{title}</Text>
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
  const [result, setResult] = useState<unknown>(null);
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
    } catch (e: unknown) {
      setError(errorMessage(e, 'Execution failed'));
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

        {result != null && (
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
        <ActionIcon aria-label="Add" size="xs" variant="light" onClick={addRow}>
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
          <ActionIcon aria-label="Delete" color="red" variant="subtle" size="xs" mt={4} onClick={() => removeRow(k)}>
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
