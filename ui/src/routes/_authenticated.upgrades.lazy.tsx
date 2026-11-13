import { createLazyFileRoute } from '@tanstack/react-router'
import { Upgrades } from '../pages/Upgrades'

export const Route = createLazyFileRoute('/_authenticated/upgrades')({
  component: Upgrades,
})
