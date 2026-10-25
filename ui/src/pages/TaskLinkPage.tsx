import { Alert, Button, Center, Container, Group, Loader, Paper, Stack, Text, Title } from '@mantine/core';
import { AlertCircle, CheckCircle2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { TaskForm } from '../components/TaskForm';
import { useTranslation } from '../i18n/context';
import { taskLinkService, tokenFromHash, type TaskLinkView } from '../services/domains/taskLinkService';
import { errorMessage } from '../services/shared/errors';

type State =
  | { kind: 'loading' }
  | { kind: 'gone'; message: string }
  | { kind: 'open'; view: TaskLinkView }
  | { kind: 'done' };

/**
 * One task, for one person, reached from a link in their mail.
 *
 * Opening the page only reads the task: a mail scanner that opens every link
 * must not complete anything. Only pressing the button does, once.
 */
export function TaskLinkPage() {
  const { t } = useTranslation();
  // Read once, then taken off the address bar so it is not left in the
  // browser's history for the next person at the same computer.
  const token = useMemo(() => tokenFromHash(window.location.hash), []);
  const [state, setState] = useState<State>({ kind: 'loading' });
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (window.location.hash) {
      window.history.replaceState(null, '', window.location.pathname);
    }
    if (!token) {
      setState({ kind: 'gone', message: t('taskLink.noToken') });
      return;
    }
    const controller = new AbortController();
    taskLinkService
      .readTaskLink(token, controller.signal)
      .then((view) => setState({ kind: 'open', view }))
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          setState({ kind: 'gone', message: errorMessage(error, t('taskLink.gone')) });
        }
      });
    return () => controller.abort();
  }, [token, t]);

  const submit = async (values: Record<string, unknown>) => {
    setSubmitting(true);
    setSubmitError(null);
    try {
      await taskLinkService.completeByTaskLink(token, values);
      setState({ kind: 'done' });
    } catch (error) {
      setSubmitError(errorMessage(error, t('taskLink.submitFailed')));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Container size="sm" py="xl">
      <Paper withBorder p="xl" radius="md">
        {state.kind === 'loading' && (
          <Center py="xl">
            <Loader aria-label={t('taskLink.loading')} />
          </Center>
        )}
        {state.kind === 'gone' && (
          <Alert color="orange" icon={<AlertCircle size={18} />} title={t('taskLink.goneTitle')}>
            {state.message}
          </Alert>
        )}
        {state.kind === 'done' && (
          <Stack align="center" gap="sm" py="lg">
            <CheckCircle2 size={40} aria-hidden />
            <Title order={2}>{t('taskLink.doneTitle')}</Title>
            <Text c="dimmed">{t('taskLink.doneBody')}</Text>
          </Stack>
        )}
        {state.kind === 'open' && (
          <OpenTask view={state.view} submitting={submitting} submitError={submitError} onSubmit={submit} />
        )}
      </Paper>
    </Container>
  );
}

function OpenTask({
  view,
  submitting,
  submitError,
  onSubmit,
}: {
  view: TaskLinkView;
  submitting: boolean;
  submitError: string | null;
  onSubmit: (values: Record<string, unknown>) => void;
}) {
  const { t } = useTranslation();
  const fields = useMemo(() => {
    try {
      return JSON.parse(view.task.form_definition || '[]');
    } catch (error) {
      console.warn('Task form definition is not valid JSON; rendering an empty form', error);
      return [];
    }
  }, [view.task.form_definition]);
  const who = view.person.display_name || view.person.username;

  return (
    <Stack gap="md">
      <Text size="sm" c="dimmed">
        {t('taskLink.for', { name: who })}
      </Text>
      <Title order={2}>{view.task.name}</Title>
      {view.task.description && <Text>{view.task.description}</Text>}
      {submitError && (
        <Alert color="red" icon={<AlertCircle size={18} />}>
          {submitError}
        </Alert>
      )}
      {Array.isArray(fields) && fields.length > 0 ? (
        <TaskForm fields={fields} variables={view.task.variables || {}} onSubmit={onSubmit} loading={submitting} />
      ) : (
        <Group justify="flex-end">
          <Button loading={submitting} onClick={() => onSubmit({})}>
            {t('taskLink.complete')}
          </Button>
        </Group>
      )}
      <Text size="xs" c="dimmed">
        {t('taskLink.once')}
      </Text>
    </Stack>
  );
}
