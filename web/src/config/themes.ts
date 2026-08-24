/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FALLBACK_THEME } from "@/utils/themeLoader";

export interface Theme {
  id: string;
  name: string;
  isPremium?: boolean;
  description?: string;
  lightOnly?: boolean;
  variations?: string[];
  /** True for sideloaded custom themes loaded at runtime from disk. */
  isCustom?: boolean;
  /** Locked premium stub: preview colors only, never applicable (no license). */
  locked?: boolean;
  /** Raw CSS for custom themes, injected verbatim as a <style> element. */
  rawCss?: string;
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

// The last applied theme, cached with its full CSS vars by applyTheme
// (utils/theme.ts) so a refresh paints it before the registry arrives from
// the API. Exported for tests.
export function parseCachedTheme(raw: string | null): Theme | null {
  try {
    const theme = raw ? JSON.parse(raw) as Theme : null;
    if (theme?.id && theme.id !== FALLBACK_THEME.id && theme.cssVars?.light && theme.cssVars?.dark) {
      return theme;
    }
  } catch {
    // Ignore malformed cache
  }
  return null;
}

function hydrateCachedTheme(): Theme[] {
  try {
    const cached = parseCachedTheme(localStorage.getItem("theme-cache"));
    return cached ? [cached] : [];
  } catch {
    return [];
  }
}

// Starts as the bundled fallback plus the cached last-applied theme; the full
// built-in list is fetched from GET /api/themes and swapped in here (see
// BuiltinThemesLoader). The array is mutated in place so existing imports stay live.
export const themes: Theme[] = [FALLBACK_THEME, ...hydrateCachedTheme()];

export function registerBuiltinThemes(list: Theme[]): void {
  if (list.length === 0) return;
  themes.splice(0, themes.length, ...list);
}

// Sideloaded custom themes are fetched at runtime (premium-gated) and registered
// here so the synchronous theme lookups below can resolve their "custom:" ids.
let customThemes: Theme[] = [];

export function registerCustomThemes(list: Theme[]): void {
  customThemes = list;
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
