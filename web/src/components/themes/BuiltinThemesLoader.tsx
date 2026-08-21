/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"

/**
 * Mounts the built-in theme query app-wide, above auth, so the login page
 * paints the selected theme too. Registration happens in the queryFn.
 */
export function BuiltinThemesLoader(): null {
  useBuiltinThemes()
  return null
}
