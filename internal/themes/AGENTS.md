# AGENTS.md

Rules for a built-in theme file in `internal/themes/assets/`. The community themes are in `assets/premium/`.

- The app paints more with some tokens than their names say:
  - `--accent` is the background of a selected torrent row. At 40%, it is the background of a hovered row. `--muted` at 40% is the background of each second row. See `getRowBackgroundClass` in `web/src/lib/torrent-table/row-display.ts` and `web/src/components/torrents/table/TorrentTableRow.tsx`.
  - `--chart-1` to `--chart-5` are also the text colors of the Ratio column, from the best ratio to the worst (`getRatioColor` in `web/src/lib/utils.ts`). Each one must be readable as text on each row background, and `--chart-5` is a warning color.
  - `--primary` is also the color of link text, and of text on a 10% tint of itself (`bg-primary/10 text-primary`).
- Before you commit a theme file, run `node scripts/theme-check.mjs ../internal/themes/assets/<file>.css` in `web/`. It lists each color that sRGB cannot show and each pair below its WCAG contrast minimum, with the files that paint the pair. A new free theme has no line under its summary line.
- `web/scripts/theme-check.test.mjs` fails when a built-in theme, free or community, has a color that sRGB cannot show. It runs in `pnpm check:i18n`, not in `pnpm test`.
- `web/src/utils/fontLoader.test.ts` gives the rule for the first font of each `--font-*` token. `documentation/docs/features/custom-themes.md` lists the tokens.
- Chrome on macOS has no font for `ui-sans-serif`, `ui-serif`, `ui-monospace`, `ui-rounded`, or `-apple-system`, and uses the next family in the list. `system-ui` and `BlinkMacSystemFont` give the system font. Put a family that Chrome has after each of those keywords, as `meridian.css` does. Measured on 2026-11-16 in Chrome 157 on macOS 27.0.1 with `CSS.getPlatformFontsForNode` of the DevTools protocol.
