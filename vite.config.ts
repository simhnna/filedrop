import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  // Serve index.html for all routes so history-API navigation works in dev + preview
  appType: 'spa',
  build: {
    // Licenses of everything bundled into dist/, linked from the footer.
    // .txt so browsers display it instead of downloading it
    license: { fileName: 'licenses.txt' },
  },
})
