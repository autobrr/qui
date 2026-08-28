---
sidebar_position: 12
title: Custom Themes
description: Sideload your own theme CSS files from a directory on disk (premium).
---

# Custom themes

:::info Premium feature
Custom themes require an active premium license. This license also unlocks the built-in premium themes. See [License Management](../licenses.md) to get a license.
:::

If you place your `.css` files into the custom themes directory, they appear in the theme picker next to the built-in themes. You do not need to rebuild qui.

## The theme picker

The theme picker is under **Settings → Premium Themes**. It shows every theme in one grid: free themes, premium themes, and your custom themes. Each card displays a badge. Click a theme card to apply it.

If you do not have an active license, premium themes appear as locked cards that show only preview colors. You cannot apply a locked theme. To get a license, click **Unlock premium** in the **Custom Themes** box below the grid.

## Theme selection syncs across devices

qui stores your theme selection in its database, so it [syncs across devices](./dashboard.md#interface-preferences-sync) like other interface preferences. The login page also paints a selected built-in theme. A custom theme paints on the login page only in a browser that applied it before. The browser cannot load custom theme files until you log in.

If your premium license lapses, qui applies the default theme instead. If you use a built-in premium theme, qui preserves your stored selection and restores the theme when you renew the license.

## Where to put theme files

qui reads custom themes from a directory on disk. By default, this directory is a `themes` folder next to your configuration file:

- **Docker:** `/config/themes`
- **Linux:** `~/.config/qui/themes`
- **Windows:** `%APPDATA%\qui\themes`

qui creates the directory on startup. If you want to use a different location, set `customThemesDir` in `config.toml` or set the `QUI__CUSTOM_THEMES_DIR` environment variable (see the [configuration reference](../configuration/reference.md)).

Place each theme as a single, self-contained `.css` file directly in that directory. qui ignores subdirectories and symlinks, skips files larger than 1 MiB, and reads at most 100 theme files.

You can find ready-made themes in [qui-community-themes](https://github.com/autobrr/qui-community-themes). The repository also accepts submissions.

## Authoring a theme

A theme file contains an optional metadata comment header, a `:root` (light mode) block, and a `.dark` (dark mode) block of CSS variables:

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

If you want to build a complete theme, copy one of the free built-in themes in qui and adjust the values. The source files provide the authoritative starting point:

[github.com/autobrr/qui/tree/main/internal/themes/assets](https://github.com/autobrr/qui/tree/main/internal/themes/assets)
(`minimal.css` is the neutral default and a good base).

Copy the `:root` and `.dark` blocks into your own file. You do **not** need the `@theme inline { ... }` block from those files. qui maps the tokens to the UI internally.

### Available design tokens

Define any of these tokens in the `:root` (light) and `.dark` blocks. If you omit tokens, qui falls back to the default theme values, so partial themes work. Built-in themes use the [OKLCH](https://oklch.com/) color space, but any valid CSS color works.

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

- **Both blocks are required.** A file must contain a `:root` block **and** a `.dark` block, each with at least one variable, even for a `@lightOnly` theme. If qui cannot parse a file, it skips the file and lists the error in the **Custom Themes** box in the theme picker.
- **`@name`** sets the display name. If you omit `@name`, qui defaults to "Untitled Theme". `@description` and `@lightOnly` are optional.
- **Fonts.** If you set `--font-sans`, `--font-serif`, or `--font-mono` to a font that qui knows (for example Inter, Montserrat, or JetBrains Mono), qui loads it from Google Fonts. The browser must be able to reach fonts.googleapis.com. If you use any other font, include an `@import` or `@font-face` rule directly in your CSS.
- **Arbitrary CSS works.** qui injects the file as a stylesheet, so you can add custom selectors and rules beyond design tokens. Scope your selectors carefully because a broad selector affects the entire app. qui does not sanitize CSS. Only load theme files that you trust or wrote yourself.
- **Variations.** qui does not support variations (the multi-swatch built-in themes) in custom themes.

## Using a custom theme

1. Place your `.css` file into the themes directory.
2. Open **Settings → Premium Themes**. Your theme appears in the theme grid with a **Custom** badge. The **Custom Themes** box below the grid shows the directory path and the number of discovered themes. Click **Refresh** in that box to load new or edited files without a restart of qui.
3. Select the theme like any other theme.

If you remove a custom theme file or your premium license lapses, qui falls back to the default theme. A built-in premium selection returns when you renew. A custom theme selection does not.
