/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE (throwaway): a small picture of the torrent list as a profile shows
// it, drawn like ThemeThumbnail in the colors of the theme in use: the filter
// sidebar and the details panel when they are open, and as many rows as fit at
// the row density. Neutral theme colors only: the profile color stays on its icon.

import { cn } from "@/lib/utils"
import type { DisplayProfile } from "./store"

const ROWS = { Table: 5, Dense: 7, Stacked: 3 } as const

export function ProfileThumbnail({ profile, className }: { profile: DisplayProfile; className?: string }) {
  const filtersOpen = profile.sidebars.filters === "open"
  const detailsOpen = profile.sidebars.details === "open"

  const listX = filtersOpen ? 42 : 4
  const listW = 156 - listX
  const listTop = 22
  const listBottom = detailsOpen ? 66 : 96
  const listH = listBottom - listTop
  const rows = ROWS[profile.rowDensity]
  const rowH = listH / rows

  return (
    <span aria-hidden="true" className={cn("block aspect-[16/10] w-full overflow-hidden rounded-md border", className)}>
      <svg viewBox="0 0 160 100" className="block size-full">
        <rect width="160" height="100" style={{ fill: "var(--background)" }} />

        {/* toolbar: search field and the column buttons */}
        <rect x={listX} y="7" width="44" height="7" rx="3.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.35" />
        <rect x="132" y="6" width="10" height="9" rx="2.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.35" />
        <rect x="145" y="6" width="11" height="9" rx="2.5" style={{ fill: "var(--foreground)" }} opacity="0.8" />

        {filtersOpen && (
          <>
            <rect width="38" height="100" style={{ fill: "var(--sidebar)" }} />
            <rect x="38" width="0.75" height="100" style={{ fill: "var(--border)" }} />
            <rect x="5" y="8" width="18" height="4" rx="2" style={{ fill: "var(--foreground)" }} opacity="0.7" />
            {[18, 26, 34, 46, 54, 62].map((y, i) => (
              <g key={y}>
                <rect x="6" y={y} width="4" height="4" rx="1" style={{ fill: "var(--muted-foreground)" }} opacity="0.5" />
                <rect x="13" y={y} width={[16, 12, 19, 14, 18, 10][i]} height="4" rx="2" style={{ fill: "var(--muted-foreground)" }} opacity="0.55" />
              </g>
            ))}
          </>
        )}

        {Array.from({ length: rows }, (_, i) => {
          const y = listTop + i * rowH
          const accent = i === 1
          if (profile.rowDensity === "Stacked") {
            const cardH = rowH - 3
            return (
              <g key={i}>
                <rect x={listX} y={y} width={listW} height={cardH} rx="3" style={{ fill: "var(--card)", stroke: "var(--border)" }} strokeWidth="0.75" />
                <rect x={listX + 4} y={y + 3} width={listW * 0.55} height="3.5" rx="1.75" style={{ fill: "var(--foreground)" }} opacity="0.85" />
                <rect x={listX + 4} y={y + 9} width={listW * 0.3} height="3" rx="1.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.55" />
                <rect x={listX + 4} y={y + cardH - 4.5} width={listW - 8} height="2" rx="1" style={{ fill: "var(--muted-foreground)" }} opacity="0.25" />
                <rect x={listX + 4} y={y + cardH - 4.5} width={(listW - 8) * [0.8, 0.45, 1][i]} height="2" rx="1" style={{ fill: "var(--foreground)" }} opacity="0.7" />
              </g>
            )
          }
          const bar = Math.min(3.5, rowH * 0.42)
          const mid = y + rowH / 2
          return (
            <g key={i}>
              {accent && <rect x={listX - 1} y={y + 0.5} width={listW + 2} height={rowH - 1} rx="1.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.18" />}
              <rect x={listX + 2} y={mid - bar / 2} width={listW * 0.42} height={bar} rx={bar / 2} style={{ fill: "var(--foreground)" }} opacity="0.8" />
              <rect x={listX + listW * 0.5} y={mid - bar / 2} width={listW * 0.18} height={bar} rx={bar / 2} style={{ fill: "var(--muted-foreground)" }} opacity="0.7" />
              <rect x={listX + listW * 0.74} y={mid - bar / 2} width={listW * 0.22} height={bar} rx={bar / 2} style={{ fill: "var(--muted-foreground)" }} opacity="0.5" />
              {i < rows - 1 && <rect x={listX} y={y + rowH - 0.25} width={listW} height="0.5" style={{ fill: "var(--border)" }} />}
            </g>
          )
        })}

        {detailsOpen && (
          <>
            <rect x={filtersOpen ? 38.75 : 0} y="70" width={filtersOpen ? 121.25 : 160} height="30" style={{ fill: "var(--card)" }} />
            <rect x={filtersOpen ? 38.75 : 0} y="70" width={filtersOpen ? 121.25 : 160} height="0.75" style={{ fill: "var(--border)" }} />
            <rect x={listX} y="74" width="14" height="4" rx="2" style={{ fill: "var(--foreground)" }} opacity="0.8" />
            <rect x={listX + 18} y="74" width="12" height="4" rx="2" style={{ fill: "var(--muted-foreground)" }} opacity="0.5" />
            <rect x={listX + 34} y="74" width="12" height="4" rx="2" style={{ fill: "var(--muted-foreground)" }} opacity="0.5" />
            <rect x={listX} y="84" width={listW * 0.6} height="3" rx="1.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.45" />
            <rect x={listX} y="90" width={listW * 0.4} height="3" rx="1.5" style={{ fill: "var(--muted-foreground)" }} opacity="0.45" />
          </>
        )}
      </svg>
    </span>
  )
}
