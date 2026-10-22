import { createLazyFileRoute } from '@tanstack/react-router'
import { AuditLog } from '../pages/AuditLog'
import { useAppStore } from '../store/useAppStore'
import { useEffect } from 'react'

export const Route = createLazyFileRoute('/_authenticated/audit')({
  component: AuditLogRoute,
})

function AuditLogRoute() {
  const { setActiveTab } = useAppStore()
  useEffect(() => {
    setActiveTab('audit')
  }, [setActiveTab])

  return <AuditLog />
}
