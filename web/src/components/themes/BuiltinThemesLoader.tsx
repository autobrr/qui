/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { applyBuiltinThemesPayload, useBuiltinThemes } from "@/hooks/useBuiltinThemes"

/**
 * The single registrar for the built-in theme registry, mounted app-wide
 * above auth so the login page paints the selected theme too.
 */
export function BuiltinThemesLoader(): null {
  const { data } = useBuiltinThemes()

  useEffect(() => {
    if (data) {
      applyBuiltinThemesPayload(data)
    }
  }, [data])

  return null
}
