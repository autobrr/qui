/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import type { RacingTorrent } from "@/types"
import { format } from "date-fns"
import { useMemo } from "react"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"

interface VolumeChartProps {
  data: RacingTorrent[]
  timeRange: string
}

export function VolumeChart({ data, timeRange }: VolumeChartProps) {
  const chartData = useMemo(() => {
    // Group data by time period based on timeRange
    const groupedData = new Map<string, { uploaded: number, downloaded: number, count: number }>()

    data.forEach(torrent => {
      if (!torrent.completedOn) return

      const date = new Date(torrent.completedOn)
      let key: string

      // Group by appropriate time period
      if (timeRange === "24h") {
        key = format(date, "HH:00")
      } else if (timeRange === "7d") {
        key = format(date, "MMM dd")
      } else if (timeRange === "30d") {
        key = format(date, "MMM dd")
      } else {
        key = format(date, "MMM yyyy")
      }

      if (!groupedData.has(key)) {
        groupedData.set(key, { uploaded: 0, downloaded: 0, count: 0 })
      }

      const group = groupedData.get(key)!
      // Use ratio to estimate upload/download
      // For completed torrents, downloaded = size, uploaded = size * ratio
      const downloaded = torrent.size || 0
      const uploaded = downloaded * (torrent.ratio || 0)
      group.uploaded += uploaded
      group.downloaded += downloaded
      group.count++
    })

    // Format for chart (convert to GB)
    const chartData = Array.from(groupedData.entries()).map(([date, values]) => ({
      date,
      uploaded: Number((values.uploaded / 1073741824).toFixed(2)),
      downloaded: Number((values.downloaded / 1073741824).toFixed(2)),
      total: Number(((values.uploaded + values.downloaded) / 1073741824).toFixed(2)),
      count: values.count,
    }))

    return chartData.slice(-20) // Limit to last 20 data points
  }, [data, timeRange])

  const chartConfig = {
    uploaded: {
      label: "Uploaded",
      color: "var(--chart-1)",
    },
    downloaded: {
      label: "Downloaded",
      color: "var(--chart-2)",
    },
    total: {
      label: "Total",
      color: "var(--chart-3)",
    },
  }

  return (
    <ChartContainer config={chartConfig} className="h-[300px] w-full">
      <AreaChart data={chartData}>
        <defs>
          <linearGradient id="colorUpload" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="var(--chart-1)" stopOpacity={0.8} />
            <stop offset="95%" stopColor="var(--chart-1)" stopOpacity={0.1} />
          </linearGradient>
          <linearGradient id="colorDownload" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="var(--chart-2)" stopOpacity={0.8} />
            <stop offset="95%" stopColor="var(--chart-2)" stopOpacity={0.1} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
        <XAxis
          dataKey="date"
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
        />
        <YAxis
          className="text-xs"
          tick={{ fill: "var(--muted-foreground)" }}
          tickFormatter={(value) => `${value} GB`}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) => {
                if (typeof value === "number") {
                  return (
                    <div className="flex items-center justify-between gap-2">
                      <span>{name}:</span>
                      <span className="font-mono">{value.toFixed(2)} GB</span>
                    </div>
                  )
                }
                return null
              }}
            />
          }
        />
        <ChartLegend content={<ChartLegendContent />} />
        <Area
          type="monotone"
          dataKey="uploaded"
          stroke="var(--chart-1)"
          fillOpacity={1}
          fill="url(#colorUpload)"
        />
        <Area
          type="monotone"
          dataKey="downloaded"
          stroke="var(--chart-2)"
          fillOpacity={1}
          fill="url(#colorDownload)"
        />
      </AreaChart>
    </ChartContainer>
  )
}