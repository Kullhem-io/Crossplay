import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev, the Vite server proxies the API + WebSocket to the Go backend
// (default :3001) so the SPA and engine share one origin.
const backend = process.env.BACKEND ?? 'http://localhost:3001'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/ws': { target: backend, ws: true },
      '/healthz': { target: backend },
    },
  },
})
