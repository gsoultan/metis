/*
 * Inter, self-hosted. The weights are the ones the Mantine theme and index.css
 * actually ask for — importing the variable font or the full family would ship
 * every weight from 100 to 900 for the four we use.
 *
 * See index.html for why this is not a fonts.googleapis.com stylesheet.
 */
import '@fontsource/inter/400.css';
import '@fontsource/inter/500.css';
import '@fontsource/inter/600.css';
import '@fontsource/inter/700.css';

import './styles/tailwind.css';
import { queryClientDefaults } from './services/queryDefaults';

/*
 * Accessibility checks in development.
 *
 * The lint rule catches missing labels at build time; axe catches what only
 * exists once rendered — contrast ratios, ARIA relationships, focus order,
 * duplicate landmarks. Violations print to the browser console.
 *
 * Dev only: it walks the DOM after every render and would be a needless cost
 * in production.
 */
if (import.meta.env.DEV) {
  void (async () => {
    const [{ default: axe }, React, ReactDOM] = await Promise.all([
      import('@axe-core/react'),
      import('react'),
      import('react-dom'),
    ]);
    void axe(React, ReactDOM, 1000);
  })();
}
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './index.css'
import App from './App.tsx'

// Without defaults every query refetches on every mount and focus — see
// services/queryDefaults for what each resource's rate of change justifies.
const queryClient = new QueryClient({ defaultOptions: queryClientDefaults })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
