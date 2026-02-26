/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useCallback, useMemo } from "react";
import {
  getCurrentThemeMode,
  getCurrentTheme,
  setTheme,
  setThemeMode,
  setThemeVariation,
  getThemeVariation,
  type ThemeMode
} from "@/utils/theme";
import { themes, isThemePremium } from "@/config/themes";
import { Sun, Moon, Monitor, Check, Palette, CornerDownRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
  DropdownMenuLabel
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { useHasPremiumAccess } from "@/hooks/useLicense.ts";
import { canSwitchToPremiumTheme } from "@/lib/license-entitlement";
import { useTranslation } from "react-i18next";
import i18n, { languageOptions, normalizeLanguage, type AppLanguage } from "@/i18n";

// Constants
const THEME_CHANGE_EVENT = "themechange";

// Custom hook for theme change detection
const useThemeChange = () => {
  const [currentMode, setCurrentMode] = useState<ThemeMode>(getCurrentThemeMode());
  const [currentTheme, setCurrentTheme] = useState(getCurrentTheme());
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains("dark"));

  const checkTheme = useCallback(() => {
    setCurrentMode(getCurrentThemeMode());
    setCurrentTheme(getCurrentTheme());
    setIsDark(document.documentElement.classList.contains("dark"));
  }, []);

  useEffect(() => {
    const handleThemeChange = () => {
      checkTheme();
    };

    window.addEventListener(THEME_CHANGE_EVENT, handleThemeChange);
    return () => {
      window.removeEventListener(THEME_CHANGE_EVENT, handleThemeChange);
    };
  }, [checkTheme]);

  return { currentMode, currentTheme, isDark };
};

export const ThemeToggle: React.FC = () => {
  const { t } = useTranslation()
  const { currentMode, currentTheme, isDark } = useThemeChange();
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess();
  const [open, setOpen] = useState(false);
  const [activeThemeId, setActiveThemeId] = useState<string | null>(null);
  const activeLanguage = normalizeLanguage(i18n.resolvedLanguage || i18n.language)

  const canSwitchPremium = canSwitchToPremiumTheme({
    hasPremiumAccess,
    isError,
    isLoading,
  });

  const sortedThemes = useMemo(() => {
    return [...themes].sort((a, b) => {
      const aIsPremium = isThemePremium(a.id);
      const bIsPremium = isThemePremium(b.id);
      if (aIsPremium === bIsPremium) return 0;
      return aIsPremium ? 1 : -1;
    });
  }, []);

  const previewColorsCache = useMemo(() => new Map<string, {
    primary: string;
    secondary: string;
    accent: string;
    variations?: Array<{ id: string; color: string }>;
  }>(), []);

  const modeKey = isDark ? "dark" : "light";
  const getPreviewColors = useCallback((theme: (typeof themes)[number]) => {
    const cacheKey = `${modeKey}:${theme.id}`;
    const cached = previewColorsCache.get(cacheKey);
    if (cached) return cached;

    const cssVars = modeKey === "dark" ? theme.cssVars.dark : theme.cssVars.light;
    const firstVariation = theme.variations?.[0];
    const resolveColor = (varName: "--primary" | "--secondary" | "--accent") => {
      const value = cssVars[varName];
      if (value === "var(--variation-color)" && firstVariation) {
        return cssVars[`--variation-${firstVariation}`] || "";
      }
      return value || "";
    };

    const colors = {
      primary: resolveColor("--primary"),
      secondary: resolveColor("--secondary"),
      accent: resolveColor("--accent"),
      variations: theme.variations?.map((id) => ({
        id,
        color: cssVars[`--variation-${id}`],
      })).filter((v) => v.color !== undefined),
    };

    previewColorsCache.set(cacheKey, colors);
    return colors;
  }, [modeKey, previewColorsCache]);

  useEffect(() => {
    if (open) {
      setActiveThemeId(currentTheme.id);
    }
  }, [open, currentTheme.id]);

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode);

    const modeNames = {
      light: t("theme.light"),
      dark: t("theme.dark"),
      auto: t("theme.system"),
    }
    toast.success(t("theme.switchedMode", { mode: modeNames[mode] }))
  }, [t]);

  const handleThemeSelect = useCallback(async (themeId: string) => {
    const isPremium = isThemePremium(themeId);
    if (isPremium && !canSwitchPremium) {
      if (isError) {
        toast.error(t("theme.unableVerifyLicense"), {
          description: t("theme.verifyLicenseDescription"),
        });
      } else {
        toast.error(t("theme.premiumThemeLocked"));
      }
      return;
    }

    setOpen(false);
    await setTheme(themeId);

    const theme = themes.find(t => t.id === themeId);
    toast.success(t("theme.switchedTheme", { theme: theme?.name || themeId }));
  }, [canSwitchPremium, isError, t]);

  const handleVariationSelect = useCallback(async (themeId: string, variationId: string) => {
    const isPremium = isThemePremium(themeId);
    if (isPremium && !canSwitchPremium) {
      if (isError) {
        toast.error(t("theme.unableVerifyLicense"), {
          description: t("theme.verifyLicenseDescription"),
        });
      } else {
        toast.error(t("theme.premiumThemeLocked"));
      }
      return;
    }

    await setTheme(themeId);
    await setThemeVariation(variationId);

    const theme = themes.find(t => t.id === themeId);
    toast.success(t("theme.switchedThemeVariation", { theme: theme?.name || themeId, variation: variationId }));

    setOpen(false);
  }, [canSwitchPremium, isError, t]);

  const handleLanguageSelect = useCallback(async (language: AppLanguage) => {
    await i18n.changeLanguage(language)
  }, [])

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={cn("transition-transform duration-300")}
        >
          <Palette className={cn("h-5 w-5 transition-transform duration-200")} />
          <span className="sr-only">{t("theme.changeTheme")}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>{t("theme.appearance")}</DropdownMenuLabel>
        <DropdownMenuSeparator />

        {/* Mode Selection */}
        <div className="px-2 py-1.5 text-sm font-medium">{t("theme.mode")}</div>
        <DropdownMenuItem
          onClick={() => handleModeSelect("light")}
          className="flex items-center gap-2"
        >
          <Sun className="h-4 w-4" />
          <span className="flex-1">{t("theme.light")}</span>
          {currentMode === "light" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => handleModeSelect("dark")}
          className="flex items-center gap-2"
        >
          <Moon className="h-4 w-4" />
          <span className="flex-1">{t("theme.dark")}</span>
          {currentMode === "dark" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => handleModeSelect("auto")}
          className="flex items-center gap-2"
        >
          <Monitor className="h-4 w-4" />
          <span className="flex-1">{t("theme.system")}</span>
          {currentMode === "auto" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>

        <DropdownMenuSeparator />

        <div className="px-2 py-1.5 text-sm font-medium">{t("languageSwitcher.menuLabel")}</div>
        {languageOptions.map((languageOption) => (
          <DropdownMenuItem
            key={languageOption.code}
            onClick={() => handleLanguageSelect(languageOption.code)}
            className="flex items-center gap-2"
          >
            <span className="flex-1">{t(languageOption.labelKey)}</span>
            {activeLanguage === languageOption.code && <Check className="h-4 w-4" />}
          </DropdownMenuItem>
        ))}

        <DropdownMenuSeparator />

        {/* Theme Selection */}
        <div className="px-2 py-1.5 text-sm font-medium">{t("theme.theme")}</div>
        {sortedThemes.map((theme) => {
          const isPremium = isThemePremium(theme.id);
          const isLocked = isPremium && !canSwitchPremium;
          const colors = getPreviewColors(theme);
          const showVariations = activeThemeId === theme.id;
          const currentVariation = showVariations ? getThemeVariation(theme.id) : null;

          return (
            <DropdownMenuItem
              key={theme.id}
              onClick={() => handleThemeSelect(theme.id)}
              onMouseEnter={() => {
                if (theme.variations && theme.variations.length > 0) {
                  setActiveThemeId(theme.id);
                }
              }}
              onFocus={() => {
                if (theme.variations && theme.variations.length > 0) {
                  setActiveThemeId(theme.id);
                }
              }}
              className={cn(
                "flex items-center gap-2",
                isLocked && "opacity-60"
              )}
              disabled={isLocked}
            >
              <div className="flex-1">
                <div className="flex items-center gap-2 flex-1">
                  <div
                    className="h-4 w-4 rounded-full ring-1 ring-black/10 dark:ring-white/10 transition-all duration-300 ease-out"
                    style={{
                      backgroundColor: colors.primary,
                      backgroundImage: "none",
                      background: colors.primary + " !important",
                    }}
                  />
                  <div className="flex items-center justify-between gap-1.5 flex-1">
                    <span>{theme.name}</span>
                    {isPremium && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground font-medium">
                        {t("theme.premium")}
                      </span>
                    )}
                  </div>
                </div>

                {/* Variation pills */}
                {showVariations && colors.variations && colors.variations.length > 0 && (
                  <div className="flex items-center gap-1.5 pl-1.5">
                    <CornerDownRight className="h-3 w-3 text-muted-foreground" />
                    <div className="flex gap-1 mt-1.5">
                      {colors.variations.map((variation) => {
                        const isSelected = currentVariation === variation.id;
                        return (
                          <div
                            key={variation.id}
                            onClick={(e) => {
                              e.stopPropagation();
                              handleVariationSelect(theme.id, variation.id);
                            }}
                            className={cn(
                              "w-4 h-4 rounded-full transition-all cursor-pointer",
                              isSelected? "ring-2 ring-black dark:ring-white": "ring-1 ring-black/10 dark:ring-white/10"
                            )}
                            style={{
                              backgroundColor: variation.color,
                              backgroundImage: "none",
                              background: variation.color + " !important",
                            }}
                          />
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>
              {currentTheme.id === theme.id && <Check className="h-4 w-4 self-center" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
