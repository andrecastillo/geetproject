import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development the SPA runs on 5173 and proxies to the Go server on 8080.
// In production the built bundle is embedded into the binary and served from
// the same origin, so no proxy exists and these paths just work.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
