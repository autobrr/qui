/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface ThemeMetadata {
  name: string;
  description?: string;
  isPremium?: boolean;
  variations?: string;
  lightOnly?: boolean;
}

export interface ParsedTheme {
  metadata: ThemeMetadata;
  variations?: string[];
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

/**
 * Parse CSS theme file and extract variables and metadata
 */
export function parseThemeCSS(cssContent: string): ParsedTheme | null {
  try {
    // Extract metadata from CSS comments
    const metadata = extractMetadata(cssContent);

    // Extract CSS variables
    const lightVars = extractCSSVariables(cssContent, ":root");
    const darkVars = extractCSSVariables(cssContent, ".dark");

    if (!lightVars || !darkVars) {
      console.error("Failed to extract CSS variables from theme");
      return null;
    }

    // Parse variations if included
    let variations: string[] | undefined;
    if (metadata.variations) {
      variations = parseVariations(metadata.variations, lightVars, darkVars, metadata.name);
    }

    return {
      metadata,
      variations,
      cssVars: {
        light: lightVars,
        dark: darkVars,
      },
    };
  } catch (error) {
    console.error("Error parsing theme CSS:", error);
    return null;
  }
}

/**
 * Extract metadata from CSS comments
 * Expected format:
 * /* @name: Theme Name
 *  * @description: Theme description
 *  * @premium: true/false
 *  * @lightOnly: true/false (optional)
 *  * @variations: orange, blue, green (optional)
 *  */
function extractMetadata(cssContent: string): ThemeMetadata {
  const metadata: ThemeMetadata = {
    name: "Untitled Theme",
  };

  // Extract individual metadata fields for more flexible parsing
  const nameMatch = cssContent.match(/@name:\s*(.+?)(?:\s*\n|\s*\*)/);
  const descMatch = cssContent.match(/@description:\s*(.+?)(?:\s*\n|\s*\*)/);
  const premiumMatch = cssContent.match(/@premium:\s*(true|false)/);
  const lightOnlyMatch = cssContent.match(/@lightOnly:\s*(true|false)/);
  const variationsMatch = cssContent.match(/@variations:\s*(.+?)(?:\s*\n|\s*\*\/)/);

  if (nameMatch) {
    metadata.name = nameMatch[1].trim();
  }
  if (descMatch) {
    metadata.description = descMatch[1].trim();
  }
  if (premiumMatch) {
    metadata.isPremium = premiumMatch[1] === "true";
  }
  if (lightOnlyMatch) {
    metadata.lightOnly = lightOnlyMatch[1] === "true";
  }
  if (variationsMatch) {
    metadata.variations = variationsMatch[1].trim();
  }

  return metadata;
}

function parseVariations(
  variationsStr: string,
  lightVars: Record<string, string>,
  darkVars: Record<string, string>,
  themeName: string
): string[] | undefined {
  const variations = variationsStr.split(",").map(v => v.trim());

  // Check variation variables exist
  for (const variation of variations) {
    const varName = `--variation-${variation}`;

    if (!lightVars[varName]) {
      console.warn(`Theme "${themeName}": Missing light mode CSS variable ${varName}`);
    }

    if (!darkVars[varName]) {
      console.warn(`Theme "${themeName}": Missing dark mode CSS variable ${varName}`);
    }
  }

  return variations.length > 0 ? variations : undefined;
}

/**
 * Extract CSS variables from a selector block
 */
function extractCSSVariables(cssContent: string, selector: string): Record<string, string> | null {
  const variables: Record<string, string> = {};

  // Escape special characters in selector for regex
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  // Match the selector block
  const blockRegex = new RegExp(`${escapedSelector}\\s*{([^}]+)}`, "ms");
  const blockMatch = cssContent.match(blockRegex);

  if (!blockMatch) {
    console.warn(`No ${selector} block found in theme CSS`);
    return null;
  }

  const blockContent = blockMatch[1];

  // Extract all CSS variables
  const varRegex = /(--[a-zA-Z0-9-]+):\s*([^;]+);/g;
  let match;

  while ((match = varRegex.exec(blockContent)) !== null) {
    const varName = match[1].trim();
    const varValue = match[2].trim();
    variables[varName] = varValue;
  }

  return Object.keys(variables).length > 0 ? variables : null;
}

/**
 * Generate a theme ID from the theme name
 */
export function generateThemeId(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}
