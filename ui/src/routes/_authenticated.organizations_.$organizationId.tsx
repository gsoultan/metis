import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

// Which tab of the organization's page is open. In the address, so a tab can
// be linked to and survives a reload.
const organizationSearchSchema = z.object({
  tab: z.enum(['overview', 'members', 'projects', 'settings']).catch('overview'),
})

export const Route = createFileRoute('/_authenticated/organizations_/$organizationId')({
  validateSearch: (search) => organizationSearchSchema.parse(search),
})
