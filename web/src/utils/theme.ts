/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { themes, getThemeById, getDefaultTheme, type Theme } from "@/config/themes";
import { loadThemeFonts } from "./fontLoader";
import { getStoredVariation, setStoredVariation } from "@/hooks/usePersistedThemeVariation";

// Theme constants
const THEME_KEY = "theme";
const COLOR_THEME_KEY = "color-theme";
const THEME_DARK = "dark";
const THEME_LIGHT = "light";
const THEME_AUTO = "auto";
const THEME_TRANSITION_CLASS = "theme-transition";
const THEME_TRANSITION_DURATION = 150;
const THEME_STYLES_ID = "theme-transitions";
const ENABLE_THEME_TRANSITIONS = false;

// CSS for theme transitions - lightweight version for performance
// Only transitions the root background; child elements inherit instantly via CSS variables
const THEME_TRANSITION_CSS = `
  .theme-transition {
    transition: background-color 150ms ease-out;
  }
`;

// Type definitions
export type ThemeMode = typeof THEME_DARK | typeof THEME_LIGHT | typeof THEME_AUTO;

interface ThemeChangeEvent extends CustomEvent {
  detail: {
    mode: ThemeMode;
    theme: Theme;
    isSystemChange: boolean;
    variant?: string | null;
  };
}

// Utility functions
const getStoredMode = (): ThemeMode | null => {
  const mode = localStorage.getItem(THEME_KEY);
  if (mode === THEME_DARK || mode === THEME_LIGHT || mode === THEME_AUTO) {
    return mode;
  }
  return null;
};

const setStoredMode = (mode: ThemeMode): void => {
  localStorage.setItem(THEME_KEY, mode);
};

const getStoredThemeId = (): string | null => {
  return localStorage.getItem(COLOR_THEME_KEY);
};

const setStoredThemeId = (themeId: string): void => {
  localStorage.setItem(COLOR_THEME_KEY, themeId);
};

const getSystemPreference = (): MediaQueryList => {
  return window.matchMedia("(prefers-color-scheme: dark)");
};

const getSystemTheme = (): typeof THEME_DARK | typeof THEME_LIGHT => {
  return getSystemPreference().matches ? THEME_DARK : THEME_LIGHT;
};

const dispatchThemeChange = (mode: ThemeMode, theme: Theme, isSystemChange: boolean, variant?: string | null): void => {
  const event = new CustomEvent("themechange", {
    detail: { mode, theme, isSystemChange, variant },
  }) as ThemeChangeEvent;
  window.dispatchEvent(event);
};

// Core theme application logic
const applyTheme = async (theme: Theme, variation: string | null, isDark: boolean, withTransition = false): Promise<void> => {
  const root = document.documentElement;

  // Kick off font loading in the background (non-blocking)
  // Fonts will render when ready; no need to wait
  loadThemeFonts(theme);

  if (ENABLE_THEME_TRANSITIONS && withTransition) {
    root.classList.add(THEME_TRANSITION_CLASS);
  }

  // For lightOnly themes, force light mode regardless of user preference
  const effectiveIsDark = theme.lightOnly ? false : isDark;

  // Store lightOnly flag for anti-FOUC script
  if (theme.lightOnly) {
    localStorage.setItem("theme-light-only", "true");
  } else {
    localStorage.removeItem("theme-light-only");
  }

  // Apply dark mode class
  if (effectiveIsDark) {
    root.classList.add(THEME_DARK);
  } else {
    root.classList.remove(THEME_DARK);
  }

  // Clean up all variation variables to prevent stale values
  Array.from(root.style)
    .filter(prop => prop.startsWith('--variation'))
    .forEach(prop => root.style.removeProperty(prop));

  // Apply theme CSS variables (lightOnly themes always use light vars)
  const cssVars = effectiveIsDark ? theme.cssVars.dark : theme.cssVars.light;
  Object.entries(cssVars).forEach(([key, value]) => {
    root.style.setProperty(key, value);
  });

  // Apply variation if provided
  if (variation && theme.variations && theme.variations.length > 0) {
    const variationColor = cssVars[`--variation-${variation}`];
    if (variationColor) {
      root.style.setProperty("--variation-color", variationColor);
    }
  }

  // Add theme class
  root.setAttribute("data-theme", theme.id);

  // Update HTML and body background color to match theme
  // This prevents flash of hardcoded background color
  const backgroundColor = cssVars["--background"];
  if (backgroundColor) {
    // Apply to both html and body for consistency
    root.style.backgroundColor = backgroundColor;
    if (document.body) {
      document.body.style.backgroundColor = backgroundColor;
    }

    // Store critical vars in localStorage for immediate application on next load
    // This prevents FOUC by allowing the inline script to apply the exact theme color
    try {
      const criticalVars = {
        background: backgroundColor,
        foreground: cssVars["--foreground"] || "",
      };
      localStorage.setItem("theme-critical-vars", JSON.stringify(criticalVars));
    } catch {
      // Ignore localStorage errors
    }
  }

  if (ENABLE_THEME_TRANSITIONS && withTransition) {
    setTimeout(() => {
      root.classList.remove(THEME_TRANSITION_CLASS);
    }, THEME_TRANSITION_DURATION);
  }
};

// Event handlers
const handleSystemThemeChange = async (event: MediaQueryListEvent): Promise<void> => {
  const storedMode = getStoredMode();

  // Only apply system theme if set to auto or not set
  if (!storedMode || storedMode === THEME_AUTO) {
    const theme = getCurrentTheme();
    const variation = getThemeVariation(theme.id);
    await applyTheme(theme, variation, event.matches, true);
    dispatchThemeChange(THEME_AUTO, theme, true, variation);
  }
};

// CSS injection
const injectThemeStyles = (): void => {
  if (!document.getElementById(THEME_STYLES_ID)) {
    const style = document.createElement("style");
    style.id = THEME_STYLES_ID;
    style.textContent = THEME_TRANSITION_CSS;
    document.head.appendChild(style);
  }
};

// Media query listener setup with fallback
const addMediaQueryListener = (
  mediaQuery: MediaQueryList,
  handler: (event: MediaQueryListEvent) => void
): void => {
  try {
    // Modern approach
    mediaQuery.addEventListener("change", handler);
  } catch {
    try {
      // Legacy fallback for older browsers
      const legacyMediaQuery = mediaQuery as MediaQueryList & {
        addListener?: (listener: (event: MediaQueryListEvent) => void) => void;
      };
      if (legacyMediaQuery.addListener) {
        legacyMediaQuery.addListener(handler);
      }
    } catch {
      console.warn("Failed to register system theme listener");
    }
  }
};

// Public API
let validatedThemes: Set<string> | null = null;
let isInitializing = true;

export const setValidatedThemes = (themeIds: string[]): void => {
  validatedThemes = new Set(themeIds);
  isInitializing = false;
};

const isThemeAccessible = (themeId: string): boolean => {
  // During initialization (before license data loads), trust the stored theme
  // This prevents the theme from resetting on hard refresh
  if (isInitializing && !validatedThemes) {
    // Allow the stored theme temporarily during initialization
    // It will be validated once license data loads
    return true;
  }

  // If we haven't received validation data yet but initialization is complete,
  // only allow non-premium themes
  if (!validatedThemes) {
    const theme = getThemeById(themeId);
    return !theme?.isPremium;
  }

  // Check if theme is in validated list
  return validatedThemes.has(themeId);
};

export const getCurrentTheme = (): Theme => {
  const storedThemeId = getStoredThemeId();
  if (storedThemeId) {
    const theme = getThemeById(storedThemeId);
    // Validate theme access
    if (theme && isThemeAccessible(theme.id)) {
      return theme;
    }
    // If theme exists but not accessible, clear it from storage
    if (theme && !isThemeAccessible(theme.id)) {
      localStorage.removeItem(COLOR_THEME_KEY);
    }
  }
  return getDefaultTheme();
};

export const getCurrentThemeMode = (): ThemeMode => {
  return getStoredMode() || THEME_AUTO;
};

export const setTheme = async (themeId: string, mode?: ThemeMode, variation?: string): Promise<void> => {
  const theme = getThemeById(themeId);

  // Validate theme access before applying
  if (!theme || !isThemeAccessible(theme.id)) {
    // Fall back to default theme if not accessible
    const defaultTheme = getDefaultTheme();
    const currentMode = mode || getCurrentThemeMode();

    setStoredThemeId(defaultTheme.id);
    if (mode) {
      setStoredMode(mode);
    }
    // Get variation for default theme
    const currentVariation = getThemeVariation(defaultTheme.id);
    if (currentVariation) {
      setStoredVariation(defaultTheme.id, currentVariation);
    }

    const isDark = currentMode === THEME_DARK ||
      (currentMode === THEME_AUTO && getSystemPreference().matches);

    await applyTheme(defaultTheme, currentVariation, isDark, false);
    dispatchThemeChange(currentMode, defaultTheme, false, currentVariation);
    return;
  }

  const currentMode = mode || getCurrentThemeMode();

  setStoredThemeId(theme.id);
  if (mode) {
    setStoredMode(mode);
  }

  // Validate and store variation
  const currentVariation = (variation && theme.variations?.includes(variation))
    ? variation
    : getThemeVariation(theme.id);

  if (currentVariation) {
    setStoredVariation(theme.id, currentVariation);
  }

  const isDark = currentMode === THEME_DARK ||
    (currentMode === THEME_AUTO && getSystemPreference().matches);

  await applyTheme(theme, currentVariation, isDark, false);
  dispatchThemeChange(currentMode, theme, false, currentVariation);
};

export const setThemeMode = async (mode: ThemeMode): Promise<void> => {
  const theme = getCurrentTheme();
  const variation = getThemeVariation(theme.id);
  setStoredMode(mode);

  const isDark = mode === THEME_DARK ||
    (mode === THEME_AUTO && getSystemPreference().matches);

  await applyTheme(theme, variation, isDark, false);
  dispatchThemeChange(mode, theme, false, variation);
};

export const initializeTheme = async (): Promise<void> => {
  injectThemeStyles();

  const storedMode = getStoredMode();
  const theme = getCurrentTheme();
  const variation = getThemeVariation(theme.id);
  const systemPreference = getSystemPreference();

  // Determine initial theme
  let isDark: boolean;
  if (storedMode === THEME_DARK || storedMode === THEME_LIGHT) {
    // User has explicit preference
    isDark = storedMode === THEME_DARK;
  } else {
    // No preference or auto - follow system
    isDark = systemPreference.matches;
    if (!storedMode) {
      setStoredMode(THEME_AUTO);
    }
  }

  await applyTheme(theme, variation, isDark, false);

  // Always listen for system theme changes
  addMediaQueryListener(systemPreference, handleSystemThemeChange);
};

export const resetToSystemTheme = async (): Promise<void> => {
  setStoredMode(THEME_AUTO);
  const theme = getCurrentTheme();
  const variation = getThemeVariation(theme.id);
  await applyTheme(theme, variation, getSystemPreference().matches, false);
  dispatchThemeChange(THEME_AUTO, theme, false, variation);
};

export const setAutoTheme = async (): Promise<void> => {
  await resetToSystemTheme();
};

export const setThemeVariation = async (variation: string): Promise<void> => {
  const theme = getCurrentTheme();

  // Validate variation exists for this theme
  if (!theme.variations?.includes(variation)) {
    console.warn(`Variation "${variation}" not found for theme "${theme.id}"`);
    return;
  }

  setStoredVariation(theme.id, variation);

  const currentMode = getCurrentThemeMode();
  const isDark = currentMode === THEME_DARK ||
    (currentMode === THEME_AUTO && getSystemPreference().matches);

  await applyTheme(theme, variation, isDark, false);
  dispatchThemeChange(currentMode, theme, false, variation);
};

export const getThemeVariation = (themeId?: string): string | null => {
  const theme = themeId ? getThemeById(themeId) : getCurrentTheme();
  if (!theme || !theme.variations || theme.variations.length === 0) {
    return null;
  }

  const stored = getStoredVariation(theme.id);
  if (stored && theme.variations.includes(stored)) {
    return stored;
  }

  return theme.variations[0];
};

// When colorVar is provided, return string
export function getThemeColors(
  theme: Theme,
  colorVar: '--primary' | '--secondary' | '--accent',
  mode?: 'light' | 'dark'
): string;

// When colorVar is not provided, return object
export function getThemeColors(
  theme: Theme
): {
  primary: string;
  secondary: string;
  accent: string;
  variations?: Array<{ id: string; color: string }>;
};

export function getThemeColors(
  theme: Theme,
  colorVar?: '--primary' | '--secondary' | '--accent',
  mode?: 'light' | 'dark'
): string | {
  primary: string;
  secondary: string;
  accent: string;
  variations?: Array<{ id: string; color: string }>;
} {
  // Use passed mode if specified
  const isDark = mode ? mode === 'dark' : document.documentElement.classList.contains("dark");
  const cssVars = isDark ? theme.cssVars.dark : theme.cssVars.light;

  // Helper to resolve variation colors
  const resolveColor = (varName: '--primary' | '--secondary' | '--accent'): string => {
    const colorValue = cssVars[varName];

    if (colorValue === "var(--variation-color)") {
      // Get stored variation or fallback to first variation
      const variation = getThemeVariation(theme.id);
      if (!variation) return "";
      return cssVars[`--variation-${variation}`] || "";
    }
    return colorValue || "";
  };

  // Return single color if colorVar passed
  if (colorVar) {
    return resolveColor(colorVar);
  }

  // Otherwise return all
  return {
    primary: resolveColor('--primary'),
    secondary: resolveColor('--secondary'),
    accent: resolveColor('--accent'),
    variations: theme.variations?.map(varId => ({
      id: varId,
      color: cssVars[`--variation-${varId}`],
    })).filter(v => v.color !== undefined),
  };
}

// Re-export for backward compatibility
export { getSystemTheme };
export { themes };
export { getStoredVariation };
