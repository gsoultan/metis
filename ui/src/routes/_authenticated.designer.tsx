import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

const designerSearchSchema = z.object({
  definitionId: z.string().optional(),
  instanceId: z.string().optional(),
  name: z.string().optional(),
  key: z.string().optional(),
  /** A starting diagram from domain/processTemplates.ts. */
  template: z.string().optional(),
})

export const Route = createFileRoute('/_authenticated/designer')({
  validateSearch: (search) => designerSearchSchema.parse(search),
})
