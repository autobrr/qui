/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { useClientSetting } from "@/lib/client-settings"
import {
  applyGlassOpacity,
  GLASS_SOLID_PERCENT,
  GLASS_STORAGE_KEY,
  parseGlassPercent,
  serializeGlassPercent
} from "@/lib/glass"

/** Theme-color amount for whichever theme is active. 100% is solid. Persists with the other client settings. */
export function useGlassmorphism(): [number, (percent: number) => void] {
  const [percent, setPercent] = useClientSetting<number>(GLASS_STORAGE_KEY, {
    defaultValue: GLASS_SOLID_PERCENT,
    parse: parseGlassPercent,
    serialize: serializeGlassPercent,
  })

  useEffect(() => {
    applyGlassOpacity(percent)
  }, [percent])

  return [percent, setPercent]
}
