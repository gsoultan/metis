import { Table, Text, ThemeIcon, VisuallyHidden } from '@mantine/core';
import { Check } from 'lucide-react';

import { hasRole } from '../../domain/access';
import type { RoleOption } from '../../domain/roles';
import { useTranslation } from '../../i18n/context';
import type { ApiOrganizationUser } from '../../services/types';

/**
 * Whether one person holds one role, read from the account as the server
 * holds it.
 *
 * The mark is for the eye and the sentence for a screen reader, which would
 * otherwise read a column of ticks with nothing to say whose they are.
 */
export function RoleCell({ account, option }: { account: ApiOrganizationUser; option: RoleOption }) {
  const { t } = useTranslation();
  const name = account.full_name || account.username;
  const held = hasRole(account, option.value);

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
      <VisuallyHidden>
        {t(held ? 'access.holds' : 'access.lacks', { name, role: option.label })}
      </VisuallyHidden>
    </Table.Td>
  );
}
