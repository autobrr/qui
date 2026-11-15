/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useEffect, useRef, useState, type CSSProperties, type MouseEvent, type RefObject } from "react"

export interface TapTooltip {
  // Spread on the element around the chart.
  wrapper: { ref: RefObject<HTMLDivElement | null>; onClick?: (event: MouseEvent) => void }
  // Spread on the recharts Tooltip. Without active, recharts shows the tooltip on hover.
  tooltip: { active?: boolean; wrapperStyle: CSSProperties }
}

/**
 * On a screen without hover, a tap on the chart opens its tooltip at the tapped point, or moves it there. A tap on the
 * tooltip, a tap outside the chart, or a scroll closes it. A finger that scrolls the page across the chart opens
 * nothing. A screen with hover keeps the tooltip of recharts.
 */
export function useTapTooltip(): TapTooltip {
  const touch = useMediaQuery("(hover: none)")
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (!open) {
      return
    }
    const outside = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }
    const close = () => setOpen(false)
    document.addEventListener("pointerdown", outside)
    window.addEventListener("scroll", close, { capture: true, passive: true })
    return () => {
      document.removeEventListener("pointerdown", outside)
      window.removeEventListener("scroll", close, { capture: true })
    }
  }, [open])

  // The legend comes after the tooltip, so without a z-index it paints over the lower rows of the tooltip.
  if (!touch) {
    return { wrapper: { ref }, tooltip: { wrapperStyle: { zIndex: 1 } } }
  }
  return {
    // recharts moves its active point to the tap, so a tap on the chart opens the tooltip there.
    wrapper: { ref, onClick: event => setOpen(!(event.target instanceof Element && event.target.closest(".recharts-tooltip-wrapper"))) },
    // recharts lets taps pass through its tooltip, so the tooltip takes them here to close on a tap.
    tooltip: { active: open, wrapperStyle: { zIndex: 1, pointerEvents: "auto" } },
  }
}
