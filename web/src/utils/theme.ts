/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { themes, getThemeById, getDefaultTheme, type Theme } from "@/config/themes";
import {
  cacheAppliedTheme,
  endDefaultThemeVisit,
  isDefaultThemeVisit,
  storedMode,
  storedThemeId,
  storeMode,
  storeThemeId
} from "@/lib/theme-storage";
import type { PreviewableThemeCss } from "@/lib/custom-theme-preview";
import { loadThemeFonts } from "./fontLoader";
import { parseThemeCSS } from "./themeParser";
import { getStoredVariation, setStoredVariation } from "@/hooks/usePersistedThemeVariation";

const THEME_DARK = "dark";
const THEME_LIGHT = "light";
const THEME_AUTO = "auto";
const THEME_TRANSITION_CLASS = "theme-transition";
const THEME_TRANSITION_DURATION = 150;
const THEME_STYLES_ID = "theme-transitions";
const CUSTOM_THEME_STYLE_ID = "custom-theme-style";
const ENABLE_THEME_TRANSITIONS = false;

const THEME_TRANSITION_CSS = `
  .theme-transition {
    transition: background-color 150ms ease-out;
  }
`;

export type ThemeMode = typeof THEME_DARK | typeof THEME_LIGHT | typeof THEME_AUTO;

interface ThemeChangeEvent extends CustomEvent {
  detail: {
    mode: ThemeMode;
    theme: Theme;
    isSystemChange: boolean;
    variant?: string | null;
  };
}

const getSystemPreference = (): MediaQueryList => {
  return window.matchMedia("(prefers-color-scheme: dark)");
};

const getSystemTheme = (): typeof THEME_DARK | typeof THEME_LIGHT => {
  return getSystemPreference().matches ? THEME_DARK : THEME_LIGHT;
};

export const isDarkMode = (mode: ThemeMode): boolean => {
  return mode === THEME_DARK || (mode === THEME_AUTO && getSystemPreference().matches);
};

const dispatchThemeChange = (mode: ThemeMode, theme: Theme, isSystemChange: boolean, variant?: string | null): void => {
  const event = new CustomEvent("themechange", {
    detail: { mode, theme, isSystemChange, variant },
  }) as ThemeChangeEvent;
  window.dispatchEvent(event);
};

const applyCustomThemeStyle = (rawCss: string): void => {
  let style = document.getElementById(CUSTOM_THEME_STYLE_ID) as HTMLStyleElement | null;
  if (!style) {
    style = document.createElement("style");
    style.id = CUSTOM_THEME_STYLE_ID;
    document.head.appendChild(style);
  }
  if (style.textContent !== rawCss) {
    style.textContent = rawCss;
  }
};

const removeCustomThemeStyle = (): void => {
  document.getElementById(CUSTOM_THEME_STYLE_ID)?.remove();
};

let appliedCustomCss: string | null = null;
let preview: { css: string; cssVars: Theme["cssVars"]; lightOnly: boolean } | null = null;

export const getAppliedCustomThemeCss = (): string | null => appliedCustomCss;

const clearInlineThemeVars = (root: HTMLElement): void => {
  Array.from(root.style)
    .filter(prop => prop.startsWith("--"))
    .forEach(prop => root.style.removeProperty(prop));
};

const paintCustomCss = (css: string): void => {
  clearInlineThemeVars(document.documentElement);
  applyCustomThemeStyle(css);
};

const paintPreview = (isDark: boolean): void => {
  if (!preview) return;
  const root = document.documentElement;
  void loadThemeFonts({ cssVars: preview.cssVars });
  root.classList.toggle(THEME_DARK, !preview.lightOnly && isDark);
  paintCustomCss(preview.css);
  root.style.removeProperty("background-color");
  document.body?.style.removeProperty("background-color");
};

const applyCriticalBackground = (root: HTMLElement, cssVars: Record<string, string>): void => {
  const backgroundColor = cssVars["--background"];
  if (!backgroundColor) {
    return;
  }
  root.style.backgroundColor = backgroundColor;
  if (document.body) {
    document.body.style.backgroundColor = backgroundColor;
  }
};

const applyTheme = async (theme: Theme, variation: string | null, isDark: boolean, withTransition = false): Promise<void> => {
  const root = document.documentElement;

  loadThemeFonts(theme);

  if (ENABLE_THEME_TRANSITIONS && withTransition) {
    root.classList.add(THEME_TRANSITION_CLASS);
  }

  const effectiveIsDark = theme.lightOnly ? false : isDark;

  if (effectiveIsDark) {
    root.classList.add(THEME_DARK);
  } else {
    root.classList.remove(THEME_DARK);
  }

  const cssVars = effectiveIsDark ? theme.cssVars.dark : theme.cssVars.light;

  if (theme.isCustom && theme.rawCss) {
    paintCustomCss(theme.rawCss);
    appliedCustomCss = theme.rawCss;
  } else {
    removeCustomThemeStyle();
    appliedCustomCss = null;

    clearInlineThemeVars(root);

    Object.entries(cssVars).forEach(([key, value]) => {
      root.style.setProperty(key, value);
    });

    if (variation && theme.variations && theme.variations.length > 0) {
      const variationColor = cssVars[`--variation-${variation}`];
      if (variationColor) {
        root.style.setProperty("--variation-color", variationColor);
      }
    }
  }

  root.setAttribute("data-theme", theme.id);
  applyCriticalBackground(root, cssVars);
  cacheAppliedTheme(theme, cssVars);
  paintPreview(isDark);

  if (ENABLE_THEME_TRANSITIONS && withTransition) {
    setTimeout(() => {
      root.classList.remove(THEME_TRANSITION_CLASS);
    }, THEME_TRANSITION_DURATION);
  }
};

const handleSystemThemeChange = async (event: MediaQueryListEvent): Promise<void> => {
  if (getCurrentThemeMode() === THEME_AUTO) {
    const theme = getCurrentTheme();
    const variation = getThemeVariation(theme.id);
    await applyTheme(theme, variation, event.matches, true);
    dispatchThemeChange(THEME_AUTO, theme, true, variation);
  }
};

const injectThemeStyles = (): void => {
  if (!document.getElementById(THEME_STYLES_ID)) {
    const style = document.createElement("style");
    style.id = THEME_STYLES_ID;
    style.textContent = THEME_TRANSITION_CSS;
    document.head.appendChild(style);
  }
};

const addMediaQueryListener = (
  mediaQuery: MediaQueryList,
  handler: (event: MediaQueryListEvent) => void
): void => {
  try {
    mediaQuery.addEventListener("change", handler);
  } catch {
    try {
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

export const getCurrentTheme = (): Theme => {
  const themeId = storedThemeId();
  if (themeId) {
    const theme = getThemeById(themeId);
    if (theme && !theme.locked) {
      return theme;
    }
  }
  return getDefaultTheme();
};

export const getCurrentThemeMode = (): ThemeMode => {
  return storedMode() ?? THEME_AUTO;
};

export const setTheme = async (themeId: string, mode?: ThemeMode, variation?: string, isSystemChange = false): Promise<void> => {
  if (isDefaultThemeVisit()) {
    if (isSystemChange) {
      const defaultTheme = getDefaultTheme();
      const currentMode = getCurrentThemeMode();
      const currentVariation = getThemeVariation(defaultTheme.id);
      await applyTheme(defaultTheme, currentVariation, isDarkMode(currentMode), false);
      dispatchThemeChange(currentMode, defaultTheme, true, currentVariation);
      return;
    }
    mode ??= storedMode() ?? undefined;
    endDefaultThemeVisit();
  }

  const theme = getThemeById(themeId);

  if (!theme || theme.locked) {
    const defaultTheme = getDefaultTheme();
    const currentMode = mode || getCurrentThemeMode();

    if (mode) {
      storeMode(mode);
    }
    if (variation) {
      setStoredVariation(themeId, variation);
    }
    const currentVariation = getThemeVariation(defaultTheme.id);
    if (currentVariation) {
      setStoredVariation(defaultTheme.id, currentVariation);
    }

    const isDark = isDarkMode(currentMode);

    await applyTheme(defaultTheme, currentVariation, isDark, false);
    dispatchThemeChange(currentMode, defaultTheme, true, currentVariation);
    return;
  }

  const currentMode = mode || getCurrentThemeMode();

  storeThemeId(theme.id);
  if (mode) {
    storeMode(mode);
  }

  const currentVariation = (variation && theme.variations?.includes(variation))? variation: getThemeVariation(theme.id);

  if (currentVariation) {
    setStoredVariation(theme.id, currentVariation);
  }

  const isDark = isDarkMode(currentMode);

  await applyTheme(theme, currentVariation, isDark, false);
  dispatchThemeChange(currentMode, theme, isSystemChange, currentVariation);
};

export const setThemeMode = async (mode: ThemeMode): Promise<void> => {
  const theme = getCurrentTheme();
  const variation = getThemeVariation(theme.id);
  storeMode(mode);

  const isDark = isDarkMode(mode);

  await applyTheme(theme, variation, isDark, false);
  dispatchThemeChange(mode, theme, false, variation);
};

export const paintCustomThemePreview = (css: PreviewableThemeCss): void => {
  const parsed = parseThemeCSS(css);
  if (!parsed) return;
  preview = { css, cssVars: parsed.cssVars, lightOnly: parsed.metadata.lightOnly === true };
  paintPreview(isDarkMode(getCurrentThemeMode()));
};

export const endCustomThemePreview = async (): Promise<void> => {
  if (!preview) return;
  preview = null;
  const theme = getCurrentTheme();
  await applyTheme(theme, getThemeVariation(theme.id), isDarkMode(getCurrentThemeMode()), false);
};

export const initializeTheme = async (): Promise<void> => {
  injectThemeStyles();

  const mode = storedMode();
  const theme = getCurrentTheme();
  const variation = getThemeVariation(theme.id);
  const systemPreference = getSystemPreference();

  let isDark: boolean;
  if (mode === THEME_DARK || mode === THEME_LIGHT) {
    isDark = mode === THEME_DARK;
  } else {
    isDark = systemPreference.matches;
    if (!mode) {
      storeMode(THEME_AUTO);
    }
  }

  await applyTheme(theme, variation, isDark, false);

  addMediaQueryListener(systemPreference, handleSystemThemeChange);
};

export const setThemeVariation = async (variation: string): Promise<void> => {
  const theme = getCurrentTheme();

  if (!theme.variations?.includes(variation)) {
    console.warn(`Variation "${variation}" not found for theme "${theme.id}"`);
    return;
  }

  setStoredVariation(theme.id, variation);

  const currentMode = getCurrentThemeMode();
  const isDark = isDarkMode(currentMode);

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

export function getThemeColors(
  theme: Theme,
  colorVar: "--primary" | "--secondary" | "--accent",
  mode?: "light" | "dark"
): string;

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
  colorVar?: "--primary" | "--secondary" | "--accent",
  mode?: "light" | "dark"
): string | {
  primary: string;
  secondary: string;
  accent: string;
  variations?: Array<{ id: string; color: string }>;
} {
  const isDark = mode ? mode === "dark" : document.documentElement.classList.contains("dark");
  const cssVars = isDark ? theme.cssVars.dark : theme.cssVars.light;

  const resolveColor = (varName: "--primary" | "--secondary" | "--accent"): string => {
    const colorValue = cssVars[varName];

    if (colorValue === "var(--variation-color)") {
      const variation = getThemeVariation(theme.id);
      if (!variation) return "";
      return cssVars[`--variation-${variation}`] || "";
    }
    return colorValue || "";
  };

  if (colorVar) {
    return resolveColor(colorVar);
  }

  return {
    primary: resolveColor("--primary"),
    secondary: resolveColor("--secondary"),
    accent: resolveColor("--accent"),
    variations: theme.variations?.map(varId => ({
      id: varId,
      color: cssVars[`--variation-${varId}`],
    })).filter(v => v.color !== undefined),
  };
}

export { getSystemTheme };
export { themes };
export { getStoredVariation };
