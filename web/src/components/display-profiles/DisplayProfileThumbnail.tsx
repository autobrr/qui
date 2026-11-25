/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"

export type DisplayProfileDensity = "table" | "dense" | "stacked"

const ROW_COUNT: Record<DisplayProfileDensity, number> = { table: 5, dense: 7, stacked: 3 }

const LIST_TOP = 22
const LIST_BOTTOM = 96
const SIDEBAR_ITEMS = [[18, 16], [26, 12], [34, 19], [46, 14], [54, 18], [62, 10]] as const

interface DisplayProfileThumbnailProps {
  density: DisplayProfileDensity
  filterSidebarOpen: boolean
  className?: string
}

export function DisplayProfileThumbnail({ density, filterSidebarOpen, className }: DisplayProfileThumbnailProps) {
  const listX = filterSidebarOpen ? 44 : 6
  const listWidth = 154 - listX
  const rowHeight = (LIST_BOTTOM - LIST_TOP) / ROW_COUNT[density]

  return (
    <span aria-hidden="true" className={cn("block aspect-[16/10] w-full overflow-hidden rounded-md border", className)}>
      <svg viewBox="0 0 160 100" className="block size-full">
        <rect width="160" height="100" style={{ fill: "var(--background)" }} />

        <rect x={listX} y="7" width="44" height="7" rx="3.5" opacity="0.35" style={{ fill: "var(--muted-foreground)" }} />
        <rect x="131" y="6" width="10" height="9" rx="2.5" opacity="0.35" style={{ fill: "var(--muted-foreground)" }} />
        <rect x="144" y="6" width="10" height="9" rx="2.5" opacity="0.8" style={{ fill: "var(--foreground)" }} />

        {filterSidebarOpen && (
          <g>
            <rect width="38" height="100" style={{ fill: "var(--sidebar)" }} />
            <rect x="38" width="0.75" height="100" style={{ fill: "var(--border)" }} />
            <rect x="5" y="8" width="18" height="4" rx="2" opacity="0.7" style={{ fill: "var(--foreground)" }} />
            {SIDEBAR_ITEMS.map(([y, width]) => (
              <g key={y}>
                <rect x="6" y={y} width="4" height="4" rx="1" opacity="0.5" style={{ fill: "var(--muted-foreground)" }} />
                <rect x="13" y={y} width={width} height="4" rx="2" opacity="0.55" style={{ fill: "var(--muted-foreground)" }} />
              </g>
            ))}
          </g>
        )}

        {Array.from({ length: ROW_COUNT[density] }, (_, index) => {
          const y = LIST_TOP + index * rowHeight
          if (density === "stacked") {
            const cardHeight = rowHeight - 3
            return (
              <g key={index}>
                <rect x={listX} y={y} width={listWidth} height={cardHeight} rx="3" strokeWidth="0.75" style={{ fill: "var(--card)", stroke: "var(--border)" }} />
                <rect x={listX + 4} y={y + 4} width={listWidth * 0.55} height="3.5" rx="1.75" opacity="0.85" style={{ fill: "var(--foreground)" }} />
                <rect x={listX + 4} y={y + 10} width={listWidth * 0.3} height="3" rx="1.5" opacity="0.55" style={{ fill: "var(--muted-foreground)" }} />
                <rect x={listX + 4} y={y + cardHeight - 5} width={listWidth - 8} height="2" rx="1" opacity="0.25" style={{ fill: "var(--muted-foreground)" }} />
                <rect x={listX + 4} y={y + cardHeight - 5} width={(listWidth - 8) * [0.8, 0.45, 1][index]} height="2" rx="1" opacity="0.7" style={{ fill: "var(--foreground)" }} />
              </g>
            )
          }
          const bar = Math.min(3.5, rowHeight * 0.42)
          const middle = y + rowHeight / 2 - bar / 2
          return (
            <g key={index}>
              {index === 1 && <rect x={listX - 1} y={y + 0.5} width={listWidth + 2} height={rowHeight - 1} rx="1.5" opacity="0.18" style={{ fill: "var(--muted-foreground)" }} />}
              <rect x={listX + 2} y={middle} width={listWidth * 0.42} height={bar} rx={bar / 2} opacity="0.8" style={{ fill: "var(--foreground)" }} />
              <rect x={listX + listWidth * 0.5} y={middle} width={listWidth * 0.18} height={bar} rx={bar / 2} opacity="0.7" style={{ fill: "var(--muted-foreground)" }} />
              <rect x={listX + listWidth * 0.74} y={middle} width={listWidth * 0.22} height={bar} rx={bar / 2} opacity="0.5" style={{ fill: "var(--muted-foreground)" }} />
              {index < ROW_COUNT[density] - 1 && <rect x={listX} y={y + rowHeight - 0.25} width={listWidth} height="0.5" style={{ fill: "var(--border)" }} />}
            </g>
          )
        })}
      </svg>
    </span>
  )
}
