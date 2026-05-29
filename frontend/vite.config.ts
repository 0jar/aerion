import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import path from 'path'

// Extensions live outside frontend/ at the repo root (../extensions/<name>/frontend/...).
// $extensions aliases the extensions dir so App.svelte and other host files
// can import extension Svelte components and stores cleanly. $wailsjs aliases
// the generated Wails bindings so deep extension files don't need ../ chains.
//
// Because extension Svelte/TS files live OUTSIDE frontend/, Rollup's default
// resolution doesn't find frontend/node_modules. The npm deps used by
// extensions (iconify, svelte) are aliased explicitly to the host's node_modules
// so a single dependency tree is shared. Add new entries here when extensions
// pull in additional npm packages.
const EXTENSIONS_DIR = path.resolve(__dirname, '../extensions')
const WAILSJS_DIR = path.resolve(__dirname, './wailsjs')
const NODE_MODULES_DIR = path.resolve(__dirname, './node_modules')

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: {
      '$lib': path.resolve('./src/lib'),
      '$': path.resolve('./src'),
      '$extensions': EXTENSIONS_DIR,
      '$wailsjs': WAILSJS_DIR,
      // Shared deps used by extensions — must resolve to the host's node_modules
      // because extension files live outside the frontend/ root.
      '@iconify/svelte': path.resolve(NODE_MODULES_DIR, '@iconify/svelte'),
      'svelte-i18n': path.resolve(NODE_MODULES_DIR, 'svelte-i18n'),
    },
  },
  optimizeDeps: {
    include: ['@iconify-json/mdi', '@iconify-json/lucide', '@iconify-json/heroicons', '@iconify-json/logos', '@iconify-json/simple-icons'],
  },
  build: {
    target: 'esnext',
    minify: 'esbuild',
    sourcemap: false,
    rollupOptions: {
      input: {
        main: path.resolve(__dirname, 'index.html'),
        composer: path.resolve(__dirname, 'composer.html'),
      },
    },
  },
  server: {
    strictPort: true,
    fs: {
      // Vite blocks file reads outside its root by default. Extensions live
      // at <repo>/extensions/, one level above the frontend root, so allow it.
      allow: ['..', EXTENSIONS_DIR],
    },
  },
})
