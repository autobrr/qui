/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { useClientSettingsSync } from "@/hooks/useClientSettingsSync"
import { useThemeSettingsSync } from "@/hooks/useThemeSettingsSync"

export function BuiltinThemesLoader(): null {
  useBuiltinThemes()
  useThemeSettingsSync()
  useClientSettingsSync()
  return null
}
