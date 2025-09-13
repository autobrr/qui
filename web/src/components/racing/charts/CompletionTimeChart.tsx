/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart"
import type { RacingTorrent } from "@/types"
import { format } from "date-fns"
import { useMemo } from "react"
import { Area, AreaChart, CartesianGrid, Line, XAxis, YAxis } from "recharts"

interface CompletionTimeChartProps {
  data: RacingTorrent[]
  timeRange: string
}

export function CompletionTimeChart({ data, timeRange }: CompletionTimeChartProps) {
  const chartData = useMemo(() => {
    // Group data by time period based on timeRange
    const groupedData = new Map<string, { completionTimes: number[], count: number }>()

    data.forEach(torrent => {
      if (!torrent.completedOn || !torrent.completionTime) return

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
        key = format(date, "MMM dd")
      }

      if (!groupedData.has(key)) {
        groupedData.set(key, { completionTimes: [], count: 0 })
      }

      const group = groupedData.get(key)!
      group.completionTimes.push(torrent.completionTime)
      group.count++
    })

    // Calculate averages and format for chart
    const chartData = Array.from(groupedData.entries()).map(([date, values]) => {
      const avgTime = values.completionTimes.reduce((a, b) => a + b, 0) / values.completionTimes.length
      const minTime = Math.min(...values.completionTimes)
      const maxTime = Math.max(...values.completionTimes)

      return {
        date,
        avgCompletionTime: Math.round(avgTime),
        minCompletionTime: minTime,
        maxCompletionTime: maxTime,
        count: values.count,
      }
    })

    return chartData.slice(-20) // Limit to last 20 data points
  }, [data, timeRange])

  const formatTime = (seconds: number) => {
    if (seconds < 60) return `${seconds}s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
    return `${Math.floor(seconds / 3600)}h`
  }

  const chartConfig = {
    avgCompletionTime: {
      label: "Avg Time",
      color: "var(--chart-1)",
    },
    minCompletionTime: {
      label: "Min Time",
      color: "var(--chart-2)",
    },
    maxCompletionTime: {
      label: "Max Time",
      color: "var(--chart-3)",
    },
  }

  return (
    <ChartContainer config={chartConfig} className="h-[300px] w-full">
      <AreaChart data={chartData}>
        <defs>
          <linearGradient id="colorAvg" x1="0" y1="0" x2="0" y2="1">
            <stop offset="5%" stopColor="var(--chart-1)" stopOpacity={0.8} />
            <stop offset="95%" stopColor="var(--chart-1)" stopOpacity={0.1} />
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
          tickFormatter={formatTime}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) => {
                if (typeof value === "number") {
                  return (
                    <div className="flex items-center justify-between gap-2">
                      <span>{name}:</span>
                      <span className="font-mono">{formatTime(value)}</span>
                    </div>
                  )
                }
                return null
              }}
            />
          }
        />
        <Area
          type="monotone"
          dataKey="avgCompletionTime"
          stroke="var(--chart-1)"
          fillOpacity={1}
          fill="url(#colorAvg)"
          strokeWidth={2}
        />
        <Line
          type="monotone"
          dataKey="minCompletionTime"
          stroke="var(--chart-2)"
          strokeDasharray="5 5"
          dot={false}
        />
        <Line
          type="monotone"
          dataKey="maxCompletionTime"
          stroke="var(--chart-3)"
          strokeDasharray="5 5"
          dot={false}
        />
      </AreaChart>
    </ChartContainer>
  )
}