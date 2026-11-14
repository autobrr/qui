/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useCallback } from "react";
import {
  getCurrentThemeMode,
  getCurrentTheme,
  isDarkMode,
  setTheme,
  setThemeMode,
  setThemeVariation,
  getThemeVariation,
  type ThemeMode
} from "@/utils/theme";
import { themes, getThemeById, type Theme } from "@/config/themes";
import { Sun, Moon, Monitor, Palette, ListChecks } from "lucide-react";
import { Link } from "@tanstack/react-router";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
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
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes";
import { useCustomThemes } from "@/hooks/useCustomThemes";
import { buildThemeCatalog } from "@/lib/theme-catalog";
import { useThemeVisibility } from "@/hooks/useThemeVisibility";
import { themeThumbnailPalette, themeVariationColors } from "@/lib/theme-thumbnail";
import { QuickThemeRow, QuickThemeVariations } from "@/components/themes/QuickThemeRow";

// Constants
const THEME_CHANGE_EVENT = "themechange";

// Custom hook for theme change detection
const useThemeChange = (customThemes: readonly Theme[]) => {
  const [currentMode, setCurrentMode] = useState<ThemeMode>(getCurrentThemeMode());
  const [currentTheme, setCurrentTheme] = useState(getCurrentTheme());

  const checkTheme = useCallback(() => {
    setCurrentMode(getCurrentThemeMode());
    setCurrentTheme(getCurrentTheme());
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

  // Custom theme registration sends no themechange. Call this hook after
  // useCustomThemes, so that a stored custom theme resolves here.
  useEffect(checkTheme, [checkTheme, customThemes]);

  return { currentMode, currentTheme };
};

const MODES = [
  { mode: "light", icon: Sun, labelKey: "themeToggle.light" },
  { mode: "dark", icon: Moon, labelKey: "themeToggle.dark" },
  { mode: "auto", icon: Monitor, labelKey: "themeToggle.system" },
] as const;

export const ThemeToggle: React.FC = () => {
  const { t } = useTranslation("common");
  const { customThemes } = useCustomThemes();
  const { currentMode, currentTheme } = useThemeChange(customThemes);
  const [open, setOpen] = useState(false);
  const [visibility] = useThemeVisibility();

  // Subscribe so the list re-renders when the async theme registry lands;
  // the registry array mutates in place, so it must not be a memo dep.
  useBuiltinThemes();
  const sortedThemes = buildThemeCatalog(themes, customThemes, { visibility, currentThemeId: currentTheme.id });

  // The thumbnails follow the mode setting, not the light-only theme that can be in use.
  const dark = isDarkMode(currentMode);

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode);

    const modeNames = { light: t("themeToggle.light"), dark: t("themeToggle.dark"), auto: t("themeToggle.system") };
    toast.success(t("themeToggle.switchedToMode", { mode: modeNames[mode] }));
  }, [t]);

  const handleThemeSelect = useCallback(async (themeId: string) => {
    // The server is the authority: a premium theme without a license arrives
    // as a locked stub with no CSS, so the locked flag is the gate.
    if (getThemeById(themeId)?.locked) {
      toast.error(t("themeToggle.premiumThemeError"));
      return;
    }

    await setTheme(themeId);

    const theme = getThemeById(themeId);
    toast.success(t("themeToggle.switchedToTheme", { theme: theme?.name || themeId }));
  }, [t]);

  const handleVariationSelect = useCallback(async (themeId: string, variationId: string) => {
    if (getThemeById(themeId)?.locked) {
      toast.error(t("themeToggle.premiumThemeError"));
      return;
    }

    await setTheme(themeId);
    await setThemeVariation(variationId);

    const theme = getThemeById(themeId);
    toast.success(t("themeToggle.switchedToThemeVariation", { theme: theme?.name || themeId, variation: variationId }));

  }, [t]);

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
        className="w-72 max-w-[calc(100vw-1rem)]"
      >
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t("themeToggle.mode")}</DropdownMenuLabel>
        <DropdownMenuPrimitive.RadioGroup
          value={currentMode}
          onValueChange={(mode) => void handleModeSelect(mode as ThemeMode)}
          className="grid grid-cols-3 gap-1 px-1 pb-1"
        >
          {MODES.map(({ mode, icon: Icon, labelKey }) => (
            <DropdownMenuPrimitive.RadioItem
              key={mode}
              value={mode}
              onSelect={(event) => event.preventDefault()}
              className="flex cursor-default flex-col items-center gap-1 rounded-md border border-transparent px-1 py-2 text-xs text-muted-foreground outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[state=checked]:border-primary/50 data-[state=checked]:bg-primary/10 data-[state=checked]:font-medium data-[state=checked]:text-foreground"
            >
              <Icon className="size-4" />
              {t(labelKey)}
            </DropdownMenuPrimitive.RadioItem>
          ))}
        </DropdownMenuPrimitive.RadioGroup>

        <DropdownMenuSeparator />

        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">{t("themeToggle.theme")}</DropdownMenuLabel>
        <div className="max-h-[max(8rem,calc(var(--radix-dropdown-menu-content-available-height)-14rem))] overflow-y-auto overscroll-contain pr-1">
          {sortedThemes.map((theme) => {
            const isLocked = !!theme.locked;
            const isCurrentTheme = currentTheme.id === theme.id;
            const variation = getThemeVariation(theme.id);
            const variations = themeVariationColors(theme, dark);

            return (
              <DropdownMenuItem
                key={theme.id}
                aria-current={isCurrentTheme ? "true" : undefined}
                onSelect={(event) => {
                  event.preventDefault();
                  void handleThemeSelect(theme.id);
                }}
                className="block min-w-0"
                disabled={isLocked}
              >
                <QuickThemeRow
                  theme={theme}
                  palette={themeThumbnailPalette(theme, dark, variation)}
                  variations={variations}
                  isCurrent={isCurrentTheme}
                />
                {isCurrentTheme && variations.length > 0 && (
                  <QuickThemeVariations
                    variations={variations}
                    selected={variation}
                    onSelect={(variationId) => void handleVariationSelect(theme.id, variationId)}
                    className="mt-1.5 pl-11"
                  />
                )}
              </DropdownMenuItem>
            );
          })}
        </div>

        <DropdownMenuSeparator />
        <DropdownMenuItem asChild>
          <Link to="/settings" search={{ tab: "themes", modal: "theme-menu" }}>
            <ListChecks className="size-4" />
            {t("settings:themes.menu.button")}
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
