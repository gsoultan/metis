import { Alert, Badge, Button, Card, Center, Container, Group, Loader, Paper, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core';
import { AlertCircle, CheckCircle2, LogOut } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import { PersonKeys } from '../components/PersonKeys';
import { TaskForm } from '../components/TaskForm';
import { useTranslation } from '../i18n/context';
import { actionsFor, peopleService, PeopleSessionEnded, type PeopleSession, type PersonTask } from '../services/domains/peopleService';
import { clearPeopleSession, projectFromSearch, readPeopleSession, savePeopleSession } from '../services/domains/peopleSession';
import { errorMessage } from '../services/shared/errors';

/**
 * Where a person signs in and works the tasks given or offered to them.
 *
 * A person is not a User: this page has no navigation into the platform, its
 * session is its own, and signing out here leaves any User session in the same
 * browser alone.
 */
export function PeopleInboxPage() {
  const [session, setSession] = useState<PeopleSession | null>(() => readPeopleSession());
  const [ended, setEnded] = useState(false);

  const signOut = useCallback((sessionEnded: boolean) => {
    clearPeopleSession();
    setSession(null);
    setEnded(sessionEnded);
  }, []);

  return (
    <Container size="md" py="xl">
      {session ? (
        <Inbox session={session} onSignOut={() => signOut(false)} onEnded={() => signOut(true)} />
      ) : (
        <SignIn
          ended={ended}
          onSignedIn={(next) => {
            savePeopleSession(next);
            setEnded(false);
            setSession(next);
          }}
        />
      )}
    </Container>
  );
}

function SignIn({ ended, onSignedIn }: { ended: boolean; onSignedIn: (session: PeopleSession) => void }) {
  const { t } = useTranslation();
  const project = useMemo(() => projectFromSearch(window.location.search), []);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onSignedIn(await peopleService.signIn(project, username.trim(), password));
    } catch (failure) {
      setError(errorMessage(failure, t('people.signIn.failed')));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Paper withBorder p="xl" radius="md" maw={420} mx="auto">
      <form onSubmit={submit}>
        <Stack gap="md">
          <Title order={2}>{t('people.signIn.title')}</Title>
          {ended && <Alert color="orange">{t('people.signIn.ended')}</Alert>}
          {!project ? (
            <Alert color="orange" icon={<AlertCircle size={18} />}>
              {t('people.signIn.noProject')}
            </Alert>
          ) : (
            <>
              <TextInput
                label={t('people.signIn.username')}
                value={username}
                onChange={(event) => setUsername(event.currentTarget.value)}
                autoComplete="username"
                required
              />
              <PasswordInput
                label={t('people.signIn.password')}
                value={password}
                onChange={(event) => setPassword(event.currentTarget.value)}
                autoComplete="current-password"
                required
              />
              {error && (
                <Alert color="red" icon={<AlertCircle size={18} />}>
                  {error}
                </Alert>
              )}
              <Button type="submit" loading={busy} disabled={!username.trim() || !password}>
                {t('people.signIn.submit')}
              </Button>
            </>
          )}
        </Stack>
      </form>
    </Paper>
  );
}

type Load = { kind: 'loading' } | { kind: 'failed'; message: string } | { kind: 'ready'; tasks: PersonTask[] };

function Inbox({ session, onSignOut, onEnded }: { session: PeopleSession; onSignOut: () => void; onEnded: () => void }) {
  const { t } = useTranslation();
  const [load, setLoad] = useState<Load>({ kind: 'loading' });
  const [openID, setOpenID] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const who = session.person.display_name || session.person.username;

  // Reads the list and says what to show; null when the session is over.
  const fetchTasks = useCallback(
    async (signal?: AbortSignal): Promise<Load | null> => {
      try {
        return { kind: 'ready', tasks: await peopleService.listTasks(session.token, signal) };
      } catch (failure) {
        if (failure instanceof PeopleSessionEnded) {
          return null;
        }
        return { kind: 'failed', message: errorMessage(failure, t('people.inbox.loadFailed')) };
      }
    },
    [session.token, t],
  );

  const show = useCallback(
    (next: Load | null) => {
      if (next === null) {
        onEnded();
      } else {
        setLoad(next);
      }
    },
    [onEnded],
  );

  useEffect(() => {
    const controller = new AbortController();
    fetchTasks(controller.signal).then((next) => {
      if (!controller.signal.aborted) {
        show(next);
      }
    });
    return () => controller.abort();
  }, [fetchTasks, show]);

  const refresh = async () => show(await fetchTasks());

  const open = load.kind === 'ready' ? load.tasks.find((task) => task.id === openID) : undefined;

  return (
    <Stack gap="lg">
      <Group justify="space-between">
        <div>
          <Title order={2}>{t('people.inbox.title')}</Title>
          <Text c="dimmed" size="sm">
            {t('people.inbox.signedInAs', { name: who })}
          </Text>
        </div>
        <Button variant="subtle" leftSection={<LogOut size={16} />} onClick={onSignOut}>
          {t('people.inbox.signOut')}
        </Button>
      </Group>
      {notice && (
        <Alert color="teal" icon={<CheckCircle2 size={18} />} withCloseButton onClose={() => setNotice(null)}>
          {notice}
        </Alert>
      )}
      {load.kind === 'loading' && (
        <Center py="xl">
          <Loader aria-label={t('people.inbox.loading')} />
        </Center>
      )}
      {load.kind === 'failed' && (
        <Alert color="red" icon={<AlertCircle size={18} />}>
          {load.message}
        </Alert>
      )}
      {load.kind === 'ready' && open && (
        <OpenTask
          key={open.id}
          task={open}
          session={session}
          onBack={() => setOpenID(null)}
          onEnded={onEnded}
          onChanged={async (message, closed) => {
            setNotice(message);
            if (closed) {
              setOpenID(null);
            }
            await refresh();
          }}
        />
      )}
      {load.kind === 'ready' && !open && (
        load.tasks.length === 0 ? (
          <Text c="dimmed">{t('people.inbox.empty')}</Text>
        ) : (
          <Stack gap="sm">
            {load.tasks.map((task) => (
              <Card key={task.id} withBorder padding="md" radius="md">
                <Group justify="space-between" wrap="nowrap">
                  <div>
                    <Text fw={600}>{task.name}</Text>
                    {task.due_date && (
                      <Text size="sm" c="dimmed">
                        {t('people.inbox.due', { date: new Date(task.due_date).toLocaleString() })}
                      </Text>
                    )}
                  </div>
                  <Group gap="xs" wrap="nowrap">
                    <Badge variant="light">{holderLabel(task, session.person.username, t)}</Badge>
                    <Button size="xs" onClick={() => setOpenID(task.id)}>
                      {t('people.inbox.open')}
                    </Button>
                  </Group>
                </Group>
              </Card>
            ))}
          </Stack>
        )
      )}
      {!open && <PersonKeys token={session.token} onEnded={onEnded} />}
    </Stack>
  );
}

function holderLabel(task: PersonTask, username: string, t: (key: string) => string): string {
  return task.assignee?.username === username ? t('people.inbox.yours') : t('people.inbox.offered');
}

function OpenTask({
  task,
  session,
  onBack,
  onEnded,
  onChanged,
}: {
  task: PersonTask;
  session: PeopleSession;
  onBack: () => void;
  onEnded: () => void;
  onChanged: (message: string, closed: boolean) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const may = actionsFor(task, session.person.username);
  const fields = useMemo(() => {
    try {
      return JSON.parse(task.form_definition || '[]');
    } catch (failure) {
      console.warn('Task form definition is not valid JSON; rendering an empty form', failure);
      return [];
    }
  }, [task.form_definition]);

  const act = async (run: () => Promise<void>, message: string, closed: boolean) => {
    setBusy(true);
    setError(null);
    try {
      await run();
      await onChanged(message, closed);
    } catch (failure) {
      if (failure instanceof PeopleSessionEnded) {
        onEnded();
        return;
      }
      setError(errorMessage(failure, t('people.task.failed')));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Paper withBorder p="xl" radius="md">
      <Stack gap="md">
        <Group justify="space-between">
          <Title order={3}>{task.name}</Title>
          <Button variant="subtle" onClick={onBack}>
            {t('people.task.back')}
          </Button>
        </Group>
        {task.description && <Text>{task.description}</Text>}
        {error && (
          <Alert color="red" icon={<AlertCircle size={18} />}>
            {error}
          </Alert>
        )}
        {may.claim && (
          <Group>
            <Button loading={busy} onClick={() => act(() => peopleService.claim(session.token, task.id), t('people.task.claimed'), false)}>
              {t('people.task.claim')}
            </Button>
            <Text size="sm" c="dimmed">
              {t('people.task.claimFirst')}
            </Text>
          </Group>
        )}
        {may.complete &&
          (Array.isArray(fields) && fields.length > 0 ? (
            <TaskForm
              fields={fields}
              variables={task.variables || {}}
              loading={busy}
              onSubmit={(values) => act(() => peopleService.complete(session.token, task.id, values), t('people.task.completed'), true)}
            />
          ) : (
            <Group justify="flex-end">
              <Button loading={busy} onClick={() => act(() => peopleService.complete(session.token, task.id, {}), t('people.task.completed'), true)}>
                {t('people.task.complete')}
              </Button>
            </Group>
          ))}
        {may.release && (
          <Group justify="flex-start">
            <Button variant="default" loading={busy} onClick={() => act(() => peopleService.release(session.token, task.id), t('people.task.released'), true)}>
              {t('people.task.release')}
            </Button>
          </Group>
        )}
      </Stack>
    </Paper>
  );
}
