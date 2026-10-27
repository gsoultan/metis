import {
  ActionIcon,
  Badge,
  Button,
  Card,
  CopyButton,
  Modal,
  PasswordInput,
  Group,
  Stack,
  Table,
  Text,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import { KeyRound, Search, Trash2, Upload, UserCircle, Users } from 'lucide-react';
import { useState, useTransition } from 'react';

import { PageHeader } from '../components/PageHeader';
import { ParticipantImportModal } from '../components/ParticipantImportModal';
import { ParticipantSources } from '../components/ParticipantSources';
import { PeopleSwitchOver } from '../components/PeopleSwitchOver';
import { EmptyState, ErrorState, TableLoadingState } from '../components/state';
import {
  STANDING_LABELS,
  standingOf,
  type ImportSummary,
  type Participant,
} from '../domain/participantImport';
import { hasRole } from '../domain/access';
import { PRIVILEGED_ROLE } from '../domain/roles';
import { matchesQuery } from '../domain/textSearch';
import { useAppStore } from '../store/useAppStore';
import {
  useImportParticipants,
  useParticipants,
  useRemoveParticipant,
  useSetParticipantPassword,
} from '../hooks/useParticipants';
import { useTranslation } from '../i18n/context';

const COLUMNS = 4;

const STANDING_COLOURS: Record<string, string> = {
  ready: 'green',
  'no-credentials': 'yellow',
  inactive: 'gray',
};

/**
 * The people this project's processes can assign work to.
 *
 * Deliberately not the same page as platform accounts, and deliberately not
 * next to them in the navigation. They were one list, which meant one role
 * list answered two unrelated questions — who may approve an invoice, and who
 * may deploy a process model — so giving somebody an inbox gave them the
 * platform.
 *
 * These are per project: two projects naming an approver "ada" mean two
 * different people.
 */
export function ParticipantList() {
  const { t } = useTranslation();
  const isAdmin = useAppStore((state) => hasRole(state.user, PRIVILEGED_ROLE));
  const isDesigner = useAppStore((state) => hasRole(state.user, PRIVILEGED_ROLE) || hasRole(state.user, 'DESIGNER'));
  const projectId = useAppStore((state) => state.currentProjectId);
  const signInAddress = projectId ? `${window.location.origin}/p/?project=${encodeURIComponent(projectId)}` : '';
  const { data, isLoading, error, refetch } = useParticipants();
  const removeParticipant = useRemoveParticipant();
  const setPassword = useSetParticipantPassword();
  // The person whose password is being set, and what has been typed so far.
  const [passwordFor, setPasswordFor] = useState<string | null>(null);
  const [newPassword, setNewPassword] = useState('');
  // The person being removed, held while the confirmation is open. Removal is
  // reversible, so this asks rather than warns — but it still asks, because the
  // row it hides is somebody a running process may be about to assign work to.
  const [removing, setRemoving] = useState<string | null>(null);
  const importParticipants = useImportParticipants();
  const [query, setQuery] = useState('');
  const [importOpen, setImportOpen] = useState(false);
  const [result, setResult] = useState<ImportSummary | null>(null);
  const [, startTransition] = useTransition();

  const everyone: Participant[] = data?.participants ?? [];
  // Held for the confirmation's wording: it names the person rather than saying
  // "this participant", because the row it is about has already scrolled.
  const removedPerson = everyone.find((person) => person.id === removing) ?? null;
  const passwordPerson = everyone.find((person) => person.id === passwordFor) ?? null;
  const people = query
    ? everyone.filter((p) => matchesQuery(query, p.username, p.display_name ?? '', p.email ?? ''))
    : everyone;

  const runImport = async (body: Record<string, unknown>) => {
    try {
      setResult(await importParticipants.mutateAsync(body));
    } catch {
      // The hook reports it; the dialog stays open so the source can be
      // corrected rather than retyped.
    }
  };

  if (error) {
    return <ErrorState error={error} action="load the people in this project" onRetry={() => refetch()} />;
  }

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.people.title')}
        description={t('page.people.subtitle')}
        actions={
          <Button
            variant="filled"
            color="indigo"
            leftSection={<Upload size={16} />}
            onClick={() => { setResult(null); setImportOpen(true); }}
          >
            Import
          </Button>
        }
      />

      {signInAddress && (
        <Card withBorder radius="lg" p="md">
          <Group justify="space-between" wrap="nowrap">
            <Stack gap={2}>
              <Text fw={600} size="sm">{t('page.people.signInAddress')}</Text>
              <Text size="sm" c="dimmed" style={{ wordBreak: 'break-all' }}>{signInAddress}</Text>
            </Stack>
            <CopyButton value={signInAddress}>
              {({ copied, copy }) => (
                <Button variant="light" size="xs" onClick={copy}>
                  {copied ? t('page.people.copied') : t('page.people.copy')}
                </Button>
              )}
            </CopyButton>
          </Group>
        </Card>
      )}

      <Card withBorder radius="lg" p={0}>
        <Group p="md" justify="space-between">
          <TextInput
            aria-label="Search people"
            placeholder="Search by name or email"
            leftSection={<Search size={16} />}
            value={query}
            onChange={(e) => {
              const value = e.currentTarget.value;
              startTransition(() => setQuery(value));
            }}
            w={320}
          />
          {everyone.length > 0 && (
            <Text size="xs" c="dimmed">
              {query ? `${people.length} of ${everyone.length} match` : `${everyone.length} in this project`}
            </Text>
          )}
        </Group>

        {isLoading && <TableLoadingState columns={COLUMNS} />}

        {!isLoading && everyone.length === 0 && (
          <EmptyState
            icon={Users}
            title="Nobody here yet"
            description="Import a directory from a file, an API or a database query, so processes have somebody to assign work to."
          />
        )}

        {!isLoading && everyone.length > 0 && (
          <Table.ScrollContainer minWidth={720}>
            <Table verticalSpacing="sm" highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Person</Table.Th>
                  <Table.Th>Email</Table.Th>
                  <Table.Th>Teams</Table.Th>
                  <Table.Th>Standing</Table.Th>
                  <Table.Th ta="right">Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {people.map((person) => {
                  const standing = standingOf(person);
                  const meta = STANDING_LABELS[standing];
                  return (
                    <Table.Tr key={person.id}>
                      <Table.Td>
                        <Group gap="sm">
                          <ThemeIcon variant="light" color="gray" radius="xl" size="md">
                            <UserCircle size={16} />
                          </ThemeIcon>
                          <Stack gap={0}>
                            <Text size="sm" fw={600}>{person.display_name || person.username}</Text>
                            <Text size="xs" c="dimmed" ff="monospace">{person.username}</Text>
                          </Stack>
                        </Group>
                      </Table.Td>
                      <Table.Td><Text size="sm" c="dimmed">{person.email || '—'}</Text></Table.Td>
                      <Table.Td>
                        <Group gap={4}>
                          {(person.groups ?? []).length === 0
                            ? <Text size="xs" c="dimmed">—</Text>
                            : (person.groups ?? []).map((team) => (
                                <Badge key={team} size="sm" variant="light" color="blue">{team}</Badge>
                              ))}
                        </Group>
                      </Table.Td>
                      <Table.Td>
                        <Tooltip label={meta.hint} multiline w={260}>
                          <Badge
                            size="sm"
                            variant="light"
                            color={STANDING_COLOURS[standing]}
                            leftSection={standing === 'no-credentials' ? <KeyRound size={10} /> : undefined}
                          >
                            {meta.label}
                          </Badge>
                        </Tooltip>
                      </Table.Td>
                      <Table.Td ta="right">
                        <Group gap={4} justify="flex-end" wrap="nowrap">
                          <Tooltip label="Set the password they sign in to their tasks with">
                            <ActionIcon
                              variant="subtle"
                              aria-label={`Set a password for ${person.display_name || person.username}`}
                              onClick={() => { setNewPassword(''); setPasswordFor(person.id); }}
                            >
                              <KeyRound size={16} />
                            </ActionIcon>
                          </Tooltip>
                          <Tooltip label="Take them out of this project's directory">
                            <ActionIcon
                              variant="subtle"
                              color="red"
                              aria-label={`Remove ${person.display_name || person.username}`}
                              loading={removeParticipant.isPending && removing === person.id}
                              onClick={() => setRemoving(person.id)}
                            >
                              <Trash2 size={16} />
                            </ActionIcon>
                          </Tooltip>
                        </Group>
                      </Table.Td>
                    </Table.Tr>
                  );
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Card>

      {/*
        Directories are administrative: each holds a connection string or an
        endpoint token, so the list alone says where they live. The server
        refuses a non-admin either way; hiding it is so nobody is shown a
        control that will only ever say no.
      */}
      {isDesigner && <PeopleSwitchOver />}

      {isAdmin && <ParticipantSources />}

      <Modal
        opened={removing !== null}
        onClose={() => setRemoving(null)}
        radius="md"
        title={<Text fw={700}>Remove from the directory</Text>}
      >
        <Stack gap="md">
          <Text size="sm">
            {removedPerson
              ? `${removedPerson.display_name || removedPerson.username} will no longer appear in this project's directory.`
              : 'They will no longer appear in this project\'s directory.'}
          </Text>
          <Text size="sm" c="dimmed">
            Any task already in their inbox stays where it is — a task names its assignee rather
            than pointing at them, so work does not disappear when somebody leaves. Importing a
            directory that names them again brings them back with their teams.
          </Text>
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={() => setRemoving(null)}>Cancel</Button>
            <Button
              color="red"
              loading={removeParticipant.isPending}
              onClick={() => {
                if (!removing) return;
                removeParticipant.mutate(removing, { onSettled: () => setRemoving(null) });
              }}
            >
              Remove
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={passwordFor !== null}
        onClose={() => setPasswordFor(null)}
        radius="md"
        title={<Text fw={700}>Set a password</Text>}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (!passwordFor) return;
            setPassword.mutate({ id: passwordFor, password: newPassword }, { onSuccess: () => setPasswordFor(null) });
          }}
        >
          <Stack gap="md">
            <Text size="sm">
              {passwordPerson
                ? `${passwordPerson.display_name || passwordPerson.username} signs in to their tasks with this password.`
                : 'They sign in to their tasks with this password.'}
            </Text>
            <PasswordInput
              label="New password"
              description="At least 8 characters. Any session they had ends."
              value={newPassword}
              onChange={(event) => setNewPassword(event.currentTarget.value)}
              autoComplete="new-password"
              data-autofocus
            />
            <Group justify="flex-end">
              <Button variant="subtle" color="gray" onClick={() => setPasswordFor(null)}>Cancel</Button>
              <Button type="submit" loading={setPassword.isPending} disabled={newPassword.trim().length === 0}>
                Set password
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      <ParticipantImportModal
        opened={importOpen}
        onClose={() => setImportOpen(false)}
        running={importParticipants.isPending}
        result={result}
        onImportFile={(file) => runImport({ kind: 'csv', file })}
        onImportHTTP={(url, method) => runImport({ kind: 'http', url, method })}
        onImportPostgres={(dsn, query2) => runImport({ kind: 'postgres', dsn, query: query2 })}
      />
    </Stack>
  );
}
