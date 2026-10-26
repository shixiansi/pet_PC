import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';

// Svelte 5 mounts from an explicit call rather than the legacy `new App({target})`.
// The target is required to exist: index.html owns the element, and a silent
// fallback to document.body would hide a broken build behind a page that renders.
const target = document.getElementById('app');
if (!target) throw new Error('#app is missing from index.html');

mount(App, { target });