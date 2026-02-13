/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useEffect, useRef } from "react"

interface PieceBarProps {
  pieceStates: number[] | undefined
  isLoading?: boolean
  isComplete?: boolean // When true and no pieceStates, show solid green bar
}

// Piece states from qBittorrent API
const PIECE_STATE = {
  NOT_DOWNLOADED: 0,
  DOWNLOADING: 1,
  DOWNLOADED: 2,
} as const

// Colors for piece states
const COLORS = {
  downloaded: "#22c55e",    // green-500
  downloading: "#eab308",   // yellow-500
  notDownloaded: "#3f3f46", // zinc-700
  background: "#27272a",    // zinc-800 (slightly lighter than notDownloaded for contrast)
} as const

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
      ctx.fillStyle = COLORS.background
      ctx.beginPath()
      ctx.roundRect(0, 0, containerWidth, barHeight, 4)
      ctx.fill()

      // Calculate bucket size - aggregate pieces when there are more pieces than pixels
      const bucketSize = Math.max(1, Math.ceil(pieceStates.length / containerWidth))
      const numBuckets = Math.ceil(pieceStates.length / bucketSize)
      if (numBuckets <= 0) return

      // Draw each bucket
      const pieceWidth = containerWidth / numBuckets

      for (let i = 0; i < numBuckets; i++) {
        const startIdx = i * bucketSize
        const endIdx = Math.min(startIdx + bucketSize, pieceStates.length)
        const bucket = pieceStates.slice(startIdx, endIdx)

        // Determine dominant state in bucket
        // Priority: downloading (show activity) > downloaded > not-downloaded
        let hasDownloading = false
        let hasNotDownloaded = false
        let hasDownloaded = false

        for (const state of bucket) {
          const stateNum = Number(state)
          if (stateNum === PIECE_STATE.DOWNLOADING) hasDownloading = true
          else if (stateNum === PIECE_STATE.NOT_DOWNLOADED) hasNotDownloaded = true
          else if (stateNum === PIECE_STATE.DOWNLOADED) hasDownloaded = true
        }

        // Choose color based on priority
        let color: string
        if (hasDownloading) {
          color = COLORS.downloading
        } else if (hasDownloaded && !hasNotDownloaded) {
          color = COLORS.downloaded
        } else if (hasNotDownloaded && !hasDownloaded) {
          color = COLORS.notDownloaded
        } else if (hasDownloaded) {
          // Mixed bucket with both downloaded and not-downloaded - show downloaded
          color = COLORS.downloaded
        } else {
          color = COLORS.notDownloaded
        }

        // Draw bucket segment
        const x = i * pieceWidth
        ctx.fillStyle = color
        ctx.fillRect(x, 0, pieceWidth + 0.5, barHeight) // +0.5 to avoid gaps
      }

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
        style={{ backgroundColor: COLORS.downloaded }}
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
