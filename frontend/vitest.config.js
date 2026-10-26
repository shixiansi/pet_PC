import { defineConfig } from 'vitest/config';

// The frontend has no test runner in the Wails build, so this config is separate
// from vite.config.js on purpose: a test environment in the shipped config would
// be dead weight in every production build, and mixing the two would make the
// jsdom globals look like they were available to the app.
//
// jsdom rather than a browser because the checks below are about what the page
// RENDERS and what it ASKS THE BACKEND TO DO, both of which the real markup plus
// a fake Wails bridge reproduce exactly. The live-GUI half (window regions,
// compositing) is covered by .dev/check-frontend.cjs and by the manual checklist
// in docs/reminder-pomodoro.md.
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.js'],
    // The app module installs intervals and an event wire on import, so each
    // test file gets a fresh module registry rather than sharing one instance.
    isolate: true,
  },
});
