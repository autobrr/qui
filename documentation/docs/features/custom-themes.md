---
sidebar_position: 12
title: Custom CSS themes for the qBittorrent web UI
sidebar_label: Custom Themes
description: Write, paste, preview, and save your own CSS theme in qui with a license, with a prompt for an AI assistant, fixed theme areas, a design token reference, and a recovery address.
---

# Custom themes

:::info License required
Custom themes require an active license. This license also unlocks the built-in licensed themes. See [License Management](../licenses.md) to get a license.
:::

A custom theme is a `.css` file in the custom themes directory on the server. It appears in the theme picker next to the built-in themes. You can write or paste a theme in the theme editor of qui, or you can put the file into the directory yourself. You do not need to rebuild qui.

## The theme picker

The theme picker is under **Settings → Themes**. It shows every theme in one grid: free themes, licensed themes, and your custom themes. A free theme has a **Free** badge, and a custom theme has a **Custom** badge. Click a theme card to apply it.

If you do not have an active license, the licensed themes appear as locked cards with a lock icon. A locked card shows only preview colors. You cannot apply a locked theme. To get a license, click **Get a license** in the **Custom themes** box below the grid.

## Create a theme in qui

You do not need access to the server to add a custom theme.

1. Open **Settings → Themes**.
2. In the **Custom themes** box, click **New theme**.
3. Paste or write the CSS of the theme in the **CSS** field. If the CSS has a `@name` line, qui fills the **Name** field from it.
4. Look at the page. qui shows the theme in this browser while you edit, before you save. Your other browsers and devices do not change.
5. Click **Save**. qui writes the theme into the custom themes directory, and the theme becomes your selected theme. Other open browsers load the new theme without a reload.

The preview needs a `:root` block and a `.dark` block, each with at least one design token. If a block is missing, the editor names the block, and the preview keeps the last CSS that qui could use.

To stop without a save, click **Cancel** or press **Esc**. qui then shows the theme that was active before. A page reload also ends the preview.

If a theme with the same file name exists, qui asks before it replaces that theme.

Each custom theme card has a **⋮** menu with **Edit** and **Remove**. **Edit** also works on a file that you put into the directory yourself. A save of an edited theme does not change your selected theme. **Remove** asks first and then deletes the file. A device that uses the removed theme shows the default theme. If qui cannot parse a file, the **Custom themes** box lists the file with the same menu, so you can repair it or delete it.

If qui cannot write to the custom themes directory, the save fails, and the message names the directory. In Docker, make sure that the volume for `/config` is not read-only.

## Ask an AI assistant to write a theme

Click **Copy prompt for your AI** in the **Custom themes** box. qui copies a text that tells an AI assistant how to write a qui theme. The text lists the design tokens, the [theme areas](#theme-areas), and the rules. The copy also works when you open qui over plain HTTP.

1. Paste the text into your AI assistant.
2. Describe the look that you want.
3. Paste the CSS that the assistant returns into the theme editor.

## Theme areas

A theme area is a part of qui with a fixed name. A theme styles an area with a selector on the `data-qui-area` attribute:

```css
[data-qui-area="filter-sidebar"] {
  background: var(--sidebar);
  border-right: 2px solid var(--primary);
}
```

| Name | Area |
| --- | --- |
| `app-header` | The bar at the top of each page, with the page title, the search field, and the menus |
| `app-sidebar` | The navigation sidebar at the left of the desktop layout |
| `mobile-nav` | The navigation bar at the bottom of the mobile layout |
| `filter-sidebar` | The torrent filter panel with status, category, tag, and tracker filters |
| `torrent-table` | The torrent list table of the desktop layout, with its header row |
| `torrent-row` | One row of the torrent table |
| `torrent-card` | One torrent card in the torrent list of the mobile layout |
| `details-panel` | The panel that shows the details of the selected torrent |
| `dashboard-card` | One card on the dashboard, for example the card of one instance |
| `login` | The login page |

qui keeps these names across releases. A later release can add a name. If a release renames or removes a name, the release notes say so. Class names and the structure inside an area can change in any release, so do not use them in a theme.

General parts, such as buttons, inputs, dialogs, menus, and tables, have a `data-slot` attribute, for example `[data-slot="button"]` or `[data-slot="card"]`.

Do not change the height of a torrent row. qui computes the position of each row from a fixed height, so a different height breaks the torrent list.

## Themes cannot load files from other websites

A theme that loads a file from another website lets that website see your IP address each time you open qui. qui refuses to save such a theme from the theme editor or through the API. The refusal names the line and the reason. The theme editor also leaves such CSS out of the preview, so the browser does not load the file while you edit.

qui ignores CSS comments. Then it refuses the theme if the theme has one of these:

- An `@import` rule.
- An `image-set()` function.
- A `url()` whose target does not start with `data:` or `#`.
- A backslash outside a quoted string. A CSS escape can spell `url` or `@import` in a form that a text search does not find.

To use an image or a font, embed it in the theme as a `data:` address, for example `url(data:image/png;base64,...)`. The theme file can have 1 MiB at most.

qui does not apply this rule to a file that you put into the directory yourself.

## Recover from a theme that hides qui

If a theme makes qui unusable, add `?theme=default` to the address, for example `http://localhost:7476/?theme=default`. If qui runs with a base URL, add the parameter after the base URL. qui then shows the default theme until the next page load without the parameter. This works on the login page and on a phone.

During this visit, qui does not change your stored theme selection. **Settings → Themes** shows a line about the visit. You can then edit or remove the bad theme. If you select a theme in a theme picker, qui stores and applies that theme as usual, and the visit ends.

## Save a theme through the API

A script or a coding agent can save, replace, and remove a custom theme through the API. The API key needs the write level of the **Settings** area, and qui needs an active license. The routes:

- `POST /api/themes/custom` with `{ "id": "ocean", "css": "..." }` creates `ocean.css`. The id has 1 to 64 lowercase letters, digits, `-`, and `_`, and it starts with a letter or a digit. If a file with that id exists in any letter case, the answer is 409.
- `PUT /api/themes/custom/{id}` with `{ "css": "..." }` replaces the theme.
- `DELETE /api/themes/custom/{id}` removes the theme.

The rule about files from other websites applies to create and to replace.

## Theme selection syncs across devices

qui stores your theme selection in its database, so it [syncs across devices](./dashboard.md#interface-preferences-sync) like other interface preferences. The login page also paints a selected built-in theme. A custom theme paints on the login page only in a browser that applied it before. The browser cannot load custom theme files until you log in.

If your license lapses, qui applies the default theme instead. If you use a built-in licensed theme, qui preserves your stored selection and restores the theme when you renew the license.

### A separate theme for the mobile layout

qui shows the mobile layout when the window is less than 768 px wide. qui uses the window width, not the device type. A narrow desktop window also shows the mobile layout.

To use a different theme on the mobile layout, turn on **Separate theme for mobile layout** below the theme grid. The mobile layout then starts with your current theme. After that, a theme change on the mobile layout changes only the mobile theme. A theme change on the desktop layout changes only the desktop theme. The theme, the light, dark, or auto mode, and the variation split together. A line under the theme grid tells you which theme the picker changes. qui stores this setting in its database, so it applies to every browser.

To change the mobile theme, use a phone or a narrow window. When you turn the setting off, qui deletes the mobile theme, and every layout shows the desktop theme.

### Keep the theme of one browser local

To keep a theme on one browser only, for example on a shared computer, turn on **Keep this browser's theme local**. This browser then does not take the theme from the server and does not send its theme changes to the server. It keeps one theme for both layouts. The setting applies only to this browser. If this setting is on, you cannot change **Separate theme for mobile layout** in this browser.

When you turn the setting off, the browser shows the theme from the server again. Your other devices do not change.

## Where to put theme files

qui reads custom themes from a directory on disk. By default, this directory is a `themes` folder next to your configuration file:

- **Docker:** `/config/themes`
- **Linux:** `~/.config/qui/themes`
- **Windows:** `%APPDATA%\qui\themes`

qui creates the directory on startup. If you want to use a different location, set `customThemesDir` in `config.toml` or set the `QUI__CUSTOM_THEMES_DIR` environment variable (see the [configuration reference](../configuration/reference.md)).

Place each theme as a single, self-contained `.css` file directly in that directory. qui ignores subdirectories and symlinks, skips files larger than 1 MiB, and reads at most 100 theme files. The theme editor cannot add a theme when the directory already has 100 theme files.

qui ships every theme from [qui-community-themes](https://github.com/autobrr/qui-community-themes) built in, so you do not need to copy those themes here. To add a theme for every qui user, send a pull request to that repository.

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

- **Both blocks are required.** A file must contain a `:root` block **and** a `.dark` block, each with at least one variable, even for a `@lightOnly` theme. If qui cannot parse a file, it skips the file and lists the error in the **Custom themes** box in the theme picker.
- **`@name`** sets the display name. If you omit `@name`, qui defaults to "Untitled Theme". `@description` and `@lightOnly` are optional.
- **Fonts.** If you set `--font-sans`, `--font-serif`, or `--font-mono` to a font that qui knows (for example Inter, Montserrat, or JetBrains Mono), qui loads it from Google Fonts. The browser must be able to reach fonts.googleapis.com. To use any other font, embed it with an `@font-face` rule and a `data:` address. A file that you put into the directory yourself can also use `@import`.
- **Arbitrary CSS works.** qui adds the file to the page as a stylesheet, so you can add rules beyond design tokens. Use the [theme areas](#theme-areas) as selectors, and keep each selector narrow, because a broad selector affects the entire app. qui refuses a theme from the theme editor or the API that [loads a file from another website](#themes-cannot-load-files-from-other-websites). qui does not apply that rule to a file that you put into the directory yourself, so only use theme files that you trust or wrote yourself.
- **Variations.** qui does not support variations (the multi-swatch built-in themes) in custom themes.

## Use a theme file from the directory

1. Place your `.css` file into the themes directory.
2. Open **Settings → Themes**. Your theme appears in the theme grid with a **Custom** badge. The **Custom themes** box below the grid shows the number of themes and the directory path. Click the refresh button next to the path to load new or edited files without a restart of qui.
3. Select the theme like any other theme.

If you remove a custom theme file or your license lapses, qui falls back to the default theme. A built-in licensed theme selection returns when you renew. A custom theme selection does not.
