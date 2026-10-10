import { RouteErrorFallback, RouteNotFound } from './components/RouteFallbacks';

/**
 * The router's fallbacks, kept apart from App so a test can make a router with
 * exactly what the app's has. See components/RouteFallbacks.
 */
export const routerFallbacks = {
  defaultErrorComponent: RouteErrorFallback,
  defaultNotFoundComponent: RouteNotFound,
};
