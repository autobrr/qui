/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Map of font names to their Google Fonts URLs
const FONT_MAP: Record<string, string> = {
  // Sans fonts
  "Inter": "Inter:wght@300;400;500;600;700",
  "Montserrat": "Montserrat:wght@300;400;500;600;700",
  "Poppins": "Poppins:wght@300;400;500;600;700",

  // Serif fonts
  "Georgia": "", // System font, no need to load
  "Source Serif 4": "Source+Serif+4:wght@300;400;500;600;700",
  "Lora": "Lora:wght@400;500;600;700",

  // Mono fonts
  "JetBrains Mono": "JetBrains+Mono:wght@300;400;500;600;700",
  "Fira Code": "Fira+Code:wght@300;400;500;600;700",
  "Source Code Pro": "Source+Code+Pro:wght@300;400;500;600;700",
  "Courier New": "", // System font, no need to load
};

// Keep track of loaded fonts to avoid duplicates
const loadedFonts = new Set<string>();

// Extract font name from font family string
function extractFontName(fontFamily: string): string {
  // Remove fallback fonts and quotes
  const match = fontFamily.match(/^["']?([^,"']+)/);
  return match ? match[1].trim() : "";
}

// Load a single font
async function loadFont(fontName: string): Promise<void> {
  const googleFontId = FONT_MAP[fontName];

  // Skip if it's a system font or already loaded
  if (!googleFontId || loadedFonts.has(fontName)) {
    return;
  }

  // Create link element
  const link = document.createElement("link");
  link.rel = "stylesheet";
  link.href = `https://fonts.googleapis.com/css2?family=${googleFontId}&display=swap`;
  link.dataset.fontLoader = fontName;

  // Add to head
  document.head.appendChild(link);
  loadedFonts.add(fontName);
}

// Load fonts for a theme
export async function loadThemeFonts(theme: {
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}): Promise<void> {
  const fontsToLoad = new Set<string>();

  // Extract fonts from both light and dark modes
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

  // Load all fonts
  await Promise.all(Array.from(fontsToLoad).map(loadFont));
}

// Preload common fonts on app start
export async function preloadCommonFonts(): Promise<void> {
  // Load the most common fonts to improve initial load
  const commonFonts = ["Inter", "JetBrains Mono"];
  await Promise.all(commonFonts.map(loadFont));
}
