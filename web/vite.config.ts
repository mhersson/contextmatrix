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
    // Let Rolldown auto-split. The previous manualChunks rule pulled
    // `react/jsx-runtime` into the lazy md-editor chunk because the editor
    // imports it transitively, and Rolldown groups the matched id with all
    // its shared deps unless other rules explicitly claim them. The result:
    // index.html statically imported md-editor on cold load, blowing
    // initial transfer up from ~95 kB to ~456 kB gzip while size-limit
    // kept passing because it measures chunks in isolation. Auto-splitting
    // produces the right shape — md-editor lazy-loads when the editor
    // mounts, react/react-dom flow into the main chunk.
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test-setup.ts',
  },
})
