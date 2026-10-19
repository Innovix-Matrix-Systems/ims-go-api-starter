import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'

export default defineConfig({
  base: '/dev/',
  plugins: [
    tanstackStart({
      router: { basepath: '/dev' },
      spa: { enabled: true },
    }),
    viteReact(),
  ],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/dev/api': 'http://127.0.0.1:8080',
      '/api': 'http://127.0.0.1:8080',
      '/docs': 'http://127.0.0.1:8080',
    },
  },
})
