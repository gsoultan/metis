import { createLazyFileRoute } from '@tanstack/react-router'
import { useEffect } from 'react'
import { SdkSandbox } from '../pages/SdkSandbox'
import { useAppStore } from '../store/useAppStore'

export const Route = createLazyFileRoute('/_authenticated/sdk')({
  component: SdkSandboxRoute,
})

function SdkSandboxRoute() {
  const { setActiveTab } = useAppStore()
  useEffect(() => {
    setActiveTab('sdk')
  }, [setActiveTab])

  return <SdkSandbox />
}
