import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

// Which view of the page: the accounts, or who holds which role. In the
// address, so the role matrix can be linked to and survives a reload.
const usersSearchSchema = z.object({
  tab: z.enum(['accounts', 'roles']).catch('accounts'),
})

export const Route = createFileRoute('/_authenticated/users')({
  validateSearch: (search) => usersSearchSchema.parse(search),
})
