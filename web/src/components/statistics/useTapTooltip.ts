/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useEffect, useRef, useState, type CSSProperties, type MouseEvent, type RefObject } from "react"

export interface TapTooltip {
  wrapper: { ref: RefObject<HTMLDivElement | null>; onClick?: (event: MouseEvent) => void; onKeyDown?: () => void; style?: CSSProperties }
  tooltip: { active?: boolean; wrapperStyle: CSSProperties }
}

export function useTapTooltip(): TapTooltip {
  const touch = useMediaQuery("(hover: none)")
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [keyboard, setKeyboard] = useState(false)
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

  if (!touch) {
    return { wrapper: { ref }, tooltip: { wrapperStyle: { zIndex: 1 } } }
  }
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
      style: { scrollMarginBottom: "calc(4rem + env(safe-area-inset-bottom))" },
    },
    tooltip: { active: keyboard ? undefined : open, wrapperStyle: { zIndex: 1, pointerEvents: "auto" } },
  }
}
