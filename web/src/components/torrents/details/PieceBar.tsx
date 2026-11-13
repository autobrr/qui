/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { computePieceBarColumns, type PieceBarColumn } from "@/lib/piece-bar"
import { memo, useEffect, useRef } from "react"

interface PieceBarProps {
  pieceStates: number[] | undefined
  isLoading?: boolean
  isComplete?: boolean // When true and no pieceStates, show solid green bar
}

type Rgb = readonly [number, number, number]

const FINISHED_RGB: Rgb = [34, 197, 94]    // green-500
const DOWNLOADING_RGB: Rgb = [234, 179, 8] // yellow-500
const EMPTY_RGB: Rgb = [63, 63, 70]        // zinc-700
const BACKGROUND_COLOR = "#27272a"         // zinc-800 (slightly lighter than empty for contrast)

function toCssColor([r, g, b]: Rgb): string {
  return `rgb(${r}, ${g}, ${b})`
}

function columnColor({ finished, downloading }: PieceBarColumn): string {
  const empty = Math.max(0, 1 - finished - downloading)
  const channel = (i: number) =>
    Math.round(EMPTY_RGB[i] * empty + FINISHED_RGB[i] * finished + DOWNLOADING_RGB[i] * downloading)
  return toCssColor([channel(0), channel(1), channel(2)])
}

export const PieceBar = memo(function PieceBar({
  pieceStates,
  isLoading = false,
  isComplete = false,
}: PieceBarProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!canvasRef.current || !containerRef.current) return
    if (!pieceStates || pieceStates.length === 0) return

    const canvas = canvasRef.current
    const container = containerRef.current
    const ctx = canvas.getContext("2d")
    if (!ctx) return

    let rafId: number | undefined

    const draw = () => {
      // Get container width for responsive sizing
      const containerWidth = container.clientWidth
      if (containerWidth <= 0) return

      const barHeight = 12

      // Set canvas size (use device pixel ratio for sharp rendering)
      const dpr = window.devicePixelRatio || 1
      canvas.width = containerWidth * dpr
      canvas.height = barHeight * dpr
      canvas.style.width = `${containerWidth}px`
      canvas.style.height = `${barHeight}px`
      ctx.setTransform(1, 0, 0, 1, 0, 0)
      ctx.scale(dpr, dpr)

      // Clear canvas with background
      ctx.fillStyle = BACKGROUND_COLOR
      ctx.beginPath()
      ctx.roundRect(0, 0, containerWidth, barHeight, 4)
      ctx.fill()

      const columns = computePieceBarColumns(pieceStates, containerWidth)
      const columnWidth = containerWidth / columns.length

      columns.forEach((column, i) => {
        ctx.fillStyle = columnColor(column)
        ctx.fillRect(i * columnWidth, 0, columnWidth + 0.5, barHeight) // +0.5 to avoid gaps
      })

      // Apply rounded corners mask by redrawing with clip
      ctx.globalCompositeOperation = "destination-in"
      ctx.beginPath()
      ctx.roundRect(0, 0, containerWidth, barHeight, 4)
      ctx.fill()
      ctx.globalCompositeOperation = "source-over"
    }

    const scheduleDraw = () => {
      if (rafId != null) cancelAnimationFrame(rafId)
      rafId = requestAnimationFrame(draw)
    }

    scheduleDraw()

    const view = container.ownerDocument.defaultView

    const ResizeObserverImpl = (globalThis as unknown as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver
    let observer: ResizeObserver | undefined
    if (ResizeObserverImpl) {
      observer = new ResizeObserverImpl(() => scheduleDraw())
      observer.observe(container)
    } else {
      view?.addEventListener("resize", scheduleDraw)
    }

    return () => {
      if (rafId != null) cancelAnimationFrame(rafId)
      observer?.disconnect()
      view?.removeEventListener("resize", scheduleDraw)
    }
  }, [pieceStates])

  // Show solid green bar for completed torrents (no need to fetch piece states)
  if (isComplete && (!pieceStates || pieceStates.length === 0)) {
    return (
      <div
        className="w-full h-3 rounded"
        style={{ backgroundColor: toCssColor(FINISHED_RGB) }}
      />
    )
  }

  // Show loading state or empty bar
  if (isLoading || !pieceStates || pieceStates.length === 0) {
    return (
      <div
        ref={containerRef}
        className="w-full h-3 rounded bg-zinc-800 animate-pulse"
      />
    )
  }

  return (
    <div ref={containerRef} className="w-full">
      <canvas
        ref={canvasRef}
        className="w-full rounded"
      />
    </div>
  )
})
