import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Grid,
  Group,
  NumberInput,
  PasswordInput,
  Select,
  Skeleton,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { AlertTriangle, CheckCircle2, Database, Plus, Server, Trash2 } from 'lucide-react';
import { useState } from 'react';

import {
  ENVIRONMENT_DRIVERS,
  DRIVER_DEFAULT_PORT,
  emptyEnvironment,
  needsServerFields,
  portsTakenByOthers,
  validateEnvironment,
  type EnvironmentDraft,
  type EnvironmentDriver,
} from '../domain/environmentForm';
import {
  useDeleteEnvironment,
  useEnvironments,
  useSaveEnvironment,
  useTestEnvironmentConnection,
} from '../hooks/useEnvironments';
import { errorMessage } from '../services/shared/errors';
import type { ApiEnvironment } from '../services/types';

const DRIVER_LABELS: Record<EnvironmentDriver, string> = {
  postgres: 'PostgreSQL',
};

/**
 * Where a project's runtimes are set up.
 *
 * Each environment names its own database, and that is the whole point: what
 * staging holds is absent from production rather than filtered out of it, so
 * there is no query that could cross between them even if one were written
 * wrong.
 *
 * The password field is never populated from the server — the API returns a
 * placeholder and accepts it back to mean "unchanged" — so editing a host does
 * not require re-typing a credential this browser was never given.
 */
export function EnvironmentSettings() {
  const { data, isLoading, error } = useEnvironments();
  const save = useSaveEnvironment();
  const remove = useDeleteEnvironment();
  const test = useTestEnvironmentConnection();
  const [draft, setDraft] = useState<EnvironmentDraft | null>(null);
  // What the last test found, cleared whenever the connection is edited so a
  // stale green tick cannot outlive the settings it was about.
  const [probe, setProbe] = useState<{ reachable: boolean; detail: string } | null>(null);

  const environments = data?.environments ?? [];
  const taken = portsTakenByOthers(
    environments.map((e) => ({ id: e.id, port: e.port })),
    draft?.id,
  );
  const errors = draft ? validateEnvironment(draft, taken) : {};
  const canSave = draft !== null && Object.keys(errors).length === 0;

  const edit = (environment: ApiEnvironment) => {
    setDraft({
      id: environment.id,
      name: environment.name,
      port: environment.port,
      driver: environment.driver,
      connection: (environment.connection ?? {}) as EnvironmentDraft['connection'],
      enabled: environment.enabled,
    });
  };

  /**
   * Edits the connection and forgets the last test.
   *
   * A green tick that outlives the settings it was about is worse than no tick:
   * it says "this works" about a host nobody has tried.
   */
  const patchConnection = (patch: Partial<EnvironmentDraft['connection']>) => {
    if (!draft) return;
    setProbe(null);
    setDraft({ ...draft, connection: { ...draft.connection, ...patch } });
  };

  const runTest = async () => {
    if (!draft) return;
    try {
      setProbe(await test.mutateAsync({ id: draft.id, driver: draft.driver, connection: draft.connection }));
    } catch (err: unknown) {
      setProbe({ reachable: false, detail: errorMessage(err, 'The database could not be reached.') });
    }
  };

  const submit = async () => {
    if (!draft) return;
    try {
      await save.mutateAsync(draft);
      notifications.show({
        title: draft.id ? `${draft.name} saved` : `${draft.name} added`,
        message: draft.enabled
          ? `Served on port ${draft.port} within 15 seconds.`
          : 'Kept, but not served: it stops within 15 seconds. Its database is untouched.',
        color: 'green',
      });
      setDraft(null);
    } catch {
      // useSaveEnvironment already reported it; keep the form open so the
      // person can correct what was wrong rather than retype it.
    }
  };

  const confirmRemove = async (environment: ApiEnvironment) => {
    try {
      await remove.mutateAsync(environment.id);
      notifications.show({
        title: `${environment.name} removed`,
        message: 'Its database was left untouched.',
        color: 'gray',
      });
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not remove it',
        message: errorMessage(err, 'It is still there.'),
        color: 'red',
      });
    }
  };

  return (
    <Card withBorder radius="lg" p="lg">
      <Stack gap="md">
        <Group justify="space-between" align="flex-start">
          <Group gap="sm">
            <ThemeIcon variant="light" color="indigo" size="lg" radius="md">
              <Server size={18} />
            </ThemeIcon>
            <Stack gap={0}>
              <Text fw={700}>Environments</Text>
              <Text size="xs" c="dimmed">
                Each runtime this project deploys into, and the database it owns. Nothing is shared
                between them.
              </Text>
            </Stack>
          </Group>
          {draft === null && (
            <Button
              size="compact-sm"
              variant="light"
              leftSection={<Plus size={14} />}
              onClick={() => setDraft(emptyEnvironment())}
            >
              Add environment
            </Button>
          )}
        </Group>

        {error && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{errorMessage(error, 'The environments could not be loaded.')}</Text>
          </Alert>
        )}

        {isLoading && <Skeleton height={80} radius="sm" />}

        {!isLoading && environments.length === 0 && draft === null && (
          <Alert color="gray" variant="light" radius="md">
            <Text size="sm">
              No environments yet. This project runs on the main database until you add one.
            </Text>
          </Alert>
        )}

        {environments.length > 0 && (
          <Table verticalSpacing="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Environment</Table.Th>
                <Table.Th>Served on</Table.Th>
                <Table.Th>Database</Table.Th>
                <Table.Th ta="right">Actions</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {environments.map((environment) => (
                <Table.Tr key={environment.id}>
                  <Table.Td>
                    <Group gap="xs">
                      <Text size="sm" fw={600}>{environment.name}</Text>
                      {!environment.enabled && (
                        <Tooltip label="Kept, but not served. Its database is untouched.">
                          <Badge size="xs" color="gray" variant="light">Disabled</Badge>
                        </Tooltip>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td><Text size="sm" ff="monospace">:{environment.port}</Text></Table.Td>
                  <Table.Td>
                    <Group gap={6}>
                      <Database size={13} />
                      <Text size="xs" c="dimmed">
                        {environment.driver}
                        {typeof environment.connection?.db_name === 'string'
                          ? ` · ${environment.connection.db_name}`
                          : ''}
                      </Text>
                    </Group>
                  </Table.Td>
                  <Table.Td>
                    <Group justify="flex-end" gap="xs">
                      <Button size="compact-xs" variant="light" onClick={() => edit(environment)}>
                        Edit
                      </Button>
                      <Tooltip label="Removes it from this list. The database is left as it is.">
                        <Button
                          size="compact-xs"
                          variant="subtle"
                          color="red"
                          leftSection={<Trash2 size={12} />}
                          loading={remove.isPending}
                          onClick={() => confirmRemove(environment)}
                        >
                          Remove
                        </Button>
                      </Tooltip>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}

        {draft && (
          <Card withBorder radius="md" p="md" bg="var(--mantine-color-body)">
            <Stack gap="sm">
              <Text fw={600} size="sm">{draft.id ? `Edit ${draft.name || 'environment'}` : 'New environment'}</Text>

              <Grid>
                <Grid.Col span={{ base: 12, sm: 6 }}>
                  <TextInput
                    label="Name"
                    placeholder="staging"
                    value={draft.name}
                    error={errors.name}
                    onChange={(e) => setDraft({ ...draft, name: e.currentTarget.value })}
                  />
                </Grid.Col>
                <Grid.Col span={{ base: 12, sm: 6 }}>
                  <NumberInput
                    label="Served on port"
                    description="Where people open this environment"
                    placeholder="8081"
                    value={draft.port || ''}
                    error={errors.port}
                    min={1024}
                    max={65535}
                    onChange={(value) => setDraft({ ...draft, port: Number(value) || 0 })}
                  />
                </Grid.Col>

                <Grid.Col span={{ base: 12, sm: 6 }}>
                  <Select
                    label="Database engine"
                    data={ENVIRONMENT_DRIVERS.map((d) => ({ value: d, label: DRIVER_LABELS[d] }))}
                    value={draft.driver}
                    error={errors.driver}
                    allowDeselect={false}
                    onChange={(value) => {
                      const driver = (value ?? 'postgres') as EnvironmentDriver;
                      setDraft({
                        ...draft,
                        driver,
                        connection: { ...draft.connection, port: DRIVER_DEFAULT_PORT[driver] || undefined },
                      });
                    }}
                  />
                </Grid.Col>
                <Grid.Col span={{ base: 12, sm: 6 }}>
                  <TextInput
                    label={needsServerFields(draft.driver) ? 'Database name' : 'File'}
                    placeholder={needsServerFields(draft.driver) ? 'metis_staging' : 'staging.db'}
                    value={String(draft.connection.db_name ?? '')}
                    error={errors.db_name}
                    onChange={(e) =>
                      patchConnection({ db_name: e.currentTarget.value })
                    }
                  />
                </Grid.Col>

                {needsServerFields(draft.driver) && (
                  <>
                    <Grid.Col span={{ base: 12, sm: 8 }}>
                      <TextInput
                        label="Database host"
                        placeholder="db.internal"
                        value={String(draft.connection.host ?? '')}
                        error={errors.host}
                        onChange={(e) =>
                          patchConnection({ host: e.currentTarget.value })
                        }
                      />
                    </Grid.Col>
                    <Grid.Col span={{ base: 12, sm: 4 }}>
                      <NumberInput
                        label="Database port"
                        value={draft.connection.port ?? ''}
                        onChange={(value) =>
                          patchConnection({ port: Number(value) || undefined })
                        }
                      />
                    </Grid.Col>
                    <Grid.Col span={{ base: 12, sm: 6 }}>
                      <TextInput
                        label="Username"
                        value={String(draft.connection.username ?? '')}
                        onChange={(e) =>
                          patchConnection({ username: e.currentTarget.value })
                        }
                      />
                    </Grid.Col>
                    <Grid.Col span={{ base: 12, sm: 6 }}>
                      <PasswordInput
                        label="Password"
                        description={draft.id ? 'Leave as it is to keep the stored one' : undefined}
                        value={String(draft.connection.password ?? '')}
                        onChange={(e) =>
                          patchConnection({ password: e.currentTarget.value })
                        }
                      />
                    </Grid.Col>
                    <Grid.Col span={12}>
                      <Checkbox
                        label="Require TLS to the database"
                        checked={Boolean(draft.connection.ssl_enabled)}
                        onChange={(e) =>
                          patchConnection({ ssl_enabled: e.currentTarget.checked })
                        }
                      />
                    </Grid.Col>
                  </>
                )}

                <Grid.Col span={12}>
                  <Checkbox
                    label="Serve this environment"
                    description="Turning it off stops the listener. The database is left as it is."
                    checked={draft.enabled}
                    onChange={(e) => setDraft({ ...draft, enabled: e.currentTarget.checked })}
                  />
                </Grid.Col>
              </Grid>

              {probe && (
                <Alert
                  color={probe.reachable ? 'green' : 'red'}
                  variant="light"
                  radius="md"
                  icon={probe.reachable ? <CheckCircle2 size={16} /> : <AlertTriangle size={16} />}
                >
                  <Text size="xs">
                    {probe.reachable
                      ? 'The database answered. This environment can be served.'
                      : probe.detail || 'The database did not answer.'}
                  </Text>
                </Alert>
              )}

              <Group justify="space-between">
                <Button
                  variant="light"
                  color="gray"
                  loading={test.isPending}
                  disabled={!draft.driver}
                  onClick={runTest}
                >
                  Test connection
                </Button>
                <Group>
                <Button variant="default" onClick={() => { setProbe(null); setDraft(null); }} disabled={save.isPending}>
                  Cancel
                </Button>
                <Button color="indigo" loading={save.isPending} disabled={!canSave} onClick={submit}>
                  {draft.id ? 'Save' : 'Add environment'}
                </Button>
                </Group>
              </Group>
            </Stack>
          </Card>
        )}
      </Stack>
    </Card>
  );
}
