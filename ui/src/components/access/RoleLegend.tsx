import { List, ScrollArea, Stack, Text } from '@mantine/core';

import { areaHeadingKey, type LegendArea } from '../../domain/roleLegend';
import type { RoleOption } from '../../domain/roles';
import { useTranslation } from '../../i18n/context';

/** What the server has said about a role so far. */
export type LegendState =
  | { status: 'loading' }
  | { status: 'failed' }
  | { status: 'known'; areas: LegendArea[] };

/** Tall enough for a designer's list; an administrator's scrolls. */
const LEGEND_MAX_HEIGHT = 320;

/**
 * What one role is required for: the actions whose gates admit it, under the
 * area each belongs to, as the server read them from the gates.
 *
 * The role's sentence stays as the summary above them. The list is the part
 * that cannot drift from what the server enforces.
 */
export function RoleLegend({ option, legend }: { option: RoleOption; legend: LegendState }) {
  const { t } = useTranslation();

  return (
    <Stack gap="xs">
      <Text fw={700} size="sm">
        {option.label}
      </Text>
      <Text size="sm" c="dimmed">
        {option.description}
      </Text>
      <Text size="xs" fw={700} tt="uppercase" c="dimmed">
        {t('access.requiredFor')}
      </Text>
      <LegendBody legend={legend} />
      <Text size="xs" c="dimmed">
        {t('access.everythingElse')}
      </Text>
    </Stack>
  );
}

function LegendBody({ legend }: { legend: LegendState }) {
  const { t } = useTranslation();

  if (legend.status === 'loading') {
    return <Text size="sm">{t('common.loading')}</Text>;
  }
  if (legend.status === 'failed') {
    return <Text size="sm">{t('access.legendUnavailable')}</Text>;
  }
  if (legend.areas.length === 0) {
    return <Text size="sm">{t('access.requiredForNothing')}</Text>;
  }
  // Focusable and named, so the popover's focus lands here when it opens: the
  // arrow keys then scroll a long list, and a screen reader says what it is.
  return (
    <ScrollArea.Autosize
      mah={LEGEND_MAX_HEIGHT}
      type="auto"
      viewportProps={{ tabIndex: 0, role: 'group', 'aria-label': t('access.requiredFor') }}
    >
      <Stack gap="xs">
        {legend.areas.map(({ area, actions }) => (
          <div key={area}>
            <Text size="sm" fw={600}>
              {t(areaHeadingKey(area))}
            </Text>
            <List size="sm" spacing={2}>
              {actions.map((action) => (
                <List.Item key={action.method}>{action.label}</List.Item>
              ))}
            </List>
          </div>
        ))}
      </Stack>
    </ScrollArea.Autosize>
  );
}
