import { Badge, Group } from '@mantine/core';

import { isPrivilegedRole, roleLabel } from '../../domain/roles';
import { useTranslation } from '../../i18n/context';
import type { ApiOrganizationUser } from '../../services/types';

/**
 * The roles an account holds, as the organization's list says: the ones held
 * here, then the ones held in every organization it belongs to — marked as
 * such, because this organization's administrators cannot take those away.
 */
export function AccountRoleBadges({ account }: { account: Pick<ApiOrganizationUser, 'roles' | 'organization_roles'> }) {
  const { t } = useTranslation();
  const colour = (role: string) => (isPrivilegedRole(role) ? 'red' : 'blue');

  return (
    <Group gap={4}>
      {(account.organization_roles ?? []).map((role) => (
        <Badge key={`here-${role}`} variant="light" size="sm" color={colour(role)}>
          {roleLabel(role)}
        </Badge>
      ))}
      {(account.roles ?? []).map((role) => (
        <Badge key={`everywhere-${role}`} variant="outline" size="sm" color={colour(role)}>
          {t('access.roleEverywhere', { role: roleLabel(role) })}
        </Badge>
      ))}
    </Group>
  );
}
