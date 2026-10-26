import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// base is './' because the built assets are served by Wails from an opaque
// http://wails.localhost origin, where absolute paths would escape the app.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: { outDir: 'dist', emptyOutDir: true },
});