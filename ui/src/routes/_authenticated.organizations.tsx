import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

// `new` opens the form for a new organization, so the header's picker can
// offer it from any page.
const organizationsSearchSchema = z.object({
  new: z.boolean().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/organizations')({
  validateSearch: (search) => organizationsSearchSchema.parse(search),
})
