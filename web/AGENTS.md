# AGENTS.md

Frontend rules for work under `web/`. i18n and translation rules live in `web/src/i18n/AGENTS.md`.

## Frontend

- React 19 + Vite + TypeScript + Tailwind v4.
- Organize React modules by feature within `web/src/{pages,routes,components}`.
- Theme fonts: every font family a theme names in `--font-sans/serif/mono` needs a `FONT_MAP` entry in `web/src/utils/fontLoader.ts` (Google Fonts spec, or `""` for a system font), or the browser silently falls back. `fontLoader.test.ts` enforces this for bundled themes; sideloaded community themes are best-effort.

## Frontend Tests

- Vitest + React Testing Library. A new test goes in the `__tests__/` folder of the directory it covers (`components/cross-seed/__tests__/`) where that folder exists, else beside the file as `*.test.ts(x)`.
- Prefer extracting logic into hooks (`web/src/hooks/`) and pure helpers (`web/src/lib/`) so it is unit-testable without mounting the whole tree (see `web/src/hooks/torrent-table/` for the pattern).
- Vitest runs with `globals: false` + jsdom. There **is** a setup file (`web/src/test/setup.ts`), but it only runs the MSW server lifecycle and stubs `localStorage` when Node has none:
  - Import test globals explicitly: `import { describe, it, expect, vi } from "vitest"`; use `render` / `renderHook` / `act` from `@testing-library/react`.
  - **Nothing auto-cleans the DOM or mocks.** Add `afterEach(cleanup)` in files where more than one test renders, and call `cleanup()` or `unmount()` yourself between two `render` calls inside the same test. Add `afterEach(() => vi.restoreAllMocks())` when a test spies on a global such as `Storage.prototype.setItem`. RTL registers its own cleanup only when a global `afterEach` exists, and `globals: false` removes it; `restoreMocks` is not set either. Without this a second `render` leaves the first in the DOM and `getBy*` throws "Found multiple elements".
  - No jest-dom matchers (`toBeInTheDocument`, `toHaveTextContent`, …) — assert plain DOM: `el.textContent`, `el.getAttribute(...)`, `expect(node).toBeNull()`.
  - When mounting components with effects, mock their boundaries (`@/lib/api`, router, context providers, `useVirtualizer`, query hooks) and return a **stable singleton** from each mock — fresh objects per render loop effects and OOM the worker. Use `vi.hoisted()` for values referenced inside `vi.mock` factories.
- jsdom does no real layout, scroll, or pointer/drag work — it renders **zero virtual rows** and cannot exercise virtualization, dnd-kit, or scroll restoration. Unit-test the extractable logic (reorder math, row-height mapping, handler wiring) and **manually smoke** anything visual or interactive; a green suite is not full coverage. Run targeted with `cd web && npx vitest run <path>`; CI gates for `web/`: `lint.yml` runs eslint on changed files and `pnpm check:i18n`; `release.yml` runs `pnpm test`, `pnpm tsc --noEmit`, and the build.

## i18n

- **Never hardcode user-facing text or raw backend values (e.g. `run.status`), in JSX or in the option tables JSX renders.** Create an `i18n` key in the relevant namespace and render it with `t()`.
- When you touch UI strings, locale JSON, `web/src/i18n/index.ts`, or formatter hooks, run `pnpm check:i18n`. `web/package.json` has a `check:i18n:*` script for each single check and each locale.
- `check:i18n` checks both directions: `check-i18n-keys.mjs` that every key in a literal `t("…")` call or a `labelKey`, `titleKey`, `placeholderKey`, or `descriptionKey` string exists, and `find-unused-i18n-keys.mjs` that every English key is still reachable from `web/src`. When it flags a key, delete the key from every locale in the same change, or teach the scanner to see the reference if the UI does use it.
- A data-only file that holds key properties but never calls `useTranslation` names its namespace with a `// i18n-namespace: <ns>` comment after its imports. A key written as `"ns:key"` names its own namespace and needs no directive: `resolveNamespaceAndKey` in `web/scripts/check-i18n-keys-lib.mjs` splits it.
- No UI text names SSH until #2917 ships. Before you add remote-instance text, read rollout step 4 in `docs/remote-backend-design.md`.

## Torrent Details Note

`web/src/components/torrents/TorrentDetailsPanel.tsx` live row state is stream-backed via `useSyncStream`; polling is fallback while stream unavailable. Content/files and Peers tabs still poll on interval, but polling is tab-scoped and visibility-gated.

`useSyncStream` listeners receive raw frames. A delta for unchanged rows carries an empty `torrents` list and the previous `total`, so a handler clears its row only on `total === 0` and keeps the previous row on an empty list.

## getqui.com Demo

`pnpm build:demo` builds the unchanged app in Vite mode `demo` for the landing page at getqui.com/demo/. `web/src/demo/` replaces `window.fetch` and `window.EventSource` with an in-memory store; there is no backend.

- A new API call on the torrent list surface needs a route in `web/src/demo/api.ts`, or the demo answers `404 {"error": "not available in the demo"}` and the feature looks broken on the site.
- Demo-only UI branches use `isDemo` from `web/src/lib/demo.ts`. Vite folds it, so production bundles carry none of them.
- Pages outside `/instances` are redirected in the demo; do not add demo handling to them.
