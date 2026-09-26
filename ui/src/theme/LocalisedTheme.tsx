import { MantineThemeProvider } from '@mantine/core';
import { useMemo, type ReactNode } from 'react';

import { useTranslation } from '../i18n/context';
import { closeButtonLabels } from './closeButtonLabels';

/**
 * The theme's words, in the interface's language.
 *
 * The theme itself is built once, before anything knows which language is in
 * use, so the few words it carries — the name of a dialog's close button — are
 * laid over it here, inside the translation provider. Colours and spacing stay
 * with the provider above, which is the one that writes them to the page.
 */
export function LocalisedTheme({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const close = t('common.close');
  const labels = useMemo(() => closeButtonLabels(close), [close]);
  return <MantineThemeProvider theme={labels}>{children}</MantineThemeProvider>;
}
