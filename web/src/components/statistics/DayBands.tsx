/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import type { DayBand, HourAxis } from "@/lib/statistics"
import { DefaultZIndexes, ReferenceArea, ZIndexLayer } from "recharts"

const LABEL_PADDING = 6

interface AreaBox {
  x?: number
  y?: number
  width?: number
  height?: number
}

interface DayBandShapeProps extends AreaBox {
  band: DayBand
  label: string
  centered: boolean
}

function DayBandShape({ x = 0, y = 0, width = 0, height = 0, band, label, centered }: DayBandShapeProps) {
  const hideWhenWiderThanBand = (text: SVGTextElement | null) => {
    if (text) {
      text.style.visibility = text.getComputedTextLength() + 2 * LABEL_PADDING > width ? "hidden" : ""
    }
  }
  return (
    <g>
      {band.shaded && <rect x={x} y={y} width={width} height={height} fill="var(--muted-foreground)" fillOpacity={0.12} />}
      {band.midnightLine && <line x1={x} x2={x} y1={y} y2={y + height} stroke="var(--muted-foreground)" />}
      <ZIndexLayer zIndex={DefaultZIndexes.label}>
        <text
          ref={hideWhenWiderThanBand}
          x={centered ? x + width / 2 : x + LABEL_PADDING}
          y={y + 12}
          textAnchor={centered ? "middle" : "start"}
          fill="var(--foreground)"
          stroke="var(--card)"
          strokeWidth={1.5}
          strokeLinejoin="round"
          paintOrder="stroke"
          fontSize={11}
          fontWeight={600}
        >
          {label}
        </text>
      </ZIndexLayer>
    </g>
  )
}

export function DayBands({ axis }: { axis: HourAxis }) {
  const { formatDayName } = useBucketFormat()
  return axis.bands.map(band => (
    <ReferenceArea
      key={band.day}
      x1={band.first}
      x2={band.last}
      ifOverflow="visible"
      shape={(box: AreaBox) => (
        <DayBandShape {...box} band={band} label={formatDayName(band.day, !(axis.manyDays && axis.narrow))} centered={axis.manyDays} />
      )}
    />
  ))
}
