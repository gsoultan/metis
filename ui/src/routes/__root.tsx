import { lazy, Suspense } from 'react'
import { createRootRoute, Outlet } from '@tanstack/react-router'
import { MantineProvider } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import { TranslationProvider } from '../i18n/TranslationProvider'
import { useAppStore } from '../store/useAppStore'
import { cssVariablesResolver, theme } from '../theme'
import { LocalisedTheme } from '../theme/LocalisedTheme'
import '@mantine/core/styles.css'
// @mantine/dates has its own stylesheet, imported by the three components that
// use a picker rather than here — see the note in TaskForm. It had never been
// imported anywhere at all, so the task form's due date and the inbox filter
// were both rendering unstyled.
import '@mantine/notifications/styles.css'

/*
 * Loaded after the first paint, not with it.
 *
 * Registering the service worker is housekeeping: nothing on screen depends on
 * it, and the bundle budget is measured on what the browser must download
 * before it can render anything. Kept eager it cost about 4 kB gzipped of the
 * critical path and pushed the first paint over its ceiling.
 */
const ServiceWorkerPrompt = lazy(() =>
  import('../pwa/ServiceWorkerPrompt').then((m) => ({ default: m.ServiceWorkerPrompt })),
)

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  const colorScheme = useAppStore((state) => state.theme)

  return (
    // defaultColorScheme="auto" rather than forcing light: a user who has never
    // chosen a scheme gets their operating system's setting. An explicit stored
    // preference still wins via forceColorScheme.
    <MantineProvider
      theme={theme}
      cssVariablesResolver={cssVariablesResolver}
      defaultColorScheme="auto"
      forceColorScheme={colorScheme}
    >
      <TranslationProvider>
        {/* Names every dialog's close button, in the interface's language. */}
        <LocalisedTheme>
          <Notifications position="top-right" limit={4} />
          <Outlet />
          {/* Says when a new version is waiting, when the connection has gone,
              and what is still waiting to be sent. */}
          <Suspense fallback={null}>
            <ServiceWorkerPrompt />
          </Suspense>
        </LocalisedTheme>
      </TranslationProvider>
    </MantineProvider>
  )
}
