import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';

// Svelte 5 mounts from an explicit call rather than the legacy `new App({target})`.
// The target is required to exist: index.html owns the element, and a silent
// fallback to document.body would hide a broken build behind a page that renders.
const target = document.getElementById('app');
if (!target) throw new Error('#app is missing from index.html');

// Any uncaught error or unhandled rejection becomes VISIBLE. The page that motivated this
// reported nothing at all - no error, no console output - and the only way to tell "the
// script never ran" from "the script ran and threw" is to render both cases.
function banner(text: string) {
  let el = document.getElementById('boot-error');
  if (!el) {
    el = document.createElement('pre');
    el.id = 'boot-error';
    el.style.cssText =
      'position:fixed;left:0;right:0;bottom:0;margin:0;padding:8px 12px;max-height:40vh;overflow:auto;' +
      'background:#3a1c1f;color:#ffd7d9;font:12px/1.5 monospace;z-index:99999';
    document.body.appendChild(el);
  }
  el.textContent += text + '\n';
}

window.addEventListener('error', (e) => banner('error: ' + (e.message || e.error)));
window.addEventListener('unhandledrejection', (e) =>
  banner('unhandledrejection: ' + ((e.reason && (e.reason.stack || e.reason.message)) || e.reason)),
);

mount(App, { target });