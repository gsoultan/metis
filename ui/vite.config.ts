import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
// The backend the dev server proxies to. Override with METIS_BACKEND when the
// Go server runs on a different port.
const backend = process.env.METIS_BACKEND ?? 'http://localhost:8273'

// Deliberately not Vite's default 5173: every other Vite project claims it, so
// two checkouts open at once would fight over the port. Override with UI_PORT.
const port = Number(process.env.UI_PORT ?? 5273)

export default defineConfig({
  // Proxying /api keeps development same-origin, so the app talks to the
  // backend exactly as it does in production and no CORS is involved.
  server: {
    port,
    proxy: {
      '/api': { target: backend, changeOrigin: true },
      // Server-sent events for live process/task updates.
      '/events': { target: backend, changeOrigin: true, ws: true },
      // The liveness probe is served outside /api/v1, ahead of authentication,
      // so it needs its own entry — the sidebar reads the running build from it.
      '/healthz': { target: backend, changeOrigin: true },
    },
  },
  plugins: [
    // The router plugin must run before the JSX transform: it rewrites route
    // files, and plugin-react would otherwise compile them first.
    tanstackRouter({
      target: 'react',
      routesDirectory: './src/routes',
      generatedRouteTree: './src/routeTree.gen.ts',
      autoCodeSplitting: true,
    }),
    react(),
    tailwindcss(),
    /*
     * Installable, and usable on a bad connection.
     *
     * The primary persona for the inbox is somebody approving work from a
     * phone, often on a train or a warehouse floor. Without a service worker
     * every one of those page loads refetches the whole application over a
     * connection that may not finish.
     *
     * `prompt` rather than `autoUpdate`: this app is a workflow engine, and
     * swapping the running bundle underneath somebody half-way through filling
     * in an approval form is how a form gets lost. The user is asked, and
     * chooses when to reload.
     */
    VitePWA({
      registerType: 'prompt',
      // The service worker is a production concern. Registering one in dev
      // caches the very assets Vite is trying to hot-reload.
      devOptions: { enabled: false },
      includeAssets: ['favicon.svg', 'icon-192.png', 'icon-512.png', 'icon-maskable-512.png'],
      manifest: {
        name: 'Metis BPM',
        short_name: 'Metis',
        description:
          'Design, run and monitor business processes. BPMN 2.0 orchestration with a DMN decision engine.',
        id: '/',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        orientation: 'any',
        background_color: '#ffffff',
        theme_color: '#1c7ed6',
        icons: [
          { src: '/icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: '/icon-512.png', sizes: '512x512', type: 'image/png' },
          // Cropped to the platform's own shape, so it is full bleed with the
          // mark inside the safe circle.
          { src: '/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
        shortcuts: [
          { name: 'My Inbox', short_name: 'Inbox', url: '/inbox' },
          { name: 'Instances', short_name: 'Instances', url: '/instances' },
        ],
      },
      workbox: {
        // The hashed bundle is precached; index.html is not, because the SPA
        // fallback below serves it and a precached shell would pin the browser
        // to an old deploy.
        globPatterns: ['**/*.{js,css,woff2}'],
        navigateFallback: '/index.html',
        // Anything the server owns must reach the server. A navigation to an
        // API path or the event stream answered from a cache would be a lie.
        navigateFallbackDenylist: [/^\/api\//, /^\/events/, /^\/healthz/, /^\/readyz/],
        cleanupOutdatedCaches: true,
        runtimeCaching: [
          {
            /*
             * Reads only, and the network is always tried first.
             *
             * A cached task list shown while offline is useful; a cached one
             * shown while online is a stale answer about work somebody may
             * already have done. The five-second timeout is what makes a dead
             * connection fall back rather than hang.
             */
            urlPattern: ({ url, request }: { url: URL; request: Request }) =>
              request.method === 'GET' && url.pathname.startsWith('/api/'),
            handler: 'NetworkFirst',
            options: {
              cacheName: 'metis-api-reads',
              networkTimeoutSeconds: 5,
              expiration: { maxEntries: 200, maxAgeSeconds: 60 * 60 * 24 },
              cacheableResponse: { statuses: [200] },
            },
          },
          {
            urlPattern: /^https:\/\/fonts\.(googleapis|gstatic)\.com\//,
            handler: 'StaleWhileRevalidate',
            options: {
              cacheName: 'metis-fonts',
              expiration: { maxEntries: 20, maxAgeSeconds: 60 * 60 * 24 * 365 },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
        ],
      },
    }),
  ],
  build: {
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        // Chunks are split by *when they are needed*, not by package name.
        //
        // The goal is the first paint of the login screen: a signed-out user
        // should not download the diagram editor, the protobuf runtime or the
        // date library to see a username field.
        manualChunks: (id) => {
          if (!id.includes('node_modules')) return undefined;

          // React itself — changes rarely, cache it separately from everything.
          if (
            id.includes('node_modules/react/') ||
            id.includes('node_modules/react-dom/') ||
            id.includes('node_modules/scheduler/')
          ) {
            return 'vendor-react';
          }

          // Only the designer route mounts React Flow.
          if (id.includes('@xyflow')) return 'vendor-flow';

          // The Connect/protobuf runtime is only used once signed in.
          if (id.includes('@bufbuild') || id.includes('@connectrpc')) return 'vendor-rpc';

          // Date handling is used by the dates picker and a few views.
          if (id.includes('dayjs')) return 'vendor-dates';

          // Zod validates route search params, so it is needed eagerly — but
          // isolating it keeps it out of the catch-all, where a change to any
          // unrelated dependency would invalidate it in the browser cache.
          if (id.includes('node_modules/zod')) return 'vendor-validation';

          // The date pickers are split out of the Mantine bundle. They are used
          // by three surfaces — a task's due date, the inbox filter and
          // scheduling a version cutover — none of which is first paint, and
          // grouping them with @mantine/core put ~7 kB of calendar on the
          // critical path for every visitor who never opens one.
          if (id.includes('@mantine/dates')) return 'vendor-mantine-dates';
          if (id.includes('@mantine')) return 'vendor-mantine';
          if (id.includes('@tanstack')) return 'vendor-tanstack';
          if (id.includes('lucide-react')) return 'vendor-icons';
          return 'vendor';
        },
      },
    },
  },
})
