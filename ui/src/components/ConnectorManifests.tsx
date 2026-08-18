/**
 * Connectors written as a document rather than compiled in.
 *
 * Adding an integration used to mean editing Go and redeploying the engine. A
 * manifest is the same connector written down — what to call, how to
 * authenticate, what goes in and comes back — and this is where one is
 * installed.
 *
 * The screen deliberately offers one box for both kinds of document. What
 * somebody has in front of them is "a file the vendor published"; being asked
 * which of two upload buttons it belongs to is a question about our
 * implementation, not theirs. Pasting an OpenAPI specification installs one
 * connector per operation.
 */
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Code,
  Group,
  Modal,
  SegmentedControl,
  Stack,
  Switch,
  Table,
  Text,
  Textarea,
  Title,
  Tooltip,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { notifications } from '@mantine/notifications';
import { FileCode, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import {
  useConnectorManifests,
  useDeleteConnectorManifest,
  useInstallConnectorManifest,
  useSetConnectorManifestEnabled,
} from '../hooks/useConnectorManifests';

const EXAMPLE = `key: crm.create-lead
version: 1
name: Create a lead
auth:
  type: bearer
request:
  method: POST
  url: "{{config.base_url}}/leads"
  body:
    name: "{{input.name}}"
response:
  outputs:
    lead_id: body.id
errors:
  - when: "status = 429"
    bpmn_error: RATE_LIMITED
    retryable: true
    retry_after: "headers['Retry-After']"`;

export function ConnectorManifests() {
  const { data: manifests } = useConnectorManifests();
  const install = useInstallConnectorManifest();
  const setEnabled = useSetConnectorManifestEnabled();
  const remove = useDeleteConnectorManifest();

  const [opened, modal] = useDisclosure(false);
  const [document, setDocument] = useState('');
  const [format, setFormat] = useState<'manifest' | 'openapi'>('manifest');

  const submit = async () => {
    try {
      const installed = await install.mutateAsync({ document, format });
      modal.close();
      setDocument('');
      notifications.show({
        title: installed.length === 1 ? 'Connector installed' : `${installed.length} connectors installed`,
        message: installed.map((m) => m.key).join(', '),
        color: 'green',
      });
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not install it',
        message: err instanceof Error ? err.message : 'The document could not be read.',
        color: 'red',
      });
    }
  };

  const installed = manifests ?? [];

  return (
    <Card withBorder radius="lg" p="xl">
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <div>
            <Group gap="xs">
              <FileCode size={18} />
              <Title order={4}>Connectors from a document</Title>
            </Group>
            <Text size="xs" c="dimmed">
              A connector described by a manifest, or imported from an API specification. No redeploy.
            </Text>
          </div>
          <Button size="xs" leftSection={<Plus size={14} />} onClick={modal.open}>
            Install
          </Button>
        </Group>

        {installed.length === 0 ? (
          <Text size="sm" c="dimmed">
            None installed. Every connector here is one of the built-in ones.
          </Text>
        ) : (
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Connector</Table.Th>
                <Table.Th>Key a step names</Table.Th>
                <Table.Th>Version</Table.Th>
                <Table.Th>On</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {installed.map((manifest) => (
                <Table.Tr key={manifest.id}>
                  <Table.Td>
                    <Text size="sm" fw={500}>
                      {manifest.name || manifest.key}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Code fz={11}>{manifest.key}</Code>
                  </Table.Td>
                  <Table.Td>
                    <Badge size="xs" variant="light">
                      v{manifest.version ?? 1}
                    </Badge>
                  </Table.Td>
                  <Table.Td>
                    <Tooltip label="Switching off leaves the document in place" withArrow>
                      <Switch
                        size="xs"
                        aria-label={`Switch ${manifest.key} ${manifest.enabled ? 'off' : 'on'}`}
                        checked={manifest.enabled}
                        onChange={(event) =>
                          setEnabled.mutate({ id: manifest.id, enabled: event.currentTarget.checked })
                        }
                      />
                    </Tooltip>
                  </Table.Td>
                  <Table.Td>
                    <Tooltip label="Remove — any step naming this key stops working">
                      <ActionIcon
                        aria-label={`Delete the ${manifest.key} connector`}
                        variant="subtle"
                        color="red"
                        size="sm"
                        onClick={() => remove.mutate(manifest.id)}
                      >
                        <Trash2 size={14} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
      </Stack>

      <Modal opened={opened} onClose={modal.close} title="Install a connector" size="xl">
        <Stack gap="md">
          <SegmentedControl
            value={format}
            onChange={(value) => setFormat(value as 'manifest' | 'openapi')}
            data={[
              { label: 'A connector manifest', value: 'manifest' },
              { label: 'An OpenAPI specification', value: 'openapi' },
            ]}
          />

          <Alert variant="light" color="blue" py="xs">
            <Text size="xs">
              {format === 'manifest'
                ? 'One document, one connector. Anything in {{ }} is filled in from the connection’s settings and the step’s inputs.'
                : 'One connector per operation in the specification. What comes out is a starting point — rename what you keep and delete the rest.'}
            </Text>
          </Alert>

          <Textarea
            aria-label="The document to install"
            placeholder={format === 'manifest' ? EXAMPLE : 'Paste the OpenAPI document…'}
            value={document}
            onChange={(event) => setDocument(event.currentTarget.value)}
            autosize
            minRows={14}
            maxRows={24}
            styles={{ input: { fontFamily: 'monospace', fontSize: 12 } }}
          />

          <Group justify="space-between">
            {format === 'manifest' && (
              <Button variant="subtle" size="xs" onClick={() => setDocument(EXAMPLE)}>
                Start from an example
              </Button>
            )}
            <Group ml="auto">
              <Button variant="subtle" color="gray" onClick={modal.close}>
                Cancel
              </Button>
              <Button onClick={submit} loading={install.isPending} disabled={!document.trim()}>
                Install
              </Button>
            </Group>
          </Group>
        </Stack>
      </Modal>
    </Card>
  );
}
