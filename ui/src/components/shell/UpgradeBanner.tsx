import { Anchor, Group, Text } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import { Link, useLocation } from '@tanstack/react-router';
import { ArrowUpCircle } from 'lucide-react';

import { PRIVILEGED_ROLE } from '../../domain/roles';
import { hasRole } from '../../domain/access';
import { upgradeAvailable } from '../../domain/upgrades';
import { useViewerAccess } from '../../hooks/useViewerAccess';
import { UPGRADE_STATUS_KEY } from '../../pages/upgradeStatusKey';
import { upgradeService } from '../../services/domains/upgradeService';

/**
 * Tells an administrator that a newer release of Metis is out, on every page
 * but the Upgrades page itself. Nobody else is asked: the server would refuse
 * them, and the version an installation runs is not theirs to act on.
 */
export function UpgradeBanner() {
  const isAdministrator = hasRole(useViewerAccess(), PRIVILEGED_ROLE);
  const location = useLocation();
  const { data } = useQuery({
    queryKey: UPGRADE_STATUS_KEY,
    queryFn: ({ signal }) => upgradeService.getStatus(signal),
    enabled: isAdministrator,
    // The server checks every six hours; asking it more than every ten
    // minutes would only repeat its answer.
    staleTime: 10 * 60_000,
    retry: false,
  });

  if (!isAdministrator || location.pathname === '/upgrades' || !upgradeAvailable(data) || data?.newest === undefined) {
    return null;
  }

  return (
    <Group
      role="status"
      gap="xs"
      px="xl"
      py={6}
      style={{ backgroundColor: 'light-dark(var(--mantine-color-blue-0), var(--mantine-color-dark-6))' }}
    >
      <ArrowUpCircle size={16} />
      <Text size="sm">
        <b>Metis {data.newest.tag} is available.</b> This installation runs {data.current}.
      </Text>
      <Anchor component={Link} to="/upgrades" size="sm" fw={600} ml="auto">
        See what is new
      </Anchor>
    </Group>
  );
}
