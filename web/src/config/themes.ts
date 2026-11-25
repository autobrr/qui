/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FALLBACK_THEME } from "@/utils/themeLoader";
import { cachedThemeJson } from "@/lib/theme-storage";

export interface Theme {
  id: string;
  name: string;
  isPremium?: boolean;
  description?: string;
  lightOnly?: boolean;
  variations?: string[];
  isCustom?: boolean;
  locked?: boolean;
  rawCss?: string;
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

export function parseCachedTheme(raw: string | null): Theme | null {
  try {
    const theme = raw ? JSON.parse(raw) as Theme : null;
    if (theme?.id && theme.id !== FALLBACK_THEME.id && theme.cssVars?.light && theme.cssVars?.dark) {
      return theme;
    }
    return null;
  } catch {
    return null;
  }
}

function hydrateCachedTheme(): Theme[] {
  const cached = parseCachedTheme(cachedThemeJson());
  return cached ? [cached] : [];
}

export const themes: Theme[] = [FALLBACK_THEME, ...hydrateCachedTheme()];

export function registerBuiltinThemes(list: Theme[]): void {
  if (list.length === 0) return;
  themes.splice(0, themes.length, ...list);
}

let customThemes: Theme[] = [];

export function registerCustomThemes(list: Theme[]): void {
  customThemes = list;
}

export function getThemeById(id: string): Theme | undefined {
  return themes.find(theme => theme.id === id) ?? customThemes.find(theme => theme.id === id);
}

export function getDefaultTheme(): Theme {
  return themes.find(theme => theme.id === "minimal") || themes[0];
}

export { themes as default };
