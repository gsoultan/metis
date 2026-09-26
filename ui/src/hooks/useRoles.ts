import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import {
  roleUpdate,
  sendRoleChange,
  withAccountRoles,
  type AccountList,
  type RoleChangeOutcome,
} from '../domain/roleChange';
import { roleService } from '../services/domains/roleService';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';
import type { ApiOrganizationUser } from '../services/types';
import { useUpdateUser } from './useUser';

/**
 * What each role is required for, as the server reads it from its gates.
 *
 * The same for everybody and changed only by a new server, so it is kept like
 * the rest of the installation's shape rather than refetched on every visit.
 */
export const useRoleLegend = () =>
  useQuery({
    queryKey: ['roles'],
    queryFn: ({ signal }) => roleService.listRoles(signal),
    staleTime: DIRECTORY_STALE_TIME,
  });

/**
 * Grants or revokes one role at a time, through the Accounts view's own user
 * update, and says which role is being saved for whom.
 *
 * Once the server accepts a change, the roles it now holds are written into
 * the account lists already read, and the update's own invalidation reads them
 * again behind that. A refused change writes nothing. While one of a person's
 * roles is being saved, the rest of their row waits, so every change to them
 * starts from the one before — see withAccountRoles.
 */
export function useRoleChange() {
  const queryClient = useQueryClient();
  const updateUser = useUpdateUser();
  const [saving, setSaving] = useState<ReadonlyMap<string, string>>(() => new Map());

  const change = async (account: ApiOrganizationUser, role: string, granted: boolean): Promise<RoleChangeOutcome> => {
    const update = roleUpdate(account, role, granted);
    setSaving((current) => new Map(current).set(account.id, role));
    try {
      const outcome = await sendRoleChange(update, (sent) => updateUser.mutateAsync(sent));
      if (outcome.changed) {
        queryClient.setQueriesData<AccountList>({ queryKey: ['users'] }, (list) =>
          withAccountRoles(list, update.id, update.roles),
        );
      }
      return outcome;
    } finally {
      setSaving((current) => {
        const next = new Map(current);
        next.delete(account.id);
        return next;
      });
    }
  };

  return { saving, change };
}
