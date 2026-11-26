# AGENTS.md

Frontend rules for work under `web/`. Locale and translation rules live in `web/src/i18n/AGENTS.md`. The UI i18n rules are below.

## Frontend

- Organize React modules by feature within `web/src/{pages,routes,components}`.
- Build new UI from the page that already shows the same kind of thing: use the option that the page uses on each UI primitive, and reuse its header, table, dialog or drawer, and the modules in `src/components/kit/`. Take each color from the theme: a status color (`success`, `warning`, `info`) for a status, `destructive` for an error, and a theme color such as `muted-foreground` or `chart-1` for the rest. `shadcn/no-raw-colors` fails on a palette color. Before you add a pattern or an option that no page uses, ask the maintainer.
- On a Button, a Badge, or a Label, pass layout classes, and pick the rest, color included, with an option: `<Badge size="sm">` for 10 px text, `<Label size="sm">` for 12 px text in a dense form, `<Button size="xs">` for a small button, `<Badge variant="warning">` (or `success`, `info`) for a status. An `Alert` takes the same three variants. `shadcn/no-restyle` fails on another class ("UI Primitive Styling" in `docs/linting.md`). A destructive confirmation takes `<AlertDialogAction variant="destructive">`, and on a Settings page the plain `ConfirmDialog` from the kit.
- A user preference that must follow the user to other browsers uses `useClientSetting` from `web/src/lib/client-settings.ts`. A plain `localStorage` write stays in one browser. `SYNCED_KEYS` and `SYNCED_PREFIXES` only control which local values a page load copies to the server when the server lacks them. A control that shows a default before the first read of the server takes no input until `useClientSettingsSettled` is true, as the **24 h** and **Live** tabs of the Dashboard do. A pick of the value that already shows saves nothing, and the server value then replaces it.
- A new worktree has no `web/node_modules`. Run `pnpm install --frozen-lockfile` in `web/` before `pnpm exec tsc`, `eslint`, or `pnpm check:i18n`. `npx tsc` without it does not run the TypeScript compiler.
- Before you write or run a frontend test, read `docs/standards/frontend-tests.md`.
- Before you build or change a tab of Settings, or a module in `src/components/kit/`, read `src/components/kit/AGENTS.md`.
- Before you change when a tab of `TorrentDetailsPanel.tsx` or the piece bar fetches, or change a `useSyncStream` listener, read "Torrent details" in `docs/agents/frontend-notes.md`.
- Before you add or review an API call, a page, or demo-only UI, read "getqui.com demo" in `docs/agents/frontend-notes.md`. Only the `/instances` pages run in the demo, so another page needs no demo route.

## i18n

- **Never hardcode user-facing text or raw backend values (e.g. `run.status`), in JSX or in the option tables JSX renders.** Create an `i18n` key in the relevant namespace and render it with `t()`.
- Translation fallbacks rely on the English locale only. Never hardcode one, not as `defaultValue: "English"` and not as `t(…) ?? "English"`. A computed key the checker cannot read falls back to a translated key outside its namespace (``t([`stateLabels.${state}`, "stateLabelFallback"])``), never to a raw backend value. A call that has to break this documents why in place.
- When you touch UI strings, locale JSON, `web/src/i18n/index.ts`, or formatter hooks, run `pnpm check:i18n`. `web/package.json` has a `check:i18n:*` script for most single checks and each non-English locale.
- When `check:i18n` flags a key, delete the key from every locale in the same change, or teach the scanner to see the reference if the UI does use it.
- A file that holds key properties, or calls a `t` that it gets as a parameter, but never calls `useTranslation` names its namespace with a `// i18n-namespace: <ns>` comment after its imports. `pnpm check:i18n` checks its keys against that namespace. A key written as `"ns:key"` names its own namespace and needs no directive: `resolveNamespaceAndKey` in `web/scripts/check-i18n-keys-lib.mjs` splits it.

## Code Review Rules

- When a PR adds a table or a list where the user picks rows, apply "Selection" in `docs/standards/frontend-review.md`. Report a bar for the picked rows that the PR builds by hand instead of `SelectionBar`, also one above the list, and name what looks different from the bar of the Search results.
- When a PR adds or changes `useState` in a component, apply the React state rule in `docs/standards/frontend-review.md`. Report it only when two values in the diff can reach a state that must not exist. Name that state and what the user sees, for example a spinner and an error at the same time.
