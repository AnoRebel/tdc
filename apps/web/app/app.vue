<script setup lang="ts">
// The session is resolved by the auth middleware on protected routes, so that
// it happens before a page decides whether to render or redirect. Public pages
// do not need it resolved first.
</script>

<template>
  <div class="app">
    <NuxtPage />
  </div>
</template>

<style>
:root {
  color-scheme: light dark;

  --surface: #ffffff;
  --surface-sunken: #f4f4f5;
  --border: #d4d4d8;
  --text: #18181b;
  --text-muted: #52525b;
  --accent: #1d4ed8;
  --accent-text: #ffffff;
  --danger: #b91c1c;
  --success: #15803d;
  --radius: 0.5rem;
}

@media (prefers-color-scheme: dark) {
  :root {
    --surface: #18181b;
    --surface-sunken: #27272a;
    --border: #3f3f46;
    --text: #f4f4f5;
    --text-muted: #a1a1aa;
    --accent: #60a5fa;
    --accent-text: #0b1120;
    --danger: #f87171;
    --success: #4ade80;
  }
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  background: var(--surface-sunken);
  color: var(--text);
  font-family:
    system-ui,
    -apple-system,
    "Segoe UI",
    sans-serif;
  line-height: 1.5;
}

.app {
  min-height: 100vh;
}

.loading {
  padding: 2rem;
  color: var(--text-muted);
}

/* Shared form and control styles, kept here while the application is small
   enough that a design system would be premature. */
.field {
  display: grid;
  gap: 0.35rem;
  margin-bottom: 1rem;
}

.field label {
  font-weight: 600;
  font-size: 0.875rem;
}

.field input {
  padding: 0.6rem 0.75rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  color: var(--text);
  font: inherit;
  width: 100%;
}

.field input:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.field .hint {
  font-size: 0.8125rem;
  color: var(--text-muted);
}

button {
  padding: 0.6rem 1rem;
  border: 1px solid transparent;
  border-radius: var(--radius);
  background: var(--accent);
  color: var(--accent-text);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}

button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

button.secondary {
  background: transparent;
  border-color: var(--border);
  color: var(--text);
}

button.danger {
  background: transparent;
  border-color: var(--danger);
  color: var(--danger);
}

button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.error {
  padding: 0.75rem;
  border-radius: var(--radius);
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
  font-size: 0.9375rem;
}
</style>
