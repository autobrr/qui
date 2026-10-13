# Adding a language

1. Add all 10 namespace JSON files under `web/src/i18n/locales/<lang>/`.
2. Add code to `supportedLanguages` and display name to `languageNames` in `web/src/i18n/index.ts`.
3. Add the locale to `localeRules` in `web/scripts/check-locale-coverage.mjs`, writing its plural categories by hand rather than from CLDR.
4. Run `cd web && pnpm check:i18n`.
5. Update the supported-language list in `README.md` (Features), `documentation/docs/intro.md` (Features + Languages section), and the Layout section of `web/src/i18n/AGENTS.md` so the promoted list stays accurate.
