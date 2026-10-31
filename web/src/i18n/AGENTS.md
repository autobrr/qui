# AGENTS.md

Locale and translation rules for work under `web/src/i18n/`.

## Layout

Each locale lives under `web/src/i18n/locales/<lang>/`, with one JSON file for each namespace in `locales/en/`. `supportedLanguages` in `web/src/i18n/index.ts` lists the locale codes.

English is fallback/eager-loaded. Other languages are lazy-loaded by `initI18n()` / `changeLanguage()` through `import.meta.glob` in `web/src/i18n/index.ts`.

Before you add a language, follow `docs/agents/adding-a-language.md`.

## Translation Rules

- Read English namespace JSON and relevant UI first; translate in product context.
- Preserve placeholders, HTML tags, keys, examples, paths, URLs, commands, and technical notation unless the checker allows an exception.
- Byte and speed units and their decimal separator are the exception: they are localized, so render them through `web/src/lib/unit-format.ts` and never hardcode `KiB`, `MiB/s` or `Mbps`.
- In a `<Trans components={{…}}>` string, give each tag a name that is not an HTML void element, for example `<docs>` in `instances.json`. react-i18next reads `link`, `img`, `br`, `input`, `meta`, and `hr` as self-closing, so the component does not wrap the inner text.
- Plurals use the i18next v4 CLDR suffixes. `pnpm check:i18n` names each plural form that a locale is missing. In `cs`, `_many` is optional, because Czech uses it only for decimals and no count is a decimal: item counts are whole numbers, and relative-time counts are floored in `web/src/lib/dateTimeUtils.ts`.
- An unsuffixed base key beside the suffixed ones answers every category the locale omits, with one string. Use it only for text that does not change with the count. The base key also hides a missing form from `pnpm check:i18n`, so `cs` or `uk` can show the wrong form, for example "2 trackerů". A base key must exist in English and in every locale, or the missing-keys and extra-keys checks reject it.
- Product/ecosystem terms often stay English where clearer: `qBittorrent`, `Prowlarr`, `DHT`, `PEX`.
- Chinese text should prefer full-width `，。：；！？`; half-width is fine inside URLs, IPs, paths, and technical notation.
