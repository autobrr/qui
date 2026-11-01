/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type TouchEvent, useRef } from "react"

const START_DISTANCE = 10
const HORIZONTAL_RATIO = 1.5
const CLOSE_FRACTION = 0.3
const CLOSE_SPEED = 0.5 // px per ms
const SLIDE_MS = 150

type SwipeMode = "pending" | "follow" | "ignore"
type SwipeResult = SwipeMode | "close" | "return"

interface SwipeGesture {
  dx: number
  dy: number
  // Time since the touch started
  ms: number
  width: number
}

/**
 * Decides what a swipe from left to right does. After 10 px of movement it
 * follows the finger when the movement goes right and is more than 1.5 times
 * the vertical movement; else it is a scroll. On release it closes past 30% of
 * the width or above 0.5 px/ms, else the view returns to its place.
 */
export function decideSwipe(mode: SwipeMode, { dx, dy, ms, width }: SwipeGesture, released: boolean): SwipeResult {
  if (mode === "pending") {
    if (Math.hypot(dx, dy) < START_DISTANCE) return released ? "ignore" : "pending"
    mode = dx > 0 && Math.abs(dx) > Math.abs(dy) * HORIZONTAL_RATIO ? "follow" : "ignore"
  }
  if (mode === "ignore" || !released) return mode
  return dx > width * CLOSE_FRACTION || dx / Math.max(1, ms) > CLOSE_SPEED ? "close" : "return"
}

function scrollsSideways(target: EventTarget | null, stop: Element): boolean {
  for (let el = target instanceof Element ? target : null; el && el !== stop; el = el.parentElement) {
    if (el.matches("input, textarea, select, [role=slider]")) return true
    const overflowX = getComputedStyle(el).overflowX
    if ((overflowX === "auto" || overflowX === "scroll") && el.scrollWidth > el.clientWidth + 1) return true
  }
  return false
}

// The Sheet renders its dimmed overlay right before its content
function overlayOf(el: HTMLElement): HTMLElement | null {
  const prev = el.previousElementSibling
  return prev instanceof HTMLElement && prev.dataset.slot === "sheet-overlay" ? prev : null
}

/**
 * Touch handlers for a Sheet content that closes on a swipe from left to
 * right. The view follows the finger and the overlay brightens with it.
 */
export function useSwipeToClose(onClose: () => void) {
  const gesture = useRef<{ x: number; y: number; t: number; dx: number; dy: number; mode: SwipeMode } | null>(null)

  const onTouchStart = (e: TouchEvent<HTMLElement>) => {
    const touch = e.touches[0]
    // React passes touches from a portal (a dialog, an open menu) up to the Sheet
    const inView = e.target instanceof Node && e.currentTarget.contains(e.target)
    gesture.current = !inView || e.touches.length > 1 || scrollsSideways(e.target, e.currentTarget)? null: { x: touch.clientX, y: touch.clientY, t: performance.now(), dx: 0, dy: 0, mode: "pending" }
  }

  const onTouchMove = (e: TouchEvent<HTMLElement>) => {
    const g = gesture.current
    if (!g) return
    if (e.touches.length > 1) {
      g.mode = "ignore"
      return
    }
    g.dx = e.touches[0].clientX - g.x
    g.dy = e.touches[0].clientY - g.y
    const el = e.currentTarget
    g.mode = decideSwipe(g.mode, { dx: g.dx, dy: g.dy, ms: performance.now() - g.t, width: el.clientWidth }, false) as SwipeMode
    if (g.mode !== "follow") return
    const offset = Math.max(0, g.dx)
    el.style.transition = "none"
    el.style.transform = `translateX(${offset}px)`
    const overlay = overlayOf(el)
    if (overlay) {
      overlay.style.transition = "none"
      overlay.style.opacity = String(1 - offset / el.clientWidth)
    }
  }

  const onTouchEnd = (e: TouchEvent<HTMLElement>) => {
    const g = gesture.current
    gesture.current = null
    if (!g || g.mode !== "follow") return
    const el = e.currentTarget
    const result = decideSwipe(g.mode, { dx: g.dx, dy: g.dy, ms: performance.now() - g.t, width: el.clientWidth }, true)
    const overlay = overlayOf(el)
    el.style.transition = `transform ${SLIDE_MS}ms ease-out`
    if (overlay) overlay.style.transition = `opacity ${SLIDE_MS}ms ease-out`
    if (result === "close") {
      el.style.transform = "translateX(100%)"
      // Skip the exit animations of the Sheet and the overlay: the view would
      // slide in from the left edge again, and the overlay fade a second time
      el.style.animation = "none"
      if (overlay) {
        overlay.style.opacity = "0"
        overlay.style.animation = "none"
      }
      setTimeout(onClose, SLIDE_MS)
    } else {
      el.style.transform = ""
      if (overlay) overlay.style.opacity = ""
    }
  }

  return { onTouchStart, onTouchMove, onTouchEnd, onTouchCancel: onTouchEnd }
}
