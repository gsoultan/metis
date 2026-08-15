import { createRootRoute, Outlet, ScrollRestoration } from '@tanstack/react-router'
import { MantineProvider } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import { useAppStore } from '../store/useAppStore'
import { theme } from '../theme'
import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  const { theme: colorScheme } = useAppStore()

  return (
    // defaultColorScheme="auto" rather than forcing light: a user who has never
    // chosen a scheme gets their operating system's setting. An explicit stored
    // preference still wins via forceColorScheme.
    <MantineProvider theme={theme} defaultColorScheme="auto" forceColorScheme={colorScheme}>
      <Notifications position="top-right" limit={4} />
      <Outlet />
      <ScrollRestoration />
    </MantineProvider>
  )
}
