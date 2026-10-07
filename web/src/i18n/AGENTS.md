# AGENTS.md

Locale and translation rules for work under `web/src/i18n/`.

## Layout

11 locales live under `web/src/i18n/locales/<lang>/`, each with 10 namespaces:

`common`, `auth`, `settings`, `torrents`, `dashboard`, `crossseed`, `rss`, `search`, `instances`, `automations`

English is fallback/eager-loaded. Other languages are lazy-loaded by `initI18n()` / `changeLanguage()` through `import.meta.glob` in `web/src/i18n/index.ts`. Supported today: `en`, `zh-CN`, `zh-TW`, `fr`, `de`, `cs`, `it`, `ko`, `uk`, `pt-BR`, `ca`.

Before you add a language, follow `docs/agents/adding-a-language.md`.

## Translation Rules

- Read English namespace JSON and relevant UI first; translate in product context.
- Preserve placeholders, HTML tags, keys, examples, paths, URLs, commands, and technical notation unless the checker allows an exception.
- Byte and speed units and their decimal separator are the exception: they are localized, so render them through `web/src/lib/unit-format.ts` and never hardcode `KiB`, `MiB/s` or `Mbps`.
- Keep a glossary for product names and torrent/domain terms.
- Plurals use the i18next v4 CLDR suffixes:
  - English needs `_one` and `_other`.
  - Chinese and Korean take `_other` alone.
  - `cs` needs `_one`, `_few` and `_other`. `_many` is optional, because Czech uses it only for decimals and no count reaches one: item counts are whole numbers, and relative-time counts are floored in `web/src/lib/dateTimeUtils.ts`.
  - `uk` needs `_one`, `_few`, `_many` and `_other`.
- A locale that omits a category it needs shows the English string at those counts. i18next resolves a missing category against `fallbackLng`, never against another category in the same language. The gap therefore looks like a working translation.
- An unsuffixed base key beside the suffixed ones answers every category the locale omits, with one string. Use it only for text that does not change with the count. The base key also hides a missing form from `pnpm check:i18n`, so `cs` or `uk` can show the wrong form, for example "2 trackerů". A base key must exist in English and in every locale, or the missing-keys and extra-keys checks reject it.
- Never add the pre-v4 `_plural` suffix. i18next does not resolve it in any locale.
- `pnpm check:i18n` enforces these rules. `check-legacy-plural-keys.mjs` rejects `_plural` in every locale, including `en`. `web/src/i18n/plurals.test.ts` asks i18next whether each locale can resolve every plural base at nine counts, and fails on the ones it cannot, since the app serves English for those.
- Product/ecosystem terms often stay English where clearer: `qBittorrent`, `Prowlarr`, `DHT`, `PEX`.
- Chinese text should prefer full-width `，。：；！？`; half-width is fine inside URLs, IPs, paths, and technical notation.
