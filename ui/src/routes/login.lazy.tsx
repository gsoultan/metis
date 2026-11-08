import { createLazyFileRoute } from '@tanstack/react-router'
import { Login } from '../pages/Login'

export const Route = createLazyFileRoute('/login')({
  component: LoginPage,
})

function LoginPage() {
  const { redirect: redirectTo, reason } = Route.useSearch()
  return <Login redirectTo={redirectTo} reason={reason} />
}
