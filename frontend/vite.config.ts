import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  // host: true — інакше dev-сервер у контейнері слухає лише 127.0.0.1
  // і порт назовні не пробивається. strictPort, щоб не «поїхав» на 5174.
  server: {
    host: true,
    port: 5173,
    strictPort: true,
  },
  preview: {
    host: true,
    port: 5173,
    strictPort: true,
  },
})
