# AGENTS.md

Frontend rules for work under `web/`. Locale and translation rules live in `web/src/i18n/AGENTS.md`. The UI i18n rules are below.

## Frontend

- Organize React modules by feature within `web/src/{pages,routes,components}`.
- A user preference that must follow the user to other browsers uses `useClientSetting` from `web/src/lib/client-settings.ts`. A plain `localStorage` write stays in one browser. `SYNCED_KEYS` and `SYNCED_PREFIXES` only control which local values a page load copies to the server when the server lacks them.
- A new worktree has no `web/node_modules`. Run `pnpm install --frozen-lockfile` in `web/` before `pnpm exec tsc`, `eslint`, or `pnpm check:i18n`. `npx tsc` without it does not run the TypeScript compiler.
- Before you write or run a frontend test, read `docs/standards/frontend-tests.md`.
- Before you change when a tab of `TorrentDetailsPanel.tsx` or the piece bar fetches, or change a `useSyncStream` listener, read "Torrent details" in `docs/agents/frontend-notes.md`.
- Before you add an API call to the torrent list surface, or add demo-only UI, read "getqui.com demo" in `docs/agents/frontend-notes.md`.

## i18n

- **Never hardcode user-facing text or raw backend values (e.g. `run.status`), in JSX or in the option tables JSX renders.** Create an `i18n` key in the relevant namespace and render it with `t()`.
- Translation fallbacks rely on the English locale only. Never hardcode one, not as `defaultValue: "English"` and not as `t(…) ?? "English"`. A computed key the checker cannot read falls back to a translated key outside its namespace (``t([`stateLabels.${state}`, "stateLabelFallback"])``), never to a raw backend value. A call that has to break this documents why in place.
- When you touch UI strings, locale JSON, `web/src/i18n/index.ts`, or formatter hooks, run `pnpm check:i18n`. `web/package.json` has a `check:i18n:*` script for most single checks and each non-English locale.
- When `check:i18n` flags a key, delete the key from every locale in the same change, or teach the scanner to see the reference if the UI does use it.
- A data-only file that holds key properties but never calls `useTranslation` names its namespace with a `// i18n-namespace: <ns>` comment after its imports. A key written as `"ns:key"` names its own namespace and needs no directive: `resolveNamespaceAndKey` in `web/scripts/check-i18n-keys-lib.mjs` splits it.
- No UI text names SSH until #2917 ships. Before you add remote-instance text, read rollout step 4 in `docs/remote-backend-design.md`.

## Code Review Rules

- When a PR adds or changes `useState` in a component, apply the React state rule in `docs/standards/frontend-review.md`. Report it only when two values in the diff can reach a state that must not exist. Name that state and what the user sees, for example a spinner and an error at the same time.
