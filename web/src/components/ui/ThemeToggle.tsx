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
import { themes, isThemePremium, getThemeById } from "@/config/themes";
import { Sun, Moon, Monitor, Check, Lock, Palette } from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
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
import { useCustomThemes } from "@/hooks/useCustomThemes";
import { canSwitchToPremiumTheme } from "@/lib/license-entitlement";
import { buildThemeCatalog } from "@/lib/theme-catalog";

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
  const { t } = useTranslation("common");
  const { currentMode, currentTheme, isDark } = useThemeChange();
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess();
  const { customThemes } = useCustomThemes();
  const [open, setOpen] = useState(false);

  const canSwitchPremium = canSwitchToPremiumTheme({
    hasPremiumAccess,
    isError,
    isLoading,
  });

  const sortedThemes = useMemo(() => {
    return buildThemeCatalog(themes, customThemes);
  }, [customThemes]);

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

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode);

    const modeNames = { light: t("themeToggle.light"), dark: t("themeToggle.dark"), auto: t("themeToggle.system") };
    toast.success(t("themeToggle.switchedToMode", { mode: modeNames[mode] }));
  }, [t]);

  const handleThemeSelect = useCallback(async (themeId: string) => {
    const isPremium = isThemePremium(themeId);
    if (isPremium && !canSwitchPremium) {
      if (isError) {
        toast.error(t("themeToggle.unableToVerifyLicense"), {
          description: t("themeToggle.licenseCheckFailed"),
        });
      } else {
        toast.error(t("themeToggle.premiumThemeError"));
      }
      return;
    }

    await setTheme(themeId);

    const theme = getThemeById(themeId);
    toast.success(t("themeToggle.switchedToTheme", { theme: theme?.name || themeId }));
  }, [canSwitchPremium, isError, t]);

  const handleVariationSelect = useCallback(async (themeId: string, variationId: string) => {
    const isPremium = isThemePremium(themeId);
    if (isPremium && !canSwitchPremium) {
      if (isError) {
        toast.error(t("themeToggle.unableToVerifyLicense"), {
          description: t("themeToggle.licenseCheckFailed"),
        });
      } else {
        toast.error(t("themeToggle.premiumThemeError"));
      }
      return;
    }

    await setTheme(themeId);
    await setThemeVariation(variationId);

    const theme = getThemeById(themeId);
    toast.success(t("themeToggle.switchedToThemeVariation", { theme: theme?.name || themeId, variation: variationId }));

  }, [canSwitchPremium, isError, t]);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={cn("transition-transform duration-300")}
        >
          <Palette className={cn("h-5 w-5 transition-transform duration-200")} />
          <span className="sr-only">{t("themeToggle.changeTheme")}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="w-64 max-w-[calc(100vw-1rem)]"
      >
        <DropdownMenuLabel>{t("themeToggle.appearance")}</DropdownMenuLabel>
        <DropdownMenuSeparator />

        {/* Mode Selection */}
        <div className="px-2 py-1.5 text-sm font-medium">{t("themeToggle.mode")}</div>
        <DropdownMenuItem
          onClick={() => handleModeSelect("light")}
          className="flex items-center gap-2"
        >
          <Sun className="h-4 w-4" />
          <span className="flex-1">{t("themeToggle.light")}</span>
          {currentMode === "light" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => handleModeSelect("dark")}
          className="flex items-center gap-2"
        >
          <Moon className="h-4 w-4" />
          <span className="flex-1">{t("themeToggle.dark")}</span>
          {currentMode === "dark" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => handleModeSelect("auto")}
          className="flex items-center gap-2"
        >
          <Monitor className="h-4 w-4" />
          <span className="flex-1">{t("themeToggle.system")}</span>
          {currentMode === "auto" && <Check className="h-4 w-4" />}
        </DropdownMenuItem>

        <DropdownMenuSeparator />

        {/* Theme Selection */}
        <div className="px-2 py-1.5 text-sm font-medium">{t("themeToggle.theme")}</div>
        <div className="max-h-[max(8rem,calc(var(--radix-dropdown-menu-content-available-height)-16rem))] overflow-y-auto overscroll-contain pr-1">
          {sortedThemes.map((theme) => {
            const isPremium = isThemePremium(theme.id);
            const isLocked = isPremium && !canSwitchPremium;
            const colors = getPreviewColors(theme);
            const isCurrentTheme = currentTheme.id === theme.id;
            const currentVariation = isCurrentTheme ? getThemeVariation(theme.id) : null;

            return (
              <DropdownMenuItem
                key={theme.id}
                aria-current={isCurrentTheme ? "true" : undefined}
                onSelect={(event) => {
                  event.preventDefault();
                  void handleThemeSelect(theme.id);
                }}
                className={cn(
                  "block min-w-0",
                  isLocked && "opacity-60"
                )}
                disabled={isLocked}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <div
                    className="size-4 shrink-0 rounded-full ring-1 ring-black/10 transition-all duration-300 ease-out dark:ring-white/10"
                    style={{
                      backgroundColor: colors.primary,
                      backgroundImage: "none",
                      background: colors.primary + " !important",
                    }}
                  />
                  <span className="min-w-0 flex-1 whitespace-normal break-words">
                    {theme.name}
                  </span>

                  <span className="flex size-4 shrink-0 items-center justify-center">
                    {isLocked ? (
                      <Lock className="size-3.5" />
                    ) : isCurrentTheme ? (
                      <Check className="size-4" />
                    ) : null}
                  </span>
                </div>

                {colors.variations && colors.variations.length > 0 && (
                  <div
                    data-slot="theme-variations"
                    className="mt-1 flex items-center gap-1 pr-6 pl-6"
                  >
                    {colors.variations.map((variation) => {
                      const isSelected = currentVariation === variation.id;
                      return (
                        <button
                          key={variation.id}
                          type="button"
                          title={variation.id}
                          onPointerDown={(event) => event.stopPropagation()}
                          onClick={(event) => {
                            event.stopPropagation();
                            void handleVariationSelect(theme.id, variation.id);
                          }}
                          className={cn(
                            "size-4 shrink-0 cursor-pointer rounded-full transition-all",
                            isSelected
                              ? "ring-2 ring-black dark:ring-white"
                              : "ring-1 ring-black/10 dark:ring-white/10"
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
                )}
              </DropdownMenuItem>
            );
          })}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
