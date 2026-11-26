/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type MouseEvent, type PointerEvent, useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from "react"
import { flushSync } from "react-dom"

const START_DISTANCE = 10
const OVERDRAG = 0.2
const FLICK_SPEED = 0.3
const VELOCITY_WINDOW_MS = 100
const SLIDE = "transform 280ms cubic-bezier(0.32, 0.72, 0, 1)"

export function swipeOffset(from: number, dx: number, width: number): number {
  const offset = Math.min(0, from + dx)
  return offset >= -width ? offset : -width + (offset + width) * OVERDRAG
}

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
  move?: { from: number; width: number; offset: number; samples: Array<{ x: number; t: number }> }
}

interface SwipeActionsOptions {
  open: boolean
  onOpenChange: (open: boolean) => void
  disabled?: boolean
}

export function useSwipeActions({ open, onOpenChange, disabled = false }: SwipeActionsOptions) {
  const trackRef = useRef<HTMLDivElement>(null)
  const actionsRef = useRef<HTMLDivElement>(null)
  const swipe = useRef<Swipe | null>(null)
  const [actionsMounted, setActionsMounted] = useState(false)
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
      flushSync(() => setActionsMounted(true))
      const width = actionsRef.current?.offsetWidth ?? 0
      const from = open ? -width : 0
      s.x = e.clientX
      s.move = { from, width, offset: from, samples: [] }
      moved.current = true
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
    const flicked = last.t > first.t && e.timeStamp - last.t < VELOCITY_WINDOW_MS
    const endsOpen = swipeEndsOpen(offset, flicked ? (last.x - first.x) / (last.t - first.t) : 0, width)
    settle(endsOpen)
    if (endsOpen !== open) onOpenChange(endsOpen)
  }

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
