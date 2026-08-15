import { createFileRoute } from '@tanstack/react-router'
import { requireUnconfigured } from './guards'

export const Route = createFileRoute('/setup')({
  beforeLoad: requireUnconfigured,
})
