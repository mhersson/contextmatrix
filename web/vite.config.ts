import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: (id) => {
          if (id.includes('node_modules')) {
            if (id.includes('react-md-editor') || id.includes('react-markdown-preview')) return 'md-editor'
            if (id.includes('@dnd-kit')) return 'dnd-kit'
            if (id.includes('react-router')) return 'react-router'
            if (id.match(/[\\/]react[\\/]|[\\/]react-dom[\\/]/)) return 'react'
            return 'vendor'
          }
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test-setup.ts',
  },
})
