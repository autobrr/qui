---
sidebar_position: 9
title: Custom Themes
description: Sideload your own theme CSS files from a directory on disk (premium).
---

# Custom Themes

:::info Premium feature
Custom themes require an active premium license, the same one that unlocks the
built-in premium themes. See [License Management](../licenses.md) to get set up.
:::

Custom themes let you drop your own `.css` files into a directory and have them
appear in the theme picker alongside the built-in themes, without rebuilding qui.

## Where to put theme files

qui reads custom themes from a directory on disk. By default this is a `themes`
folder next to your config file:

- **Docker:** `/config/themes`
- **Linux:** `~/.config/qui/themes`
- **Windows:** `%APPDATA%\qui\themes`

The directory is created automatically on startup. To use a different location,
set `customThemesDir` in `config.toml` or the `QUI__CUSTOM_THEMES_DIR`
environment variable (see the [configuration reference](../configuration/reference.md)).

Each theme is a single, self-contained `.css` file placed directly in that
directory (subdirectories and symlinks are ignored).

Ready-made themes live in
[qui-community-themes](https://github.com/autobrr/qui-community-themes), which
also takes submissions.

## Authoring a theme

A theme file has an optional metadata comment header followed by a `:root`
(light mode) block and a `.dark` (dark mode) block of CSS variables:

```css
/* @name: Ocean
 * @description: A calm blue theme
 * @lightOnly: false
 */
:root {
  --background: oklch(0.98 0.01 250);
  --foreground: oklch(0.2 0.02 250);
  --primary: oklch(0.55 0.15 250);
  --secondary: oklch(0.9 0.03 250);
  --accent: oklch(0.7 0.12 200);
  /* ...the remaining design tokens... */
}

.dark {
  --background: oklch(0.18 0.02 250);
  --foreground: oklch(0.95 0.01 250);
  --primary: oklch(0.65 0.15 250);
  --secondary: oklch(0.3 0.03 250);
  --accent: oklch(0.6 0.12 200);
  /* ... */
}
```

### Start from a built-in theme

The easiest way to build a complete theme is to copy one of qui's free built-in
themes and adjust the values. Their source files are the authoritative, always
up-to-date starting point:

[github.com/autobrr/qui/tree/main/web/src/themes](https://github.com/autobrr/qui/tree/main/web/src/themes)
(`minimal.css` is the neutral default and a good base).

Copy the `:root` and `.dark` blocks into your own file - you do **not** need the
`@theme inline { ... }` block from those files; qui already maps the tokens to
the UI internally.

### Available design tokens

Define any of these in the `:root` (light) and `.dark` blocks. Tokens you omit
fall back to the default theme's value, so partial themes are fine. Built-in
themes use the [OKLCH](https://oklch.com/) color space, but any valid CSS color
works.

| Group | Tokens |
| --- | --- |
| Surfaces | `--background`, `--foreground`, `--card(-foreground)`, `--popover(-foreground)` |
| Semantic colors | `--primary(-foreground)`, `--secondary(-foreground)`, `--muted(-foreground)`, `--accent(-foreground)`, `--destructive(-foreground)` |
| Controls | `--border`, `--input`, `--ring` |
| Sidebar | `--sidebar`, `--sidebar-foreground`, `--sidebar-primary(-foreground)`, `--sidebar-accent(-foreground)`, `--sidebar-border`, `--sidebar-ring` |
| Charts | `--chart-1` … `--chart-5` |
| Ratio colors (qui-specific) | `--ratio-bad`, `--ratio-almost`, `--ratio-good`, `--ratio-best` |
| Typography | `--font-sans`, `--font-serif`, `--font-mono` |
| Shape & spacing | `--radius`, `--spacing`, `--tracking-normal` |
| Shadows | `--shadow-2xs` … `--shadow-2xl` |

### Requirements and notes

- **Both blocks are required.** A file must contain a `:root` block **and** a
  `.dark` block, each with at least one variable - even a `@lightOnly` theme.
  Files that can't be parsed are listed as errors in the theme picker and are
  skipped.
- **`@name`** is used as the display name (falls back to "Untitled Theme").
  `@description` and `@lightOnly` are optional.
- **Fonts.** Setting `--font-sans` / `--font-serif` / `--font-mono` to one of the
  fonts qui already bundles will load it automatically. For any other font,
  include an `@import` or `@font-face` rule directly in your CSS.
- **Arbitrary CSS works.** The file is injected as a stylesheet, so you can add
  your own selectors and rules beyond the design tokens. Scope changes
  carefully - a broad selector can affect the whole app. qui does not sanitize
  the CSS, so only load theme files you trust or wrote yourself.
- **Variations** (the multi-swatch built-in themes) are not supported in custom
  themes.

## Using a custom theme

1. Drop your `.css` file into the themes directory.
2. Open **Settings → Themes**. Your theme appears in the **Custom Themes**
   section with a preview. Use **Refresh** there to pick up new or edited files
   without restarting qui.
3. Select it like any other theme.

If a theme's file is removed, or your premium license lapses, qui falls back to
the default theme.
