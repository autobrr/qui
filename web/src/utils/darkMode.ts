/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

// Theme constants
const THEME_KEY = 'theme';
const THEME_DARK = 'dark';
const THEME_LIGHT = 'light';
const THEME_AUTO = 'auto';
const THEME_TRANSITION_CLASS = 'theme-transition';
const THEME_TRANSITION_DURATION = 300;
const THEME_STYLES_ID = 'theme-transitions';

// CSS for theme transitions
const THEME_TRANSITION_CSS = `
  /* Main transition for theme switching */
  .theme-transition :not(::-webkit-scrollbar):not(::-webkit-scrollbar-track):not(::-webkit-scrollbar-thumb) {
    transition-property: background-color, border-color, color, fill, box-shadow;
    transition-duration: 0.3s;
    transition-timing-function: ease-in-out;
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
`;

// Type definitions
export type Theme = typeof THEME_DARK | typeof THEME_LIGHT;
export type ThemeMode = Theme | typeof THEME_AUTO;

interface ThemeChangeEvent extends CustomEvent {
  detail: {
    theme: Theme;
    isSystemChange: boolean;
  };
}

// Utility functions
const getStoredTheme = (): ThemeMode | null => {
  const theme = localStorage.getItem(THEME_KEY);
  if (theme === THEME_DARK || theme === THEME_LIGHT || theme === THEME_AUTO) {
    return theme;
  }
  return null;
};

const setStoredTheme = (theme: ThemeMode): void => {
  localStorage.setItem(THEME_KEY, theme);
};

const getSystemPreference = (): MediaQueryList => {
  return window.matchMedia('(prefers-color-scheme: dark)');
};

const getSystemTheme = (): Theme => {
  return getSystemPreference().matches ? THEME_DARK : THEME_LIGHT;
};

const dispatchThemeChange = (theme: Theme, isSystemChange: boolean): void => {
  const event = new CustomEvent('themechange', {
    detail: { theme, isSystemChange },
  }) as ThemeChangeEvent;
  window.dispatchEvent(event);
};

// Core theme application logic
const applyTheme = (isDark: boolean, withTransition = false): void => {
  const root = document.documentElement;

  if (withTransition) {
    root.classList.add(THEME_TRANSITION_CLASS);
  }

  if (isDark) {
    root.classList.add(THEME_DARK);
  } else {
    root.classList.remove(THEME_DARK);
  }

  if (withTransition) {
    setTimeout(() => {
      root.classList.remove(THEME_TRANSITION_CLASS);
    }, THEME_TRANSITION_DURATION);
  }
};

// Event handlers
const handleSystemThemeChange = (event: MediaQueryListEvent): void => {
  const storedTheme = getStoredTheme();

  // Only apply system theme if set to auto or not set
  if (!storedTheme || storedTheme === THEME_AUTO) {
    const theme = event.matches ? THEME_DARK : THEME_LIGHT;
    applyTheme(event.matches, true);
    dispatchThemeChange(theme, true);
  }
};

// CSS injection
const injectThemeStyles = (): void => {
  if (!document.getElementById(THEME_STYLES_ID)) {
    const style = document.createElement('style');
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
    mediaQuery.addEventListener('change', handler);
  } catch {
    try {
      // Legacy fallback for older browsers
      // Type-safe approach for deprecated addListener method
      const legacyMediaQuery = mediaQuery as MediaQueryList & {
        addListener?: (listener: (event: MediaQueryListEvent) => void) => void;
      };
      if (legacyMediaQuery.addListener) {
        legacyMediaQuery.addListener(handler);
      }
    } catch {
      console.warn('Failed to register system theme listener');
    }
  }
};

// Public API
export const toggleDarkMode = (): void => {
  const root = document.documentElement;
  root.classList.add(THEME_TRANSITION_CLASS);

  const isDark = root.classList.contains(THEME_DARK);
  const newTheme: Theme = isDark ? THEME_LIGHT : THEME_DARK;

  applyTheme(!isDark, false);
  setStoredTheme(newTheme);
  dispatchThemeChange(newTheme, false);

  setTimeout(() => {
    root.classList.remove(THEME_TRANSITION_CLASS);
  }, THEME_TRANSITION_DURATION);
};

export const initializeDarkMode = (): void => {
  injectThemeStyles();

  const storedTheme = getStoredTheme();
  const systemPreference = getSystemPreference();

  // Determine initial theme
  let isDark: boolean;
  if (storedTheme === THEME_DARK || storedTheme === THEME_LIGHT) {
    // User has explicit preference
    isDark = storedTheme === THEME_DARK;
  } else {
    // No preference or auto - follow system
    isDark = systemPreference.matches;
    if (!storedTheme) {
      setStoredTheme(THEME_AUTO);
    }
  }

  applyTheme(isDark, false);

  // Always listen for system theme changes
  addMediaQueryListener(systemPreference, handleSystemThemeChange);
};

export const resetToSystemTheme = (): void => {
  setStoredTheme(THEME_AUTO);
  applyTheme(getSystemPreference().matches, true);
  dispatchThemeChange(getSystemTheme(), false);
};

export const setAutoTheme = (): void => {
  resetToSystemTheme();
};

export const hasManualPreference = (): boolean => {
  const theme = getStoredTheme();
  return theme === THEME_DARK || theme === THEME_LIGHT;
};

export const getCurrentThemeMode = (): ThemeMode => {
  return getStoredTheme() || THEME_AUTO;
};

// Re-export system theme getter with consistent naming
export { getSystemTheme };
