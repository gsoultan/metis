import { Badge, Button, Group, PasswordInput, Stack, Text, TextInput, Tooltip } from '@mantine/core';
import { History, Lock, Pencil } from 'lucide-react';
import { useState } from 'react';

import { draftProblem, shownValue } from '../../domain/serverSettings';
import { useChangeServerSetting } from '../../hooks/useServerSettings';
import { useTranslation } from '../../i18n/context';
import type { ServerSetting } from '../../services/domains/serverSettingsService';

const SOURCE_COLOR = { environment: 'gray', saved: 'blue', default: 'teal' } as const;

interface Props {
  setting: ServerSetting;
  onShowHistory: (setting: ServerSetting) => void;
}

/**
 * One server setting: what it is, where its value comes from, and — unless the
 * environment sets it — a way to change it with a reason.
 *
 * A secret's field always starts empty. The server never sent its value, so
 * there is nothing to show, and typing replaces it.
 */
export function ServerSettingRow({ setting, onShowHistory }: Props) {
  const { t } = useTranslation();
  const change = useChangeServerSetting(t('settings.server.failed'));
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState('');
  const [reason, setReason] = useState('');
  const locked = setting.source === 'environment';
  const problem = draftProblem(setting, value, reason);

  const open = () => {
    setValue(setting.secret ? '' : setting.value);
    setReason('');
    setEditing(true);
  };
  const send = (next: string | null) =>
    change.mutate({ name: setting.name, value: next, reason }, { onSuccess: () => setEditing(false) });

  const Field = setting.secret ? PasswordInput : TextInput;
  return (
    <Stack gap={6} py="sm">
      <Group justify="space-between" wrap="nowrap" align="flex-start">
        <Stack gap={2}>
          <Group gap="xs">
            <Text fw={600} size="sm">{setting.label}</Text>
            <Badge size="xs" variant="light" color={SOURCE_COLOR[setting.source]}>
              {t(`settings.server.source.${setting.source}`)}
            </Badge>
            {setting.pending_restart && (
              <Badge size="xs" variant="light" color="orange">{t('settings.server.pendingRestart')}</Badge>
            )}
          </Group>
          <Text size="xs" c="dimmed">{setting.help}</Text>
          <Text size="sm" ff="monospace">
            {shownValue(setting, t('settings.server.notSet'), t('settings.server.secretSet'))}
          </Text>
          {setting.updated_by && (
            <Text size="xs" c="dimmed">
              {t('settings.server.changedBy', { name: setting.updated_by, when: new Date(setting.updated_at ?? '').toLocaleString() })}
            </Text>
          )}
        </Stack>
        <Group gap="xs" wrap="nowrap">
          {setting.updated_by && (
            <Button size="xs" variant="subtle" leftSection={<History size={14} />} onClick={() => onShowHistory(setting)}>
              {t('settings.server.history')}
            </Button>
          )}
          {locked ? (
            <Tooltip label={t('settings.server.lockedHelp')} multiline w={260}>
              <Badge leftSection={<Lock size={12} />} variant="outline" color="gray">{t('settings.server.locked')}</Badge>
            </Tooltip>
          ) : (
            !editing && (
              <Button size="xs" variant="light" leftSection={<Pencil size={14} />} onClick={open}
                aria-label={t('settings.server.changeNamed', { label: setting.label })}>
                {t('settings.server.change')}
              </Button>
            )
          )}
        </Group>
      </Group>

      {editing && (
        <Stack gap="xs" pl="md">
          <Field
            label={setting.label}
            description={setting.default ? t('settings.server.defaultIs', { value: setting.default }) : undefined}
            value={value}
            onChange={(e) => setValue(e.currentTarget.value)}
            autoComplete="off"
            error={value !== '' && problem === 'value' ? t('settings.server.badValue') : undefined}
          />
          <TextInput
            label={t('settings.server.reason')}
            placeholder={t('settings.server.reasonPlaceholder')}
            value={reason}
            onChange={(e) => setReason(e.currentTarget.value)}
            required
          />
          <Text size="xs" c="dimmed">
            {setting.applies === 'live' ? t('settings.server.appliesLive') : t('settings.server.appliesRestart')}
          </Text>
          <Group gap="xs">
            <Button size="xs" onClick={() => send(value)} disabled={problem !== null} loading={change.isPending}>
              {t('settings.server.save')}
            </Button>
            {setting.source === 'saved' && (
              <Button size="xs" variant="default" onClick={() => send(null)} disabled={reason.trim() === ''} loading={change.isPending}>
                {t('settings.server.useDefault')}
              </Button>
            )}
            <Button size="xs" variant="subtle" onClick={() => setEditing(false)}>{t('settings.server.cancel')}</Button>
          </Group>
        </Stack>
      )}
    </Stack>
  );
}
