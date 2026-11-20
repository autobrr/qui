/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Map of font names to their Google Fonts URLs. Exported for fontLoader.test.ts.
export const FONT_MAP: Record<string, string> = {
  "-apple-system": "",
  "Inter": "Inter:wght@300;400;500;600;700",
  "Montserrat": "Montserrat:wght@300;400;500;600;700",
  "Poppins": "Poppins:wght@300;400;500;600;700",
  "Outfit": "Outfit:wght@300;400;500;600;700",
  "Geist": "Geist:wght@300;400;500;600;700",

  "Georgia": "",
  "Source Serif 4": "Source+Serif+4:wght@300;400;500;600;700",
  "Lora": "Lora:wght@400;500;600;700",

  "JetBrains Mono": "JetBrains+Mono:wght@300;400;500;600;700",
  "Fira Code": "Fira+Code:wght@300;400;500;600;700",
  "Source Code Pro": "Source+Code+Pro:wght@300;400;500;600;700",
  "Roboto Mono": "Roboto+Mono:wght@300;400;500;600;700",
  "Courier New": "",
  "Tahoma": "",
  "Times New Roman": "",
  "Aptos": "",
  "Cambria": "",
  "Consolas": "",
};

const loadedFonts = new Set<string>();

export function extractFontName(fontFamily: string): string {
  const match = fontFamily.match(/^["']?([^,"']+)/);
  return match ? match[1].trim() : "";
}

async function loadFont(fontName: string): Promise<void> {
  const googleFontId = FONT_MAP[fontName];

  if (!googleFontId || loadedFonts.has(fontName)) {
    return;
  }

  const link = document.createElement("link");
  link.rel = "stylesheet";
  link.href = `https://fonts.googleapis.com/css2?family=${googleFontId}&display=swap`;
  link.dataset.fontLoader = fontName;

  document.head.appendChild(link);
  loadedFonts.add(fontName);
}

export async function loadThemeFonts(theme: {
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}): Promise<void> {
  const fontsToLoad = new Set<string>();

  ["light", "dark"].forEach((mode) => {
    const vars = theme.cssVars[mode as "light" | "dark"];

    ["--font-sans", "--font-serif", "--font-mono"].forEach((key) => {
      if (vars[key]) {
        const fontName = extractFontName(vars[key]);
        if (fontName) {
          fontsToLoad.add(fontName);
        }
      }
    });
  });

  await Promise.all(Array.from(fontsToLoad).map(loadFont));
}
