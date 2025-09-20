/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TrackerStatRow } from "@/components/racing/columns-tracker-stats"
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import { Bar, CartesianGrid, ComposedChart, Line, XAxis, YAxis } from "recharts"

interface TrackerPerformanceChartProps {
  data: TrackerStatRow[]
}

export function TrackerPerformanceChart({ data }: TrackerPerformanceChartProps) {
  // Sort by total torrents and take top 10
  const chartData = data
    .sort((a, b) => b.totalTorrents - a.totalTorrents)
    .slice(0, 10)
    .map(tracker => ({
      tracker: tracker.tracker.length > 20 ? tracker.tracker.substring(0, 20) + "..." : tracker.tracker,
      totalTorrents: tracker.totalTorrents,
      averageRatio: Number(tracker.averageRatio.toFixed(2)),
      completionRate: Number(((tracker.completedTorrents / tracker.totalTorrents) * 100).toFixed(1)),
    }))

  const chartConfig = {
    totalTorrents: {
      label: "Total Torrents",
      color: "var(--chart-1)",
    },
    averageRatio: {
      label: "Avg Ratio",
      color: "var(--chart-3)",
    },
  }

  return (
    <ChartContainer config={chartConfig} className="h-[350px] w-full">
      <ComposedChart data={chartData}>
        <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
        <XAxis
          dataKey="tracker"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          angle={-45}
          textAnchor="end"
          height={80}
        />
        <YAxis
          yAxisId="left"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          label={{ value: "Total Torrents", angle: -90, position: "insideLeft", style: { fill: "var(--muted-foreground)", fontSize: 11 } }}
        />
        <YAxis
          yAxisId="right"
          orientation="right"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          domain={[0, "dataMax + 1"]}
          label={{ value: "Average Ratio", angle: 90, position: "insideRight", style: { fill: "var(--muted-foreground)", fontSize: 11 } }}
        />
        <ChartTooltip content={<ChartTooltipContent />} />
        <ChartLegend content={<ChartLegendContent />} />
        <Bar
          yAxisId="left"
          dataKey="totalTorrents"
          fill="var(--chart-1)"
          radius={[4, 4, 0, 0]}
        />
        <Line
          yAxisId="right"
          type="monotone"
          dataKey="averageRatio"
          stroke="var(--chart-3)"
          strokeWidth={2}
          dot={{ fill: "var(--chart-3)", r: 4 }}
        />
      </ComposedChart>
    </ChartContainer>
  )
}