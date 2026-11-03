import { createLazyFileRoute } from '@tanstack/react-router'
import { useEffect } from 'react'
import { OrganizationPage } from '../pages/OrganizationPage'
import { useAppStore } from '../store/useAppStore'

export const Route = createLazyFileRoute('/_authenticated/organizations_/$organizationId')({
  component: OrganizationRoute,
})

function OrganizationRoute() {
  const { setActiveTab } = useAppStore()
  useEffect(() => {
    setActiveTab('organizations')
  }, [setActiveTab])

  return <OrganizationPage />
}
