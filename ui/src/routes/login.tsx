import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAppStore } from '../store/useAppStore'
import { z } from 'zod'
import { requireConfigured } from './guards'

const loginSearchSchema = z.object({
  redirect: z.string().optional(),
})

export const Route = createFileRoute('/login')({
  validateSearch: loginSearchSchema,
  beforeLoad: async () => {
    await requireConfigured()

    // If already authenticated, redirect to home
    const { user, token } = useAppStore.getState()
    if (user && token) {
      throw redirect({ to: '/' })
    }
  },
})
