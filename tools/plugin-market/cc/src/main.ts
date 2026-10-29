import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { Theme } from './lib/bridge';
import { syncThemeState } from './lib/theme.svelte';

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

// The colour scheme has to be on <html> BEFORE the first paint: `tokens.css` keys every colour off
// `data-theme`, and the plugin frames read the same attribute to pick theirs. Applying it after
// mount paints the wrong scheme for a frame; never applying it - which is what happened until now,
// the binding existed and nothing called it - leaves the window on the stylesheet's default
// whatever the user picked.
//
// TIMED OUT on purpose: a Wails binding that never settles would otherwise hold the window on a
// blank page for ever, and that is a worse failure than starting in the wrong scheme.
async function applyStoredTheme() {
  const asked = Promise.race([
    Theme(),
    new Promise<string>((_, reject) => setTimeout(() => reject(new Error('theme 没回应')), 1500)),
  ]);
  let theme = 'dark';
  try {
    theme = (await asked) === 'light' ? 'light' : 'dark';
  } catch {
    // No answer means no preference: the host's own normalizeTheme answers "dark" for anything
    // that is not "light", so this is the same answer the file would have given.
  }
  document.documentElement.dataset.theme = theme;
  syncThemeState();
}

await applyStoredTheme();
mount(App, { target });
