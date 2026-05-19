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
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

// Load all themes from the themes directory
export const themes: Theme[] = loadThemes();

// Helper functions
export function getThemeById(id: string): Theme | undefined {
  return themes.find(theme => theme.id === id);
}

export function getDefaultTheme(): Theme {
  return themes.find(theme => theme.id === "minimal") || themes[0];
}

export function isThemePremium(themeId: string): boolean {
  const theme = getThemeById(themeId);
  return theme?.isPremium ?? false;
}

export { themes as default };
