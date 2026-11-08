import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAppStore } from '../store/useAppStore'
import { z } from 'zod'
import { requireConfigured } from './guards'
import { signInReturnAddress } from '../domain/signInReturn'

const loginSearchSchema = z.object({
  redirect: z.string().optional(),
  // Set when the app sent the person here because the session ended, so the
  // page can say so rather than look like they were signed out for nothing.
  reason: z.enum(['expired']).optional().catch(undefined),
})

export const Route = createFileRoute('/login')({
  validateSearch: loginSearchSchema,
  beforeLoad: async ({ search }) => {
    await requireConfigured()

    // Already signed in: go where the sign-in would have gone.
    const { user, token } = useAppStore.getState()
    if (user && token) {
      throw redirect({ href: signInReturnAddress(search.redirect) })
    }
  },
})
