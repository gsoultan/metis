import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'

// https://vite.dev/config/
// The backend the dev server proxies to. Override with GOBPM_BACKEND when the
// Go server runs on a different port.
const backend = process.env.GOBPM_BACKEND ?? 'http://localhost:8080'

export default defineConfig({
  // Proxying /api keeps development same-origin, so the app talks to the
  // backend exactly as it does in production and no CORS is involved.
  server: {
    port: 5173,
    proxy: {
      '/api': { target: backend, changeOrigin: true },
      // Server-sent events for live process/task updates.
      '/events': { target: backend, changeOrigin: true, ws: true },
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
  ],
  build: {
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes('node_modules')) {
            if (id.includes('@mantine')) return 'vendor-mantine';
            if (id.includes('@tanstack')) return 'vendor-tanstack';
            if (id.includes('@xyflow')) return 'vendor-flow';
            if (id.includes('lucide-react')) return 'vendor-icons';
            if (id.includes('node_modules/react/') || id.includes('node_modules/react-dom/') || id.includes('node_modules/scheduler/')) return 'vendor-react';
            return 'vendor';
          }
        },
      },
    },
  },
})
