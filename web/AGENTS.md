# AGENTS.md

Frontend and i18n rules for work under `web/`.

## Frontend

- React 19 + Vite + TypeScript + Tailwind v4.
- Source: `web/src`; static assets: `web/public`.
- Production bundle must stay synced to `internal/web/dist` via `make frontend` or `make build`.
- Organize React modules by feature within `web/src/{pages,routes,components}`.
- File names should be descriptive, e.g. `torrent-table.tsx`.
- Style: two-space indentation, double quotes, trailing commas on multiline literals, Unix line endings.
- Frontend tests: Vitest + React Testing Library, colocated as `*.test.tsx` near the component.
- Theme fonts: every font family a theme names in `--font-sans/serif/mono` needs a `FONT_MAP` entry in `web/src/utils/fontLoader.ts` (Google Fonts spec, or `""` for a system font), or the browser silently falls back. `fontLoader.test.ts` enforces this for bundled themes; sideloaded community themes are best-effort.
- Field help goes in a tooltip on the field label. Use `FieldHelp` from `@/components/ui/field-help`. Do not add a help paragraph under the control.
- Keep this text inline, never in a tooltip: error and validation messages, warnings about data loss or actions the user cannot undo, and text the user must read before they choose.
- Per-option text in a radio group or a checkbox list stays inline. The user compares the options side by side and cannot do that through hovers.
- Status text, computed previews, section intros, and empty states are not field help. The rule does not apply to them.
- If the help text only repeats the label, delete it. Do not move it to a tooltip.

## Frontend Tests

- Colocate `*.test.ts(x)` specs with the change. Prefer extracting logic into hooks (`web/src/hooks/`) and pure helpers (`web/src/lib/`) so it is unit-testable without mounting the whole tree (see `web/src/hooks/torrent-table/` for the pattern).
- Vitest runs with `globals: false` + jsdom. There **is** a setup file (`web/src/test/setup.ts`), but it only runs the MSW server lifecycle:
  - Import test globals explicitly: `import { describe, it, expect, vi } from "vitest"`; use `render` / `renderHook` / `act` from `@testing-library/react`.
  - **Nothing auto-cleans the DOM or mocks.** Add `afterEach(cleanup)` in files where more than one test renders, and call `cleanup()` or `unmount()` yourself between two `render` calls inside the same test. Add `afterEach(() => vi.restoreAllMocks())` when a test spies on a global such as `Storage.prototype.setItem`. RTL registers its own cleanup only when a global `afterEach` exists, and `globals: false` removes it; `restoreMocks` is not set either. Without this a second `render` leaves the first in the DOM and `getBy*` throws "Found multiple elements".
  - No jest-dom matchers (`toBeInTheDocument`, `toHaveTextContent`, …) — assert plain DOM: `el.textContent`, `el.getAttribute(...)`, `expect(node).toBeNull()`.
  - When mounting components with effects, mock their boundaries (`@/lib/api`, router, context providers, `useVirtualizer`, query hooks) and return a **stable singleton** from each mock — fresh objects per render loop effects and OOM the worker. Use `vi.hoisted()` for values referenced inside `vi.mock` factories.
- jsdom does no real layout, scroll, or pointer/drag work — it renders **zero virtual rows** and cannot exercise virtualization, dnd-kit, or scroll restoration. Unit-test the extractable logic (reorder math, row-height mapping, handler wiring) and **manually smoke** anything visual or interactive; a green suite is not full coverage. Run targeted with `cd web && npx vitest run <path>`; CI runs the full suite via `make test-frontend`.

## React Effects

- Use `useEffect` only to sync with external systems: DOM, subscriptions, network.
- Avoid derived state in Effects; calculate during render or use `useMemo` for expensive compute.
- Put user-driven logic in event handlers.
- To reset state, prefer a `key` or render-time adjustment.
- Fetch Effects must guard stale responses with cleanup/abort.
- Reference: https://react.dev/learn/you-might-not-need-an-effect

## i18n

Locales live under `web/src/i18n/locales/<lang>/` with 10 namespaces:

`common`, `auth`, `settings`, `torrents`, `dashboard`, `crossseed`, `rss`, `search`, `instances`, `automations`

English is fallback/eager-loaded. Other languages are lazy-loaded by `initI18n()` / `changeLanguage()` through `import.meta.glob` in `web/src/i18n/index.ts`. Supported today: `en`, `zh-CN`, `zh-TW`, `fr`, `de`, `cs`, `it`, `ko`, `uk`, `pt-BR`, `ca`.

## i18n Commands

- `pnpm check:i18n`
- `pnpm check:i18n:plural-keys`
- `pnpm check:i18n:hardcoded`
- `pnpm check:i18n:raw-backend-values`
- `pnpm check:i18n:unused`
- `pnpm check:i18n:zh-cn`
- `pnpm check:i18n:zh-tw`
- `pnpm check:i18n:fr`
- `pnpm check:i18n:de`
- `pnpm check:i18n:cs`
- `pnpm check:i18n:it`
- `pnpm check:i18n:ko`
- `pnpm check:i18n:uk`
- `pnpm check:i18n:pt-br`
- `pnpm check:i18n:ca`

Run relevant checks when touching UI strings, locale JSON, `web/src/i18n/index.ts`, or formatter hooks.

`check:i18n` checks both directions: `check-i18n-keys.mjs` that every key the UI asks for exists, and `find-unused-i18n-keys.mjs` that every English key is still reachable from `web/src`. The second one is a ratchet over a backlog of keys that were already dead when it landed — its `knownUnusedKeys` list may shrink, never grow. Drop a key from that list only in the change that deletes it from every locale.

## Adding Languages

1. Add all 10 namespace JSON files under `web/src/i18n/locales/<lang>/`.
2. Add code to `supportedLanguages` and display name to `languageNames` in `web/src/i18n/index.ts`.
3. Add/adapt a locale coverage script. Both Chinese locales share `scripts/check-chinese-coverage.mjs`, which takes the locale as its argument.
4. Run `pnpm check:i18n`.
5. Update the supported-language list in `README.md` (Features), `documentation/docs/intro.md` (Features + Languages section), and the i18n section above so the promoted list stays accurate.

Coverage must compare against English for missing/extra keys, interpolation placeholders, HTML tag parity, plural forms, empty strings, encoding, and JSON validity.

## Translation Rules

- **Never hardcode text or raw backend variables (e.g., `run.status`, `task.status`) directly into JSX.** If a status or string is displayed to the user, you MUST create a corresponding `i18n` key (e.g., `statusLabels`) in the relevant JSON namespace and render it via `t()`.
- Read English namespace JSON and relevant UI first; translate in product context.
- Preserve placeholders, HTML tags, keys, examples, paths, URLs, commands, and technical notation unless the checker allows an exception.
- Keep a glossary for product names and torrent/domain terms.
- Plurals use the i18next v4 CLDR suffixes:
  - English needs `_one` and `_other`.
  - Chinese and Korean take `_other` alone.
  - `cs` needs `_one`, `_few` and `_other`. `_many` is optional, because Czech uses it only for decimals and every count is floored before the lookup.
  - `uk` needs `_one`, `_few`, `_many` and `_other`.
- A locale that omits a category it needs shows the English string at those counts. i18next resolves a missing category against `fallbackLng`, never against another category in the same language. The gap therefore looks like a working translation.
- An unsuffixed base key beside the suffixed ones answers every category the locale omits.
- Never add the pre-v4 `_plural` suffix. i18next does not resolve it in any locale.
- `pnpm check:i18n` enforces these rules. `check-legacy-plural-keys.mjs` rejects `_plural` in every locale, including `en`. `src/i18n/plurals.test.ts` renders every plural base through i18next with the app's fallback settings, and fails when a locale falls back to English.
- Product/ecosystem terms often stay English where clearer: `qBittorrent`, `Prowlarr`, `DHT`, `PEX`.
- Chinese text should prefer full-width `，。：；！？`; half-width is fine inside URLs, IPs, paths, and technical notation.

## Torrent Details Note

`web/src/components/torrents/TorrentDetailsPanel.tsx` live row state is stream-backed via `useSyncStream`; polling is fallback while stream unavailable. Content/files and Peers tabs still poll on interval, but polling is tab-scoped and visibility-gated.

`useSyncStream` listeners receive raw frames. A delta for unchanged rows carries an empty `torrents` list and the previous `total`, so a handler clears its row only on `total === 0` and keeps the previous row on an empty list.

## getqui.com Demo

`pnpm build:demo` builds the unchanged app in Vite mode `demo` for the landing page at getqui.com/demo/. `web/src/demo/` replaces `window.fetch` and `window.EventSource` with an in-memory store; there is no backend.

- A new API call on the torrent list surface needs a route in `web/src/demo/api.ts`, or the demo answers `404 {"error": "not available in the demo"}` and the feature looks broken on the site.
- Demo-only UI branches use `isDemo` from `web/src/lib/demo.ts`. Vite folds it, so production bundles carry none of them.
- Pages outside `/instances` are redirected in the demo; do not add demo handling to them.
