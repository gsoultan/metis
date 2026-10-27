import { ActionIcon, Alert, Badge, Button, Code, CopyButton, Group, Paper, Stack, Table, Text, TextInput, Title, Tooltip } from '@mantine/core';
import { AlertCircle, KeySquare, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from '../i18n/context';
import { PeopleSessionEnded, personKeyService, type PersonAPIKey } from '../services/domains/peopleService';
import { errorMessage } from '../services/shared/errors';

/**
 * The keys a person gives their own systems, each named for the system that
 * uses it so one can be revoked without the others. A key is shown once, when
 * it is made.
 */
export function PersonKeys({ token, onEnded }: { token: string; onEnded: () => void }) {
  const { t } = useTranslation();
  const [keys, setKeys] = useState<PersonAPIKey[] | null>(null);
  const [name, setName] = useState('');
  const [fresh, setFresh] = useState<{ name: string; token: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const failed = useCallback(
    (failure: unknown) => {
      if (failure instanceof PeopleSessionEnded) {
        onEnded();
        return;
      }
      setError(errorMessage(failure, t('people.keys.failed')));
    },
    [onEnded, t],
  );

  useEffect(() => {
    const controller = new AbortController();
    personKeyService
      .list(token, controller.signal)
      .then((list) => {
        if (!controller.signal.aborted) setKeys(list);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) failed(failure);
      });
    return () => controller.abort();
  }, [token, failed]);

  const create = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const made = await personKeyService.create(token, name.trim());
      setFresh({ name: made.key.name, token: made.token });
      setName('');
      setKeys(await personKeyService.list(token));
    } catch (failure) {
      failed(failure);
    } finally {
      setBusy(false);
    }
  };

  const revoke = async (id: string) => {
    setBusy(true);
    setError(null);
    try {
      await personKeyService.revoke(token, id);
      setKeys(await personKeyService.list(token));
    } catch (failure) {
      failed(failure);
    } finally {
      setBusy(false);
    }
  };

  const live = (keys ?? []).filter((key) => !key.revoked_at);

  return (
    <Paper withBorder p="lg" radius="md">
      <Stack gap="sm">
        <Group gap="xs">
          <KeySquare size={18} />
          <Title order={4}>{t('people.keys.title')}</Title>
        </Group>
        <Text size="sm" c="dimmed">
          {t('people.keys.about')}
        </Text>
        {error && (
          <Alert color="red" icon={<AlertCircle size={18} />} withCloseButton onClose={() => setError(null)}>
            {error}
          </Alert>
        )}
        {fresh && (
          <Alert color="teal" title={t('people.keys.madeTitle', { name: fresh.name })} withCloseButton onClose={() => setFresh(null)}>
            <Stack gap="xs">
              <Text size="sm">{t('people.keys.madeOnce')}</Text>
              <Group gap="xs" wrap="nowrap">
                <Code style={{ wordBreak: 'break-all' }}>{fresh.token}</Code>
                <CopyButton value={fresh.token}>
                  {({ copied, copy }) => (
                    <Button size="xs" variant="light" onClick={copy}>
                      {copied ? t('page.people.copied') : t('page.people.copy')}
                    </Button>
                  )}
                </CopyButton>
              </Group>
            </Stack>
          </Alert>
        )}
        {live.length > 0 && (
          <Table verticalSpacing="xs">
            <Table.Tbody>
              {live.map((key) => (
                <Table.Tr key={key.id}>
                  <Table.Td>
                    <Text size="sm" fw={600}>{key.name}</Text>
                    <Text size="xs" c="dimmed" ff="monospace">{key.prefix}…</Text>
                  </Table.Td>
                  <Table.Td>
                    <Badge size="sm" variant="light" color={key.last_used_at ? 'teal' : 'gray'}>
                      {key.last_used_at
                        ? t('people.keys.lastUsed', { date: new Date(key.last_used_at).toLocaleString() })
                        : t('people.keys.neverUsed')}
                    </Badge>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Tooltip label={t('people.keys.revoke')}>
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        aria-label={t('people.keys.revokeNamed', { name: key.name })}
                        disabled={busy}
                        onClick={() => revoke(key.id)}
                      >
                        <Trash2 size={16} />
                      </ActionIcon>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        )}
        <form onSubmit={create}>
          <Group align="flex-end" wrap="nowrap">
            <TextInput
              style={{ flex: 1 }}
              label={t('people.keys.name')}
              placeholder={t('people.keys.namePlaceholder')}
              value={name}
              maxLength={100}
              onChange={(event) => setName(event.currentTarget.value)}
            />
            <Button type="submit" loading={busy} disabled={!name.trim()}>
              {t('people.keys.make')}
            </Button>
          </Group>
        </form>
      </Stack>
    </Paper>
  );
}
