/**
 * What the router shows when a route fails, or when an address matches none.
 *
 * TanStack Router wraps every route in its own error boundary, which catches a
 * failing component, loader or beforeLoad before the app's ErrorBoundary ever
 * sees it. With no defaultErrorComponent it drew the router's bare built-in
 * message, and an unknown address drew its "Not Found" text — so the styled
 * fallback, with its way back, never appeared.
 */
import { lazy, Suspense, type ComponentProps } from 'react';
import { useNavigate, useRouter, type ErrorComponentProps } from '@tanstack/react-router';

/*
 * Loaded when first needed, not with the first paint. The router config is in
 * the entry chunk, and importing the fallback there pulled the whole shared
 * icon chunk into the first-paint payload for a page most sessions never see.
 */
const LazyFallback = lazy(() =>
  import('./DefaultErrorFallback').then((m) => ({ default: m.DefaultErrorFallback })),
);

function DefaultErrorFallback(props: ComponentProps<typeof LazyFallback>) {
  return (
    <Suspense fallback={null}>
      <LazyFallback {...props} />
    </Suspense>
  );
}

export function RouteErrorFallback({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  return (
    <DefaultErrorFallback
      error={error}
      onReset={() => {
        // reset() clears the boundary; invalidate() runs the route's loaders
        // and guards again, which is what failed when it was not a render.
        reset();
        void router.invalidate();
      }}
    />
  );
}

export function RouteNotFound() {
  const navigate = useNavigate();
  return (
    <DefaultErrorFallback
      title="This page does not exist"
      description="The address may be mistyped, or the page may have moved."
      resetLabel="Go to the dashboard"
      onReset={() => void navigate({ to: '/' })}
    />
  );
}
