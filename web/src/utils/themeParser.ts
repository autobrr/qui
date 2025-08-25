/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface ThemeMetadata {
  name: string;
  description?: string;
  isPremium?: boolean;
}

export interface ParsedTheme {
  metadata: ThemeMetadata;
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

/**
 * Parse CSS theme file and extract variables and metadata
 */
export function parseThemeCSS(cssContent: string,): ParsedTheme | null {
  try {
    // Extract metadata from CSS comments
    const metadata = extractMetadata(cssContent,);
    
    // Extract CSS variables
    const lightVars = extractCSSVariables(cssContent, ":root",);
    const darkVars = extractCSSVariables(cssContent, ".dark",);
    
    if (!lightVars || !darkVars) {
      console.error("Failed to extract CSS variables from theme",);
      return null;
    }
    
    return {
      metadata,
      cssVars: {
        light: lightVars,
        dark: darkVars,
      },
    };
  } catch (error) {
    console.error("Error parsing theme CSS:", error,);
    return null;
  }
}

/**
 * Extract metadata from CSS comments
 * Expected format:
 * /* @name: Theme Name
 *  * @description: Theme description
 *  * @premium: true/false
 *  */
function extractMetadata(cssContent: string,): ThemeMetadata {
  const metadata: ThemeMetadata = {
    name: "Untitled Theme",
  };
  
  // Match metadata comment block
  const metadataMatch = cssContent.match(/\/\*\s*@name:\s*(.+?)\s*\n\s*\*\s*@description:\s*(.+?)\s*\n\s*\*\s*@premium:\s*(true|false)\s*\*\//,);
  
  if (metadataMatch) {
    metadata.name = metadataMatch[1].trim();
    metadata.description = metadataMatch[2].trim();
    metadata.isPremium = metadataMatch[3] === "true";
  }
  
  return metadata;
}

/**
 * Extract CSS variables from a selector block
 */
function extractCSSVariables(cssContent: string, selector: string,): Record<string, string> | null {
  const variables: Record<string, string> = {};
  
  // Escape special characters in selector for regex
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&",);
  
  // Match the selector block
  const blockRegex = new RegExp(`${escapedSelector}\\s*{([^}]+)}`, "ms",);
  const blockMatch = cssContent.match(blockRegex,);
  
  if (!blockMatch) {
    console.warn(`No ${selector} block found in theme CSS`,);
    return null;
  }
  
  const blockContent = blockMatch[1];
  
  // Extract all CSS variables
  const varRegex = /(--[a-zA-Z0-9-]+):\s*([^;]+);/g;
  let match;
  
  while ((match = varRegex.exec(blockContent,)) !== null) {
    const varName = match[1].trim();
    const varValue = match[2].trim();
    variables[varName] = varValue;
  }
  
  return Object.keys(variables,).length > 0 ? variables : null;
}

/**
 * Generate a theme ID from the theme name
 */
export function generateThemeId(name: string,): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-",)
    .replace(/^-|-$/g, "",);
}
