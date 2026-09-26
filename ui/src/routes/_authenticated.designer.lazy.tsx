import { createLazyFileRoute } from '@tanstack/react-router'
import { ProcessDesigner } from '../pages/ProcessDesigner'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { useAppStore } from '../store/useAppStore'
import { useEffect } from 'react'

export const Route = createLazyFileRoute('/_authenticated/designer')({
  component: ProcessDesignerRoute,
})

function ProcessDesignerRoute() {
  const setActiveTab = useAppStore((state) => state.setActiveTab)
  const { definitionId, instanceId } = Route.useSearch()

  useEffect(() => {
    setActiveTab('designer')
  }, [setActiveTab])

  return (
    <ErrorBoundary>
      <ProcessDesigner
        definitionId={definitionId}
        instanceId={instanceId}
      />
    </ErrorBoundary>
  )
}
