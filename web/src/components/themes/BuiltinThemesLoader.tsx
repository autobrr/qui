/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useClientSettingsSync } from "@/hooks/useClientSettingsSync"
import { useThemeSettingsSync } from "@/hooks/useThemeSettingsSync"

/**
 * Mounts the built-in theme query and the server theme-settings sync app-wide,
 * above auth, so the login page paints the selected theme too. Registration
 * happens in the queryFn. The client-settings sync rides along; its query is
 * auth-gated and idles on the login page.
 */
export function BuiltinThemesLoader(): null {
  useBuiltinThemes()
  useThemeSettingsSync()
  useClientSettingsSync()
  return null
}
