import { createFileRoute } from '@tanstack/react-router'

/*
 * Where a person signs in and works their tasks. Public: the page signs in
 * with a People token of its own, never a User's session.
 */
export const Route = createFileRoute('/p/')({})
