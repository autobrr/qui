/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { layerColor, slotTotals, type ChartLayer } from "@/lib/dashboard-overview"
import { formatGapLength, labelledGaps } from "@/lib/statistics"
import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"

const W = 600
const UP_H = 120
const DOWN_H = 80

/**
 * Paths of the stacked layers of one half. dir is -1 for upload, which grows up from the zero line, and 1 for download.
 * An area chart of fewer values than slots ends at the right edge.
 */
function stackPaths(layers: ChartLayer[], half: "up" | "down", bars: boolean, slots: number): string[] {
  const height = half === "up" ? UP_H : DOWN_H
  const dir = half === "up" ? -1 : 1
  const length = layers[0]?.[half].length ?? 0
  const peak = Math.max(0, ...slotTotals(layers, half))
  if (length === 0 || (!bars && length < 2)) return []
  const base = Array<number>(length).fill(0)
  const step = W / length
  const x = (index: number) => ((Math.max(slots, length) - length + index) / (Math.max(slots, length) - 1)) * W
  const y = (value: number) => UP_H + dir * (value / Math.max(peak, 1)) * (height - 6)
  return layers.map(layer => {
    const top = layer[half].map((value, index) => base[index] + value)
    const d = bars
      ? top.map((value, index) => value === base[index] ? "" : `M${(index * step + 1.5).toFixed(1)},${y(base[index]).toFixed(1)}H${((index + 1) * step - 1.5).toFixed(1)}V${y(value).toFixed(1)}H${(index * step + 1.5).toFixed(1)}Z`).join("")
      : `${top.map((value, index) => `${index ? "L" : "M"}${x(index).toFixed(1)},${y(value).toFixed(1)}`).join("")}${base.map((value, index) => `L${x(index).toFixed(1)},${y(value).toFixed(1)}`).reverse().join("")}Z`
    top.forEach((value, index) => {
      base[index] = value
    })
    return d
  })
}

export interface StreamChartProps {
  layers: ChartLayer[]
  // Bars draws one bar for each value. Areas draws the values as one area for each layer.
  kind: "bars" | "areas"
  // The Recording gap of each bar, in seconds.
  gaps?: number[]
  // The title of each bar, for a chart of hourly bars.
  titles?: string[]
  // The number of values that fill the width of an area chart.
  slots?: number
  label: string
  className?: string
}

/** Stacked layers with upload above the zero line and download below it. Each half has its own scale. */
export function StreamChart({ layers, kind, gaps, titles, slots = 0, label, className }: StreamChartProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const bars = kind === "bars"
  const up = stackPaths(layers, "up", bars, slots)
  const down = stackPaths(layers, "down", bars, slots)
  const length = layers[0]?.up.length ?? gaps?.length ?? 0
  const gapBars = gaps?.flatMap((gap, index) => gap > 0 ? [index] : []) ?? []
  const labelled = labelledGaps(gapBars, length)
  const center = (index: number) => ((index + 0.5) / Math.max(length, 1)) * 100

  return (
    <div className={cn("relative", gapBars.length > 0 && "pt-4")}>
      <svg viewBox={`0 0 ${W} ${UP_H + DOWN_H}`} preserveAspectRatio="none" className={cn("block w-full", className)} role="img" aria-label={label}>
        {layers.map((layer, index) => (
          <g key={layer.layer} data-testid="chart-layer" data-layer={layer.layer}>
            {up[index] && <path d={up[index]} fill={layerColor(layer.layer)} />}
            {down[index] && <path d={down[index]} fill={layerColor(layer.layer)} opacity={0.55} />}
          </g>
        ))}
        <line x1="0" x2={W} y1={UP_H} y2={UP_H} stroke="var(--border)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        {gapBars.map(index => (
          <line
            key={index}
            data-testid="gap-mark"
            x1={(center(index) / 100) * W}
            x2={(center(index) / 100) * W}
            y1="0"
            y2={UP_H + DOWN_H}
            stroke="#eab308"
            strokeDasharray="2 3"
            vectorEffect="non-scaling-stroke"
          >
            <title>{t("chart.gap", { duration: formatGapLength(gaps![index], locale) })}</title>
          </line>
        ))}
        {titles?.map((title, index) => (
          <rect key={index} x={(index / length) * W} width={W / length} y="0" height={UP_H + DOWN_H} fill="transparent">
            <title>{title}</title>
          </rect>
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
