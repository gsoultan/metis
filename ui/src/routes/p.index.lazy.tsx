import { createLazyFileRoute } from '@tanstack/react-router'
import { PeopleInboxPage } from '../pages/PeopleInboxPage'

export const Route = createLazyFileRoute('/p/')({
  component: PeopleInboxPage,
})
