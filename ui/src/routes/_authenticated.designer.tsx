import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

const designerSearchSchema = z.object({
  definitionId: z.string().optional(),
  instanceId: z.string().optional(),
  name: z.string().optional(),
  key: z.string().optional(),
  /** A starting diagram from domain/processTemplates.ts. */
  template: z.string().optional(),
  /*
    Which half of the designer is on screen. In the URL so a simulation is
    linkable — "open this process in simulate mode" is a thing one person sends
    another when a model is doing something they cannot explain.
  */
  mode: z.enum(['design', 'simulate']).optional(),
})

export const Route = createFileRoute('/_authenticated/designer')({
  validateSearch: (search) => designerSearchSchema.parse(search),
})
