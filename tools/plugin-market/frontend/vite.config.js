import { defineConfig } from 'vite';

// Vite 4, matching the pet's and the voice studio's frontends: same toolchain,
// same pinned reason (Wails v2's AssetServer integration is known to break with
// Vite 5+).
export default defineConfig({
  // Relative base so the bundle works when served from the embedded FS.
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // The webview is a known, modern Chromium, so no legacy transpilation.
    target: 'esnext',
    minify: false,
    sourcemap: false,
  },
  server: {
    strictPort: true,
  },
});
