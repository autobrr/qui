/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCustomThemes } from "@/hooks/useCustomThemes"

/**
 * Mounts the custom-themes loader app-wide (for authenticated users) so a stored
 * custom theme is registered and applied even when the theme picker isn't open.
 */
export function CustomThemesLoader(): null {
  useCustomThemes()
  return null
}
