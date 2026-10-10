import { RouterProvider, createRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import './App.css'
import { routerFallbacks } from './routerFallbacks'
import { useAppStore } from './store/useAppStore'
import { endSessionOnRefusal, onSessionRefused } from './services/shared/sessionRefusal'

// The router used to be handed the whole app store as `context.auth` from a
// component that subscribed to all of it — so every store write, a sidebar
// toggle included, re-rendered RouterProvider. No route ever read that
// context: the guards call useAppStore.getState() directly.
// scrollRestoration here rather than the <ScrollRestoration /> component,
// which the router deprecated and warned about on every page load.
const router = createRouter({ routeTree, scrollRestoration: true, ...routerFallbacks })

// Register the router instance for type safety
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// A token the server no longer accepts ends the session here, the same way the
// sign-out item does — see services/shared/sessionRefusal.
onSessionRefused(endSessionOnRefusal(useAppStore, () => {
  void router.navigate({ to: '/login', search: { redirect: router.state.location.href } })
}))

function App() {
  return <RouterProvider router={router} />
}

export default App
