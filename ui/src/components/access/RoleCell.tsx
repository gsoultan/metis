import { Box, Checkbox, Group, Loader, Table, Text, ThemeIcon, VisuallyHidden } from '@mantine/core';
import { Check } from 'lucide-react';

import { hasRole } from '../../domain/access';
import type { RoleOption } from '../../domain/roles';
import { useTranslation } from '../../i18n/context';
import type { ApiOrganizationUser } from '../../services/types';

interface RoleCellProps {
  account: ApiOrganizationUser;
  option: RoleOption;
  /**
   * Whether the person looking may change it. Nobody else is shown a control
   * that would only ever be refused; the server refuses them anyway.
   */
  canEdit: boolean;
  /** The role being saved for this account, while one is. */
  saving?: string;
  onToggle: (granted: boolean) => void;
}

/** The loader's size, and the room kept for it so a cell does not shift when it appears. */
const LOADER_SIZE = 12;

/**
 * Whether one person holds one role, read from the account as the server holds
 * it — so a change the server refuses leaves the box as it was.
 *
 * While a change to the person's roles is being saved, the row's boxes stay
 * focusable and ignore clicks, rather than being disabled: disabling the box
 * somebody just pressed Space on would throw their focus to the top of the page.
 */
export function RoleCell({ account, option, canEdit, saving, onToggle }: RoleCellProps) {
  const { t } = useTranslation();
  const name = account.full_name || account.username;
  const held = hasRole(account, option.value);

  if (!canEdit) {
    return (
      <Table.Td ta="center">
        {held ? (
          <ThemeIcon variant="light" color="indigo" size="sm" radius="xl" aria-hidden>
            <Check size={14} />
          </ThemeIcon>
        ) : (
          <Text span c="dimmed" aria-hidden>
            —
          </Text>
        )}
        <VisuallyHidden>{t(held ? 'access.holds' : 'access.lacks', { name, role: option.label })}</VisuallyHidden>
      </Table.Td>
    );
  }

  const busy = saving !== undefined;
  const savingThis = saving === option.value;
  return (
    <Table.Td ta="center">
      <Group gap={6} justify="center" wrap="nowrap">
        <Box w={LOADER_SIZE} aria-hidden />
        <Checkbox
          checked={held}
          aria-label={t('access.cell', { role: option.label, name })}
          aria-disabled={busy || undefined}
          style={busy ? { opacity: 0.5 } : undefined}
          onChange={(event) => {
            if (!busy) onToggle(event.currentTarget.checked);
          }}
        />
        <Box w={LOADER_SIZE}>{savingThis && <Loader size={LOADER_SIZE} aria-hidden />}</Box>
      </Group>
      {savingThis && (
        <VisuallyHidden role="status">{t('access.saving', { role: option.label, name })}</VisuallyHidden>
      )}
    </Table.Td>
  );
}
