/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type MouseEvent, type PointerEvent, useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from "react"
import { flushSync } from "react-dom"

const START_DISTANCE = 10
const OVERDRAG = 0.2
const FLICK_SPEED = 0.3 // px per ms
const VELOCITY_WINDOW_MS = 100
const SLIDE = "transform 280ms cubic-bezier(0.32, 0.72, 0, 1)"

/**
 * The offset of a row that follows the finger. from is its offset when the
 * swipe started, and width the width of its actions. The row stops at its
 * closed place, and past its actions it moves at a fifth of the finger.
 */
export function swipeOffset(from: number, dx: number, width: number): number {
  const offset = Math.min(0, from + dx)
  return offset >= -width ? offset : -width + (offset + width) * OVERDRAG
}

/**
 * Whether a released row ends with its actions in view. A flick faster than
 * 0.3 px/ms decides by its direction, else the row opens past half of the width.
 */
export function swipeEndsOpen(offset: number, velocity: number, width: number): boolean {
  if (Math.abs(velocity) > FLICK_SPEED) return velocity < 0
  return offset < -width / 2
}

function slide(track: HTMLElement | null, offset: number, transition: string) {
  if (!track) return
  track.style.transition = transition
  track.style.transform = offset === 0 ? "" : `translate3d(${offset}px, 0, 0)`
}

interface Swipe {
  pointerId: number
  x: number
  y: number
  // Set when the swipe starts to move the row.
  move?: { from: number; width: number; offset: number; samples: Array<{ x: number; t: number }> }
}

interface SwipeActionsOptions {
  open: boolean
  onOpenChange: (open: boolean) => void
  disabled?: boolean
}

/**
 * A row that a touch swipes to the left to show its actions. The track holds
 * the row, and the caller puts the actions past the right edge of the track.
 * The caller gives the track `touch-action: pan-y`, so the browser keeps the
 * vertical scroll, and clips the parent of the track. A tap on an open row
 * closes it, and an open row that leaves the page reports closed.
 * Render the actions only while `actionsMounted` is true. It is false until
 * the first swipe of the row, because a list has many rows and few get a swipe.
 */
export function useSwipeActions({ open, onOpenChange, disabled = false }: SwipeActionsOptions) {
  const trackRef = useRef<HTMLDivElement>(null)
  const actionsRef = useRef<HTMLDivElement>(null)
  const swipe = useRef<Swipe | null>(null)
  const [actionsMounted, setActionsMounted] = useState(false)
  // The click that follows a swipe is not a tap.
  const moved = useRef(false)

  const settle = (isOpen: boolean) => slide(trackRef.current, isOpen ? -(actionsRef.current?.offsetWidth ?? 0) : 0, SLIDE)

  useLayoutEffect(() => {
    slide(trackRef.current, open ? -(actionsRef.current?.offsetWidth ?? 0) : 0, SLIDE)
  }, [open])

  const closeOnUnmount = useEffectEvent(() => {
    if (open) onOpenChange(false)
  })
  useEffect(() => () => closeOnUnmount(), [])

  const onPointerDown = (e: PointerEvent<HTMLElement>) => {
    moved.current = false
    if (!e.isPrimary) {
      // A second finger ends the swipe.
      swipe.current = null
      settle(open)
    } else if (!disabled && e.pointerType !== "mouse") {
      swipe.current = { pointerId: e.pointerId, x: e.clientX, y: e.clientY }
    }
  }

  const onPointerMove = (e: PointerEvent<HTMLElement>) => {
    const s = swipe.current
    if (!s || e.pointerId !== s.pointerId) return
    if (!s.move) {
      const dx = e.clientX - s.x
      const dy = e.clientY - s.y
      if (Math.hypot(dx, dy) < START_DISTANCE) return
      if (Math.abs(dx) <= Math.abs(dy)) {
        swipe.current = null
        return
      }
      // The actions must be on the page before the row moves, because the row needs their width.
      flushSync(() => setActionsMounted(true))
      const width = actionsRef.current?.offsetWidth ?? 0
      const from = open ? -width : 0
      // The row starts from the finger, not START_DISTANCE behind it.
      s.x = e.clientX
      s.move = { from, width, offset: from, samples: [] }
      moved.current = true
      // The list updates live. The swipe must not stop when the element under the finger goes away.
      e.currentTarget.setPointerCapture(e.pointerId)
    }
    const { move } = s
    move.offset = swipeOffset(move.from, e.clientX - s.x, move.width)
    move.samples.push({ x: e.clientX, t: e.timeStamp })
    while (move.samples[0].t < e.timeStamp - VELOCITY_WINDOW_MS) move.samples.shift()
    slide(trackRef.current, move.offset, "none")
  }

  const onPointerUp = (e: PointerEvent<HTMLElement>) => {
    const s = swipe.current
    if (!s || e.pointerId !== s.pointerId) return
    swipe.current = null
    if (!s.move) return
    const { offset, width, samples } = s.move
    const first = samples[0]
    const last = samples[samples.length - 1]
    // A finger that rests before the release has no speed.
    const flicked = last.t > first.t && e.timeStamp - last.t < VELOCITY_WINDOW_MS
    const endsOpen = swipeEndsOpen(offset, flicked ? (last.x - first.x) / (last.t - first.t) : 0, width)
    settle(endsOpen)
    if (endsOpen !== open) onOpenChange(endsOpen)
  }

  // The browser took the touch for a scroll.
  const onPointerCancel = (e: PointerEvent<HTMLElement>) => {
    if (e.pointerId !== swipe.current?.pointerId) return
    swipe.current = null
    settle(open)
  }

  const onClickCapture = (e: MouseEvent<HTMLElement>) => {
    if (moved.current) {
      moved.current = false
      e.stopPropagation()
    } else if (open && !actionsRef.current?.contains(e.target as Node)) {
      e.stopPropagation()
      onOpenChange(false)
    }
  }

  return { trackRef, actionsRef, actionsMounted, trackProps: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel, onClickCapture } }
}
