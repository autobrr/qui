/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { themes, getThemeById, getDefaultTheme, type Theme } from "@/config/themes";
import { loadThemeFonts } from "./fontLoader";

// Theme constants
const THEME_KEY = "theme";
const COLOR_THEME_KEY = "color-theme";
const THEME_DARK = "dark";
const THEME_LIGHT = "light";
const THEME_AUTO = "auto";
const THEME_TRANSITION_CLASS = "theme-transition";
const THEME_TRANSITION_DURATION = 400;
const THEME_STYLES_ID = "theme-transitions";

// CSS for theme transitions
const THEME_TRANSITION_CSS = `
  /* CSS Variables for transition control */
  :root {
    --theme-transition-duration: 400ms;
    --theme-transition-easing: cubic-bezier(0.4, 0.0, 0.2, 1);
    --theme-transition-stagger: 50ms;
  }

  /* Main transition for theme switching */
  .theme-transition {
    position: relative;
  }
  
  /* Core element transitions */
  .theme-transition * {
    transition-property: background-color, border-color, color, fill, stroke, box-shadow;
    transition-duration: var(--theme-transition-duration);
    transition-timing-function: var(--theme-transition-easing);
  }
  
  /* Font transitions for smooth font family changes */
  .theme-transition body,
  .theme-transition .font-sans,
  .theme-transition .font-serif,
  .theme-transition .font-mono {
    transition-property: font-family, letter-spacing, line-height;
    transition-duration: calc(var(--theme-transition-duration) * 0.8);
    transition-timing-function: var(--theme-transition-easing);
  }
  
  /* Subtle fade effect without any transforms */
  .theme-transition {
    animation: theme-transition-fade var(--theme-transition-duration) var(--theme-transition-easing);
  }
  
  @keyframes theme-transition-fade {
    0% {
      opacity: 1;
    }
    50% {
      opacity: 0.96;
    }
    100% {
      opacity: 1;
    }
  }
  
  /* Staggered transitions for different UI sections */
  .theme-transition header,
  .theme-transition nav {
    transition-delay: calc(var(--theme-transition-stagger) * 0);
  }
  
  .theme-transition main {
    transition-delay: calc(var(--theme-transition-stagger) * 1);
  }
  
  .theme-transition aside {
    transition-delay: calc(var(--theme-transition-stagger) * 2);
  }
  
  .theme-transition footer {
    transition-delay: calc(var(--theme-transition-stagger) * 3);
  }
  
  /* Cards and panels get a subtle lift effect */
  .theme-transition [class*="card"],
  .theme-transition [class*="panel"] {
    transition-property: background-color, border-color, color, box-shadow, transform;
    transition-duration: var(--theme-transition-duration);
    transition-timing-function: var(--theme-transition-easing);
  }
  
  /* Buttons get special treatment */
  .theme-transition button,
  .theme-transition [role="button"] {
    transition-property: background-color, border-color, color, box-shadow, transform, filter;
    transition-duration: calc(var(--theme-transition-duration) * 0.8);
    transition-timing-function: var(--theme-transition-easing);
  }
  
  /* Prevent scrollbar transitions */
  .theme-transition ::-webkit-scrollbar,
  .theme-transition ::-webkit-scrollbar-track,
  .theme-transition ::-webkit-scrollbar-thumb,
  ::-webkit-scrollbar,
  ::-webkit-scrollbar-track,
  ::-webkit-scrollbar-thumb {
    transition: none !important;
  }
  
  /* Prevent scrollbar color from animating */
  html.theme-transition {
    scrollbar-color: initial !important;
  }
  
  /* Disable transitions for performance-sensitive elements */
  .theme-transition svg *,
  .theme-transition path,
  .theme-transition circle,
  .theme-transition rect,
  .theme-transition line,
  .theme-transition polyline,
  .theme-transition polygon {
    transition: none !important;
  }
  
`;

// Type definitions
export type ThemeMode = typeof THEME_DARK | typeof THEME_LIGHT | typeof THEME_AUTO;

interface ThemeChangeEvent extends CustomEvent {
  detail: {
    mode: ThemeMode;
    theme: Theme;
    isSystemChange: boolean;
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

const dispatchThemeChange = (mode: ThemeMode, theme: Theme, isSystemChange: boolean): void => {
  const event = new CustomEvent("themechange", {
    detail: { mode, theme, isSystemChange },
  }) as ThemeChangeEvent;
  window.dispatchEvent(event);
};

// Core theme application logic
const applyTheme = async (theme: Theme, isDark: boolean, withTransition = false): Promise<void> => {
  const root = document.documentElement;

  // Load fonts for this theme
  await loadThemeFonts(theme);

  if (withTransition) {
    root.classList.add(THEME_TRANSITION_CLASS);
  }

  // Apply dark mode class
  if (isDark) {
    root.classList.add(THEME_DARK);
  } else {
    root.classList.remove(THEME_DARK);
  }

  // Apply theme CSS variables
  const cssVars = isDark ? theme.cssVars.dark : theme.cssVars.light;
  Object.entries(cssVars).forEach(([key, value]) => {
    root.style.setProperty(key, value);
  });

  // Add theme class
  root.setAttribute("data-theme", theme.id);

  if (withTransition) {
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
    await applyTheme(theme, event.matches, true);
    dispatchThemeChange(THEME_AUTO, theme, true);
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

export const setTheme = async (themeId: string, mode?: ThemeMode): Promise<void> => {
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

    const isDark = currentMode === THEME_DARK || 
      (currentMode === THEME_AUTO && getSystemPreference().matches);

    await applyTheme(defaultTheme, isDark, true);
    dispatchThemeChange(currentMode, defaultTheme, false);
    return;
  }
  
  const currentMode = mode || getCurrentThemeMode();
  
  setStoredThemeId(theme.id);
  if (mode) {
    setStoredMode(mode);
  }

  const isDark = currentMode === THEME_DARK || 
    (currentMode === THEME_AUTO && getSystemPreference().matches);

  await applyTheme(theme, isDark, true);
  dispatchThemeChange(currentMode, theme, false);
};

export const setThemeMode = async (mode: ThemeMode): Promise<void> => {
  const theme = getCurrentTheme();
  setStoredMode(mode);

  const isDark = mode === THEME_DARK || 
    (mode === THEME_AUTO && getSystemPreference().matches);

  await applyTheme(theme, isDark, true);
  dispatchThemeChange(mode, theme, false);
};

export const initializeTheme = async (): Promise<void> => {
  injectThemeStyles();

  const storedMode = getStoredMode();
  const theme = getCurrentTheme();
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

  await applyTheme(theme, isDark, false);

  // Always listen for system theme changes
  addMediaQueryListener(systemPreference, handleSystemThemeChange);
};

export const resetToSystemTheme = async (): Promise<void> => {
  setStoredMode(THEME_AUTO);
  const theme = getCurrentTheme();
  await applyTheme(theme, getSystemPreference().matches, true);
  dispatchThemeChange(THEME_AUTO, theme, false);
};

export const setAutoTheme = async (): Promise<void> => {
  await resetToSystemTheme();
};

// Re-export for backward compatibility
export { getSystemTheme };
export { themes };
