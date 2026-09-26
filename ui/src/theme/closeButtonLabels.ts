import {
  Alert,
  DrawerCloseButton,
  ModalCloseButton,
  Notification,
  createTheme,
  type MantineThemeOverride,
} from '@mantine/core';

/**
 * The name every icon-only close button is given, as theme defaults.
 *
 * Mantine draws the close button of a modal, a drawer, a notification and an
 * alert as an icon alone and names it nothing, so a screen reader announced
 * "button" and axe reported button-name, rated critical, on every dialog in
 * the app. Naming them one dialog at a time had reached three of them.
 *
 * The modal's and the drawer's are set on their close-button components rather
 * than through the dialog's `closeButtonProps`, so a dialog that passes other
 * props to its close button keeps the name, and one that names it keeps its
 * own. A notification and an alert take the name through the one prop each has
 * for it. Popovers draw no close button of their own, so there is nothing of
 * theirs to name.
 *
 * Deliberately not a default of `CloseButton` itself, which would reach every
 * close button at once: the same component is the clear button of a date
 * picker, where "Close" would say the wrong thing.
 */
export function closeButtonLabels(close: string): MantineThemeOverride {
  return createTheme({
    components: {
      ModalCloseButton: ModalCloseButton.extend({ defaultProps: { 'aria-label': close } }),
      DrawerCloseButton: DrawerCloseButton.extend({ defaultProps: { 'aria-label': close } }),
      Notification: Notification.extend({ defaultProps: { closeButtonProps: { 'aria-label': close } } }),
      Alert: Alert.extend({ defaultProps: { closeButtonLabel: close } }),
    },
  });
}
