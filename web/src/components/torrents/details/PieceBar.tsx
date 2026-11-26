/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { computePieceBarColumns, computePieceBarFill } from "@/lib/piece-bar"
import { memo, useEffect, useRef } from "react"

interface PieceBarProps {
  pieceStates: number[] | undefined
  isLoading?: boolean
  isComplete?: boolean
}

const FINISHED_COLOR = "#22c55e"
const DOWNLOADING_COLOR = "#eab308"
const EMPTY_COLOR = "#3f3f46"

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
      const containerWidth = container.clientWidth
      if (containerWidth <= 0) return

      const barHeight = 12

      const dpr = window.devicePixelRatio || 1
      canvas.width = containerWidth * dpr
      canvas.height = barHeight * dpr
      canvas.style.width = `${containerWidth}px`
      canvas.style.height = `${barHeight}px`
      const radius = 4 * dpr

      ctx.fillStyle = EMPTY_COLOR
      ctx.beginPath()
      ctx.roundRect(0, 0, canvas.width, canvas.height, radius)
      ctx.fill()

      const columns = computePieceBarColumns(pieceStates, canvas.width)
      const columnWidth = canvas.width / columns.length

      columns.forEach((column, i) => {
        const left = Math.round(i * columnWidth)
        const right = Math.round((i + 1) * columnWidth)
        const { finishedPixels, downloadingPixels } = computePieceBarFill(column, canvas.height)
        const finishedTop = canvas.height - finishedPixels
        ctx.fillStyle = FINISHED_COLOR
        ctx.fillRect(left, finishedTop, right - left, finishedPixels)
        ctx.fillStyle = DOWNLOADING_COLOR
        ctx.fillRect(left, finishedTop - downloadingPixels, right - left, downloadingPixels)
      })

      ctx.globalCompositeOperation = "destination-in"
      ctx.beginPath()
      ctx.roundRect(0, 0, canvas.width, canvas.height, radius)
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

  if (isComplete && (!pieceStates || pieceStates.length === 0)) {
    return (
      <div
        className="w-full h-3 rounded"
        style={{ backgroundColor: FINISHED_COLOR }}
      />
    )
  }

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
