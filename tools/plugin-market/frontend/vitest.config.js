import { defineConfig } from 'vitest/config';

// The test runner is configured apart from vite.config.js on purpose, exactly as
// the pet's frontend does: a test environment in the shipped config would be dead
// weight in every production build, and mixing the two would make the jsdom
// globals look like they were available to the app.
//
// jsdom rather than a browser because the checks here are about what the page
// RENDERS and what it ASKS THE BACKEND TO DO - both of which the real markup plus
// a fake Wails bridge reproduce exactly. The live-GUI half (window frame, pixel
// columns) is covered by .dev/verify-plugin-market.ps1.
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.js'],
    // The app module installs listeners on import, so each test file gets a fresh
    // module registry rather than sharing one instance.
    isolate: true,
  },
});
