---
status: accepted
date: 2026-11-15
---

# Display profiles live in Client settings, behind one module

A Display profile is a set of Client settings under its own keys, not a table of its own. Only the frontend reads a profile. The `client_settings` store already treats every key as opaque, so the server needs no migration, no route, and no Swagger change. The client settings module maps each Display setting to the copy of the active profile of the browser. It does the same for each Sort and filters setting of a profile that keeps its own. The hooks that read settings do not know about profiles.

Each profile is its own entry, so two browsers that create a profile at the same time cannot lose one. A delete blanks the entry and its values and does not remove rows. It also writes a tombstone key that no edit writes, so an edit from a browser that missed the delete cannot bring the profile back. A page load pushes every local value that the server does not have, so a removed row comes back from another browser.

Two ESLint rules guard the module: `no-restricted-globals` blocks `localStorage`, and `@typescript-eslint/no-restricted-imports` blocks `readRaw` and `writeRaw`, outside the module and an allowlist.

If the theme joins profiles later, the server must read profiles before sign-in (ADR 0015), and this decision needs another look.
