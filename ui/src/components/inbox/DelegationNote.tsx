import { Badge } from '@mantine/core';

import { useTranslation } from '../../i18n/context';

/** Says whose task this is, on work that was delegated to the reader. */
export function DelegationNote({ owner }: { owner: string }) {
  const { t } = useTranslation();
  return (
    <Badge size="xs" variant="light" color="indigo" styles={{ label: { textTransform: 'none' } }}>
      {t('handover.delegatedToYouBy', { owner })}
    </Badge>
  );
}
