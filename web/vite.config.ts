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
    // The lazy-loaded md-editor chunk is ~1 MB: @uiw/react-md-editor drags in
    // react-markdown-preview plus the full rehype/remark/mdast/hast/micromark/
    // refractor toolchain. They all load together when the editor mounts, so
    // splitting further just adds HTTP round-trips. 1200 kB leaves headroom
    // for that chunk while still catching future accidental bloat.
    chunkSizeWarningLimit: 1200,
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
