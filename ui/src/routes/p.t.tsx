import { createFileRoute } from '@tanstack/react-router'

/*
 * The page a task link opens. Public: the link is the credential, so there is
 * no sign-in to require and no workspace to redirect to.
 */
export const Route = createFileRoute('/p/t')({})
