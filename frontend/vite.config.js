import { defineConfig } from 'vite';

// Vite 4 is pinned deliberately. Wails v2's AssetServer integration has known
// breakage with Vite 5+ (which also requires a newer Node), and nothing here
// needs a newer toolchain.
export default defineConfig({
  // Relative base so the built bundle works when served from the embedded FS.
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // The webview is a known, modern Chromium, so no legacy transpilation.
    target: 'esnext',
    // Keep the output readable; the bundle is tiny.
    minify: false,
    sourcemap: false,
  },
  server: {
    strictPort: true,
  },
});
