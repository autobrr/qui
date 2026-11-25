/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { layerAt, layerColor, slotTotals, type ChartLayer } from "@/lib/dashboard-overview"
import { formatGapLength, labelledGaps } from "@/lib/statistics"
import { cn } from "@/lib/utils"
import type { MouseEvent } from "react"
import { useTranslation } from "react-i18next"

const W = 600
const H = 200
const DOWN_H = 80

function filledRuns(values: (number | null)[]): [number, number][] {
  const runs: [number, number][] = []
  values.forEach((value, index) => {
    if (value === null) return
    const last = runs.at(-1)
    if (last && last[1] === index) last[1] = index + 1
    else runs.push([index, index + 1])
  })
  return runs
}

function halfScale(layers: ChartLayer[], half: "up" | "down", zero: number): { peak: number; reach: number } {
  return { peak: Math.max(1, ...slotTotals(layers, half)), reach: (half === "up" ? zero : H - zero) - 6 }
}

function stackPaths(layers: ChartLayer[], half: "up" | "down", bars: boolean, slots: number, zero: number): string[] {
  const dir = half === "up" ? -1 : 1
  const length = layers[0]?.[half].length ?? 0
  const { peak, reach } = halfScale(layers, half, zero)
  if (length === 0 || (!bars && length < 2)) return []
  const base = Array<number>(length).fill(0)
  const step = W / length
  const x = (index: number) => ((Math.max(slots, length) - length + index) / (Math.max(slots, length) - 1)) * W
  const y = (value: number) => zero + dir * (value / peak) * reach
  return layers.map(layer => {
    const top = layer[half].map((value, index) => value === null ? null : base[index] + value)
    const d = bars
      ? top.map((value, index) => value === null || value === base[index] ? "" : `M${(index * step + 1.5).toFixed(1)},${y(base[index]).toFixed(1)}H${((index + 1) * step - 1.5).toFixed(1)}V${y(value).toFixed(1)}H${(index * step + 1.5).toFixed(1)}Z`).join("")
      : filledRuns(top).filter(([start, end]) => end - start > 1).map(([start, end]) => {
        const indexes = Array.from({ length: end - start }, (_, offset) => start + offset)
        return `${indexes.map((index, at) => `${at ? "L" : "M"}${x(index).toFixed(1)},${y(top[index] ?? 0).toFixed(1)}`).join("")}${indexes.reverse().map(index => `L${x(index).toFixed(1)},${y(base[index]).toFixed(1)}`).join("")}Z`
      }).join("")
    top.forEach((value, index) => {
      if (value !== null) base[index] = value
    })
    return d
  })
}

export interface StreamChartProps {
  layers: ChartLayer[]
  kind: "bars" | "areas"
  gaps?: number[]
  slots?: number
  label: string
  className?: string
  pointed?: number
  onPoint?: (layer: number | undefined) => void
}

export function StreamChart({ layers, kind, gaps, slots = 0, label, className, pointed, onPoint }: StreamChartProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const bars = kind === "bars"
  const zero = layers.some(layer => layer.down.some(Boolean)) ? H - DOWN_H : H
  const up = stackPaths(layers, "up", bars, slots, zero)
  const down = stackPaths(layers, "down", bars, slots, zero)
  const length = layers[0]?.up.length ?? gaps?.length ?? 0
  const gapBars = gaps?.flatMap((gap, index) => gap > 0 ? [index] : []) ?? []
  const labelled = labelledGaps(gapBars, length)
  const center = (index: number) => ((index + 0.5) / Math.max(length, 1)) * 100

  const layerUnder = (event: MouseEvent<SVGSVGElement>) => {
    const box = event.currentTarget.getBoundingClientRect()
    const across = (event.clientX - box.left) / box.width
    const y = ((event.clientY - box.top) / box.height) * H
    const half = y <= zero ? "up" : "down"
    const { peak, reach } = halfScale(layers, half, zero)
    const points = Math.max(slots, length)
    const at = bars ? Math.floor(across * length) : across * (points - 1) - (points - length)
    return layerAt(layers, half, at, (Math.abs(y - zero) / reach) * peak)
  }
  const point = onPoint && ((event: MouseEvent<SVGSVGElement>) => onPoint(layerUnder(event)))

  return (
    <div className={cn("relative", gapBars.length > 0 && "pt-4")}>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className={cn("block w-full", className)}
        role="img"
        aria-label={label}
        onMouseMove={point}
        onClick={point}
        onMouseLeave={onPoint && (() => onPoint(undefined))}
      >
        {layers.map((layer, index) => (
          <g
            key={layer.layer}
            data-testid="chart-layer"
            data-layer={layer.layer}
            data-pointed={pointed === layer.layer || undefined}
            data-faded={(pointed !== undefined && pointed !== layer.layer) || undefined}
            className="transition-opacity duration-150 data-faded:opacity-30 data-pointed:fill-primary motion-reduce:transition-none"
            fill={layerColor(layer.layer)}
            stroke={layers.length > 1 ? "var(--card)" : undefined}
          >
            {up[index] && <path d={up[index]} vectorEffect="non-scaling-stroke" />}
            {down[index] && <path d={down[index]} opacity={0.55} vectorEffect="non-scaling-stroke" />}
          </g>
        ))}
        <line x1="0" x2={W} y1={zero} y2={zero} stroke="var(--border)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        {gapBars.map(index => (
          <line
            key={index}
            data-testid="gap-mark"
            x1={(center(index) / 100) * W}
            x2={(center(index) / 100) * W}
            y1="0"
            y2={H}
            stroke="#eab308"
            strokeDasharray="2 3"
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </svg>
      {gapBars.filter(index => labelled.has(index)).map(index => (
        <span
          key={index}
          className="absolute top-0 -translate-x-1/2 whitespace-nowrap text-[10px] text-yellow-600 dark:text-yellow-500"
          style={{ left: `${center(index)}%` }}
        >
          {t("chart.gapLabel", { duration: formatGapLength(gaps![index], locale) })}
        </span>
      ))}
    </div>
  )
}
