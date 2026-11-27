---
status: accepted
date: 2026-11-25
---

# A status color is one color for each mode, and text on its fill takes the page background

The 50 built-in themes get colors for success, warning, and info. Each theme sets one value for each status in light and one in dark, with no foreground color. Text on a solid status fill uses the page background, which the contrast check of the status color as text already covers. Each status keeps its hue in every theme, at the theme's chroma and lightness, except in a theme whose error color is gray, which gets grays. The status colors are readable at 4.5 on the page, a card, a popover, their own 10% tint, and the torrent row backgrounds.

## Considered options

- **A foreground color for each status, as `destructive` has.** Rejected: about 6 spots put text on a fill, and the page background covers them.
- **Separate colors for fill, text, and text on a fill.** Rejected: 450 values, and an author has to pick between two warning colors for text.
- **The standard colors in themes with one hue or none.** Rejected: they break the style of the theme, and the error color of a theme already shows how far its author bends.

## Consequences

- The bright yellow icon of a warning becomes a dark ochre in light mode.
- In White and Vantablack, a status dot without text differs only in lightness.
- In dark Bubblegum, Cyberpunk, and Nord, the selected row is too bright for any status color to reach 4.5. The maintainer accepted near-white values at 4.36 to 4.40 there, and `web/scripts/theme-check.test.mjs` holds these nine pairs.
- A change of mind later means new values in 50 themes.
