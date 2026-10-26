import { createLazyFileRoute } from '@tanstack/react-router'
import { TaskLinkPage } from '../pages/TaskLinkPage'

export const Route = createLazyFileRoute('/p/t')({
  component: TaskLinkPage,
})
