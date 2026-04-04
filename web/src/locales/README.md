# Locale Files

Source of truth for UI translations is JSON under `web/src/locales/<language>/`.

Namespaces:
- `common.json`
- `auth.json`
- `footer.json`

Supported languages:
- `en`
- `zh-CN`
- `ja`
- `pt-BR`
- `de`
- `es-419`
- `fr`
- `ko`

Guidelines:
- Translate full sentences with context, not word-by-word.
- Keep interpolation tokens unchanged (example: `{{count}}`, `{{name}}`).
- Keep i18next plural keys unchanged (example: `_one`, `_other`).
- Do not change key names unless code is updated in the same PR.

This layout is compatible with community translation platforms like Weblate, Crowdin, and Locize.
