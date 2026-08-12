import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { MainLayout } from '../containers/MainLayout'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { processService } from '../services/api'
import { useAppStore } from '../store/useAppStore'

export const Route = createFileRoute('/_authenticated')({
  component: AuthenticatedLayout,
  beforeLoad: async ({ location }) => {
    // If system is not configured, redirect to setup
    try {
      const { status } = await processService.getSetupStatus()
      if (!status?.is_initialized) {
        throw redirect({ to: '/setup' })
      }
    } catch (e) {
      if (e instanceof Error && 'to' in e) throw e
      // On network error, assume not configured and redirect to setup
      throw redirect({ to: '/setup' })
    }

    // If not authenticated, redirect to login
    const { user, token } = useAppStore.getState()
    if (!user || !token) {
      throw redirect({
        to: '/login',
        search: {
          redirect: location.href,
        },
      })
    }
  },
})

function AuthenticatedLayout() {
  // activeTab/onTabChange were passed here and never read by MainLayout —
  // navigation state lives in the router, not the store.
  return (
    <MainLayout>
      <ErrorBoundary>
        <Outlet />
      </ErrorBoundary>
    </MainLayout>
  )
}
