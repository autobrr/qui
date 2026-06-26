/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { loadThemes } from "@/utils/themeLoader";

export interface Theme {
  id: string;
  name: string;
  isPremium?: boolean;
  description?: string;
  lightOnly?: boolean;
  variations?: string[];
  /** True for sideloaded custom themes loaded at runtime from disk. */
  isCustom?: boolean;
  /** Raw CSS for custom themes, injected verbatim as a <style> element. */
  rawCss?: string;
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

// Load all built-in themes (globbed at build time).
export const themes: Theme[] = loadThemes();

// Sideloaded custom themes are fetched at runtime (premium-gated) and registered
// here so the synchronous theme lookups below can resolve their "custom:" ids.
let customThemes: Theme[] = [];

export function registerCustomThemes(list: Theme[]): void {
  customThemes = list;
}

export function getCustomThemes(): Theme[] {
  return customThemes;
}

// Helper functions
export function getThemeById(id: string): Theme | undefined {
  return themes.find(theme => theme.id === id) ?? customThemes.find(theme => theme.id === id);
}

export function getDefaultTheme(): Theme {
  return themes.find(theme => theme.id === "minimal") || themes[0];
}

export function isThemePremium(themeId: string): boolean {
  const theme = getThemeById(themeId);
  return theme?.isPremium ?? false;
}

export { themes as default };
