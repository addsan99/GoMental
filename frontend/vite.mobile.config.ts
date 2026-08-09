import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import {renameSync} from 'node:fs'
import {resolve} from 'node:path'

export default defineConfig({
  base: './',
  plugins: [
    react(),
    {
      name: 'gomental-mobile-index',
      closeBundle() {
        renameSync(resolve('dist-mobile/mobile.html'), resolve('dist-mobile/index.html'))
      },
    },
  ],
  build: {
    outDir: 'dist-mobile',
    emptyOutDir: true,
    rollupOptions: {
      input: 'mobile.html',
    },
  },
})
