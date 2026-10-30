import { createLazyFileRoute } from '@tanstack/react-router'
import { ArchiveList } from '../pages/ArchiveList'
import { useAppStore } from '../store/useAppStore'
import { useEffect } from 'react'

export const Route = createLazyFileRoute('/_authenticated/archive')({
  component: ArchiveRoute,
})

function ArchiveRoute() {
  const { setActiveTab } = useAppStore()
  useEffect(() => {
    setActiveTab('archive')
  }, [setActiveTab])

  return <ArchiveList />
}
