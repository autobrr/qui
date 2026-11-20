/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { themes } from "@/config/themes"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { useTheme } from "@/hooks/useTheme"
import { buildThemeCatalog } from "@/lib/theme-catalog"
import { themeThumbnailPalette } from "@/lib/theme-thumbnail"
import { getThemeVariation } from "@/utils/theme"
import { Lock, Monitor, Moon, Palette, Sun } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import type { PaletteEntry } from "./palette"

const MODES = [
  { mode: "light", icon: Sun },
  { mode: "dark", icon: Moon },
  { mode: "auto", icon: Monitor },
] as const

/**
 * The Theme row. Its second level holds the three modes and every theme.
 * `close` shuts the palette.
 */
export function useThemeEntry(close: () => void): PaletteEntry {
  const { t } = useTranslation(["common", "settings"])
  const { mode, dark, currentTheme, setTheme, setThemeMode } = useTheme()
  const { customThemes } = useCustomThemes()
  // The built-in themes arrive from the server, and this subscription renders the row again when they do.
  useBuiltinThemes()

  const label = t("common:themeToggle.theme")
  const modeHeading = t("common:themeToggle.mode")
  const modeNames = { light: t("common:themeToggle.light"), dark: t("common:themeToggle.dark"), auto: t("common:themeToggle.system") }
  const pick = (change: () => Promise<void>, done: string) => () => {
    close()
    void change().then(() => toast.success(done))
  }

  return {
    id: "action.theme",
    label,
    icon: Palette,
    note: `${currentTheme.name} · ${modeNames[mode]}`,
    sections: [
      {
        id: "theme.modes",
        label: modeHeading,
        entries: MODES.map(({ mode: value, icon }) => ({
          id: `theme-mode.${value}`,
          label: modeNames[value],
          icon,
          trail: `${label} › ${modeHeading}`,
          current: value === mode,
          run: pick(() => setThemeMode(value), t("common:themeToggle.switchedToMode", { mode: modeNames[value] })),
        })),
      },
      {
        id: "theme.themes",
        label,
        entries: buildThemeCatalog(themes, customThemes).map((theme): PaletteEntry => {
          // In a search from the first level, the theme names would push the torrents down the list.
          // The picture has the colors that the theme menu shows for the theme.
          const row = { id: `theme.${theme.id}`, label: theme.name, ownListOnly: true, thumbnail: themeThumbnailPalette(theme, dark, getThemeVariation(theme.id)) }
          // A theme that needs a license has no colors to apply.
          if (theme.locked) return { ...row, icon: Lock, note: t("settings:themes.selector.needsLicense"), disabled: true }
          return { ...row, icon: Palette, current: theme.id === currentTheme.id, run: pick(() => setTheme(theme.id), t("common:themeToggle.switchedToTheme", { theme: theme.name })) }
        }),
      },
    ],
  }
}
