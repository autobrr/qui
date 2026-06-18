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

English is fallback/eager-loaded. Other languages are lazy-loaded by `initI18n()` / `changeLanguage()` through `import.meta.glob` in `web/src/i18n/index.ts`. Supported today: `en`, `zh-CN`, `fr`, `de`.

## i18n Commands

- `pnpm check:i18n`
- `pnpm check:i18n:hardcoded`
- `pnpm check:i18n:zh-cn`

Run relevant checks when touching UI strings, locale JSON, `web/src/i18n/index.ts`, or formatter hooks.

## Adding Languages

1. Add all 10 namespace JSON files under `web/src/i18n/locales/<lang>/`.
2. Add code to `supportedLanguages` and display name to `languageNames` in `web/src/i18n/index.ts`.
3. Add/adapt a locale coverage script if the locale is not `zh-CN`.
4. Run `pnpm check:i18n`.
5. Update the supported-language list in `README.md` (Features) and `documentation/docs/intro.md` (Features + Languages section) so the promoted list stays accurate.

Coverage must compare against English for missing/extra keys, interpolation placeholders, HTML tag parity, plural forms, empty strings, encoding, and JSON validity.

## Translation Rules

- Read English namespace JSON and relevant UI first; translate in product context.
- Preserve placeholders, HTML tags, keys, examples, paths, URLs, commands, and technical notation unless the checker allows an exception.
- Keep a glossary for product names and torrent/domain terms.
- English plurals use `_one`/`_other`; Chinese needs `_other`. Legacy `_plural` keys are manually dispatched and must exist in all locales.
- Product/ecosystem terms often stay English where clearer: `qBittorrent`, `Prowlarr`, `DHT`, `PEX`.
- Chinese text should prefer full-width `，。：；！？`; half-width is fine inside URLs, IPs, paths, and technical notation.

## Torrent Details Note

`web/src/components/torrents/TorrentDetailsPanel.tsx` live row state is stream-backed via `useSyncStream`; polling is fallback while stream unavailable. Content/files and Peers tabs still poll on interval, but polling is tab-scoped and visibility-gated.
