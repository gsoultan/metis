import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { MainLayout } from '../containers/MainLayout'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { useAppStore } from '../store/useAppStore'
import { requireConfigured } from './guards'

export const Route = createFileRoute('/_authenticated')({
  component: AuthenticatedLayout,
  beforeLoad: async ({ location }) => {
    await requireConfigured()

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
