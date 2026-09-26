import { useQuery } from '@tanstack/react-query';

import { roleService } from '../services/domains/roleService';
import { DIRECTORY_STALE_TIME } from '../services/queryDefaults';

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
