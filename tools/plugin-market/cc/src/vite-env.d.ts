/// <reference types="vite/client" />
//
// WHY THIS ONE-LINE FILE EXISTS: `import.meta.env.DEV` is how bridge.ts decides between
// fake data and the real bindings, and `import './app.css'` in main.ts is a side-effect
// import with no declaration behind it. Neither is an error at runtime - Vite defines
// both - but svelte-check is a plain TypeScript program, so without vite's ambient types
// it reports "Property 'env' does not exist on type 'ImportMeta'" and "Cannot find module
// ... for side-effect import". Two red errors that were not about the code are exactly the
// kind that teach you to ignore the check.
