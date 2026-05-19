/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const VARIATIONS_THEME_KEY = "variations-theme";

export const getStoredVariation = (themeId: string): string | null => {
  try {
    const stored = localStorage.getItem(VARIATIONS_THEME_KEY);
    if (!stored) return null;
    const parsed = JSON.parse(stored);
    return parsed[themeId] || null;
  } catch {
    return null;
  }
};

export const setStoredVariation = (themeId: string, variationId: string): void => {
  try {
    const stored = localStorage.getItem(VARIATIONS_THEME_KEY);
    const parsed = stored ? JSON.parse(stored) : {};
    parsed[themeId] = variationId;
    localStorage.setItem(VARIATIONS_THEME_KEY, JSON.stringify(parsed));
  } catch {
    // Ignore
  }
};
