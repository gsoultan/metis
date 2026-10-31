import { createLazyFileRoute } from '@tanstack/react-router'
import { ReleaseList } from '../pages/ReleaseList'
import { useAppStore } from '../store/useAppStore'
import { useEffect } from 'react'

export const Route = createLazyFileRoute('/_authenticated/releases')({
  component: ReleasesRoute,
})

function ReleasesRoute() {
  const { setActiveTab } = useAppStore()
  useEffect(() => {
    setActiveTab('releases')
  }, [setActiveTab])

  return <ReleaseList />
}
