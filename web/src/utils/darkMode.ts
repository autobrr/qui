/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Theme constants
const THEME_KEY = 'theme';
const THEME_DARK = 'dark';
const THEME_LIGHT = 'light';
const THEME_AUTO = 'auto';

// Type definitions
export type Theme = typeof THEME_DARK | typeof THEME_LIGHT;
export type ThemeMode = Theme | typeof THEME_AUTO;

// Utility functions
const getStoredTheme = (): ThemeMode | null => {
  const theme = localStorage.getItem(THEME_KEY);
  if (theme === THEME_DARK || theme === THEME_LIGHT || theme === THEME_AUTO) {
    return theme;
  }
  return null;
};

const getSystemPreference = (): MediaQueryList => {
  return window.matchMedia('(prefers-color-scheme: dark)');
};

const getSystemTheme = (): Theme => {
  return getSystemPreference().matches ? THEME_DARK : THEME_LIGHT;
};

// Public API
export const getCurrentThemeMode = (): ThemeMode => {
  return getStoredTheme() || THEME_AUTO;
};

// Re-export system theme getter with consistent naming
export { getSystemTheme };
