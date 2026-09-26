import { ActionIcon, Group, Popover, Table, Text } from '@mantine/core';
import { Info } from 'lucide-react';

import type { RoleOption } from '../../domain/roles';
import { useTranslation } from '../../i18n/context';
import { RoleLegend, type LegendState } from './RoleLegend';

/**
 * A role's column heading, with what the role is required for one click (or
 * one Enter) away.
 *
 * A popover rather than a tooltip: an administrator's list runs to dozens of
 * actions and has to be scrolled, and a tooltip closes the moment the pointer
 * leaves the icon. The button carries the name a screen reader announces, and
 * Escape closes the popover and returns focus to it.
 */
export function RoleColumnHeader({ option, legend }: { option: RoleOption; legend: LegendState }) {
  const { t } = useTranslation();

  return (
    <Table.Th ta="center">
      <Group gap={4} justify="center" wrap="nowrap">
        <Text size="sm" fw={700}>
          {option.label}
        </Text>
        <Popover width={340} position="bottom" withArrow shadow="md" radius="md" returnFocus>
          <Popover.Target>
            <ActionIcon
              variant="subtle"
              color="gray"
              size="sm"
              aria-label={t('access.legendButton', { role: option.label })}
            >
              <Info size={14} aria-hidden />
            </ActionIcon>
          </Popover.Target>
          <Popover.Dropdown>
            <RoleLegend option={option} legend={legend} />
          </Popover.Dropdown>
        </Popover>
      </Group>
    </Table.Th>
  );
}
