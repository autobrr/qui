/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCustomThemes } from "@/hooks/useCustomThemes"
import { useThemeSettingsSync } from "@/hooks/useThemeSettingsSync"

/**
 * Mounts the custom-themes loader app-wide (for authenticated users) so a stored
 * custom theme is registered and applied even when the theme picker isn't open.
 * Also keeps the theme selection synced with the server for premium users.
 */
export function CustomThemesLoader(): null {
  useCustomThemes()
  useThemeSettingsSync()
  return null
}
