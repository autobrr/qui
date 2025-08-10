/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

export interface Theme {
  id: string;
  name: string;
  isPremium?: boolean;
  description?: string;
  cssVars: {
    light: Record<string, string>;
    dark: Record<string, string>;
  };
}

export const themes: Theme[] = [
];

export function getThemeById(id: string): Theme | undefined {
  return themes.find(theme => theme.id === id);
}

export function getDefaultTheme(): Theme {
  return themes[0];
}

// Helper functions for premium themes
export function getPremiumThemes(): Theme[] {
  return themes.filter(theme => theme.isPremium);
}

export function getFreeThemes(): Theme[] {
  return themes.filter(theme => !theme.isPremium);
}

export function isThemePremium(themeId: string): boolean {
  const theme = getThemeById(themeId);
  return theme?.isPremium ?? false;
}