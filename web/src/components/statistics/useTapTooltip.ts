/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useEffect, useRef, useState, type CSSProperties, type MouseEvent, type RefObject } from "react"

export interface TapTooltip {
  // Spread on the element around the chart.
  wrapper: { ref: RefObject<HTMLDivElement | null>; onClick?: (event: MouseEvent) => void; onKeyDown?: () => void; style?: CSSProperties }
  // Spread on the recharts Tooltip. Without active, recharts shows the tooltip on hover.
  tooltip: { active?: boolean; wrapperStyle: CSSProperties }
}

/**
 * On a screen without hover, a tap on the plot of the chart opens its tooltip at the tapped point, or moves it there. A
 * tap on the tooltip, a tap outside the plot, or a scroll closes it. A finger that scrolls the page across the chart
 * opens nothing. A tap that opens the tooltip scrolls the whole chart into view above the bottom nav. A screen with hover
 * keeps the tooltip of recharts, and so does a key on the chart, until the next tap.
 */
export function useTapTooltip(): TapTooltip {
  const touch = useMediaQuery("(hover: none)")
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [keyboard, setKeyboard] = useState(false)
  // The scroll that a tap starts ends before this time, and does not close the tooltip that the tap opened. A touch or a
  // wheel of the user ends this time at once, so that the scroll of the user closes the tooltip.
  const ownScrollUntil = useRef(0)

  useEffect(() => {
    if (!open) {
      return
    }
    const outside = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }
    const close = () => {
      if (Date.now() >= ownScrollUntil.current) {
        setOpen(false)
      }
    }
    const userScrolls = () => {
      ownScrollUntil.current = 0
    }
    document.addEventListener("pointerdown", outside)
    window.addEventListener("scroll", close, { capture: true, passive: true })
    window.addEventListener("touchstart", userScrolls, { capture: true, passive: true })
    window.addEventListener("wheel", userScrolls, { capture: true, passive: true })
    return () => {
      document.removeEventListener("pointerdown", outside)
      window.removeEventListener("scroll", close, { capture: true })
      window.removeEventListener("touchstart", userScrolls, { capture: true })
      window.removeEventListener("wheel", userScrolls, { capture: true })
    }
  }, [open])

  // The legend comes after the tooltip, so without a z-index it paints over the lower rows of the tooltip.
  if (!touch) {
    return { wrapper: { ref }, tooltip: { wrapperStyle: { zIndex: 1 } } }
  }
  // recharts moves its active point to a tap on the plot. Outside the plot it has no point, and shows the first one.
  const onPlot = (event: MouseEvent) => {
    const plot = ref.current?.querySelector(".recharts-cartesian-grid")?.getBoundingClientRect()
    return plot !== undefined && event.clientX >= plot.left && event.clientX <= plot.right && event.clientY >= plot.top && event.clientY <= plot.bottom
  }
  const onTooltip = (event: MouseEvent) => event.target instanceof Element && event.target.closest(".recharts-tooltip-wrapper") !== null
  return {
    wrapper: {
      ref,
      onClick: event => {
        setKeyboard(false)
        const opens = !onTooltip(event) && onPlot(event)
        setOpen(opens)
        if (opens) {
          ownScrollUntil.current = Date.now() + 600
          ref.current?.scrollIntoView({ block: "nearest", behavior: "smooth" })
        }
      },
      onKeyDown: () => setKeyboard(true),
      // The bottom nav of a phone covers the end of the screen, so the chart scrolls into view above it.
      style: { scrollMarginBottom: "calc(4rem + env(safe-area-inset-bottom))" },
    },
    // recharts lets taps pass through its tooltip, so the tooltip takes them here to close on a tap.
    tooltip: { active: keyboard ? undefined : open, wrapperStyle: { zIndex: 1, pointerEvents: "auto" } },
  }
}
