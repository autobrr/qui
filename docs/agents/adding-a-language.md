# Adding a language

1. Add every namespace file in `locales/en/` under `web/src/i18n/locales/<lang>/`.
2. Add code to `supportedLanguages` and display name to `languageNames` in `web/src/i18n/index.ts`.
3. Add the locale to `localeRules` in `web/scripts/check-locale-coverage.mjs`, writing its plural categories by hand rather than from CLDR.
4. Add the locale code to `allLocales` in `web/scripts/check-locale-coverage.test.mjs`, and add a `check:i18n:<lang>` script to `web/package.json`.
5. Run `cd web && pnpm check:i18n`.
6. Update the supported-language list in `README.md` (Features) and `documentation/docs/intro.md` (Features + Languages section) so the promoted list stays accurate.
