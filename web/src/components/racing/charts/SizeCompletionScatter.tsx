/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import type { RacingTorrent } from "@/types"
import { useMemo } from "react"
import { CartesianGrid, Scatter, ScatterChart, XAxis, YAxis, ZAxis } from "recharts"

interface SizeCompletionScatterProps {
  data: RacingTorrent[]
}

export function SizeCompletionScatter({ data }: SizeCompletionScatterProps) {
  const chartData = useMemo(() => {
    return data
      .filter(t => t.completionTime && t.size)
      .map(torrent => ({
        name: torrent.name,
        size: Number((torrent.size / 1073741824).toFixed(2)), // Convert to GB
        completionTime: Math.round((torrent.completionTime || 0) / 60), // Convert to minutes
        ratio: torrent.ratio,
        tracker: torrent.tracker,
      }))
      .filter(d => d.completionTime < 1440) // Filter out torrents that took more than 24 hours
      .slice(0, 100) // Limit to 100 points for performance
  }, [data])

  const formatSize = (value: number) => `${value} GB`
  const formatTime = (value: number) => {
    if (value < 60) return `${value}m`
    return `${Math.floor(value / 60)}h ${value % 60}m`
  }

  const chartConfig = {
    scatter: {
      label: "Torrents",
      color: "var(--chart-1)",
    },
  }

  // Color based on ratio
  const getColor = (ratio: number) => {
    if (ratio >= 3) return "var(--chart-2)" // Green for high ratio
    if (ratio >= 1) return "var(--chart-1)" // Blue for medium ratio
    return "var(--chart-3)" // Red for low ratio
  }

  return (
    <ChartContainer config={chartConfig} className="h-[350px] w-full">
      <ScatterChart>
        <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
        <XAxis
          dataKey="size"
          name="Size"
          unit=" GB"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          tickFormatter={formatSize}
          domain={[0, "dataMax + 5"]}
        />
        <YAxis
          dataKey="completionTime"
          name="Time"
          unit=" min"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          tickFormatter={formatTime}
          domain={[0, "dataMax + 10"]}
        />
        <ZAxis dataKey="ratio" range={[50, 400]} />
        <ChartTooltip
          cursor={{ strokeDasharray: "3 3" }}
          content={
            <ChartTooltipContent
              formatter={(_value, _name, item) => {
                const payload = item.payload as any
                return (
                  <div className="space-y-1">
                    <div className="font-semibold text-xs truncate max-w-[200px]">
                      {payload.name}
                    </div>
                    <div className="text-xs space-y-0.5">
                      <div>Size: {payload.size} GB</div>
                      <div>Time: {formatTime(payload.completionTime)}</div>
                      <div>Ratio: {payload.ratio.toFixed(2)}</div>
                      <div className="text-muted-foreground truncate max-w-[200px]">
                        {payload.tracker}
                      </div>
                    </div>
                  </div>
                )
              }}
            />
          }
        />
        <Scatter
          name="Torrents"
          data={chartData}
          fill="var(--chart-1)"
          shape={(props: any) => {
            const { cx, cy, payload } = props
            return (
              <circle
                cx={cx}
                cy={cy}
                r={Math.min(8, Math.max(3, payload.ratio * 2))}
                fill={getColor(payload.ratio)}
                fillOpacity={0.6}
                stroke={getColor(payload.ratio)}
                strokeWidth={1}
              />
            )
          }}
        />
      </ScatterChart>
    </ChartContainer>
  )
}