/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ThemeThumbnailPalette } from "@/lib/theme-thumbnail"
import { cn } from "@/lib/utils"

interface ThemeThumbnailProps {
  palette: ThemeThumbnailPalette
  detail?: "full" | "compact"
  className?: string
}

const COMPACT_LAYERS = [
  ["primary", "16% 8%", "4.8% 12.8%"],
  ["primary", "40% 8%", "60% 78%"],
  ["foreground", "30% 6%", "42.9% 12.8%"],
  ["card", "64% 60%", "83.3% 75%"],
  ["sidebar", "24% 100%", "0 0"],
] as const

export function ThemeThumbnail({ palette, detail = "full", className }: ThemeThumbnailProps) {
  const frame = cn("block aspect-[16/10] w-full overflow-hidden rounded-md border", className)

  if (detail === "compact") {
    const colors = {
      primary: palette.primary ?? palette.foreground ?? "var(--muted-foreground)",
      foreground: palette.foreground ?? "var(--foreground)",
      card: palette.card ?? palette.background ?? "var(--muted)",
      sidebar: palette.sidebar ?? palette.card ?? palette.background ?? "var(--muted)",
    }
    const style = palette.background
      ? {
        backgroundColor: palette.background,
        backgroundImage: COMPACT_LAYERS.map(([color]) => `linear-gradient(${colors[color]}, ${colors[color]})`).join(", "),
        backgroundSize: COMPACT_LAYERS.map(([, size]) => size).join(", "),
        backgroundPosition: COMPACT_LAYERS.map(([, , position]) => position).join(", "),
        backgroundRepeat: "no-repeat",
      }
      : {
        backgroundImage: `linear-gradient(to right, ${palette.primary ?? "var(--muted)"} 0 33.4%, ${palette.secondary ?? "var(--muted)"} 33.4% 66.7%, ${palette.accent ?? "var(--muted)"} 66.7%)`,
      }
    return <span aria-hidden="true" className={frame} style={style} />
  }

  if (!palette.background) {
    return (
      <span aria-hidden="true" className={cn(frame, "flex")}>
        {[palette.primary, palette.secondary, palette.accent].map((color, index) => (
          <span key={index} className="flex-1" style={{ backgroundColor: color ?? "var(--muted)" }} />
        ))}
      </span>
    )
  }

  const foreground = palette.foreground ?? "currentColor"
  const muted = palette.mutedForeground ?? foreground
  const card = palette.card ?? palette.background
  const border = palette.border ?? muted
  const primary = palette.primary ?? foreground

  return (
    <span aria-hidden="true" className={frame}>
      <svg viewBox="0 0 160 100" className="block size-full">
        <rect width="160" height="100" style={{ fill: palette.background }} />
        <rect width="38" height="100" style={{ fill: palette.sidebar ?? card }} />
        <rect x="38" width="0.75" height="100" style={{ fill: border }} />
        <rect x="4" y="9" width="30" height="11" rx="3" opacity="0.2" style={{ fill: primary }} />
        <rect x="8" y="12.5" width="18" height="4" rx="2" style={{ fill: primary }} />
        <rect x="7" y="25" width="19" height="4" rx="2" opacity="0.55" style={{ fill: muted }} />
        <rect x="7" y="35" width="22" height="4" rx="2" opacity="0.55" style={{ fill: muted }} />
        <rect x="7" y="45" width="15" height="4" rx="2" opacity="0.55" style={{ fill: muted }} />

        <rect x="48" y="11" width="44" height="6" rx="3" style={{ fill: foreground }} />
        <rect x="126" y="9" width="24" height="10" rx="3" style={{ fill: primary }} />

        <rect x="48.5" y="27.5" width="101" height="63" rx="5" style={{ fill: card, stroke: border }} />
        <rect x="52" y="33" width="94" height="14" rx="3" style={{ fill: palette.accent ?? card }} />
        <circle cx="58" cy="40" r="3" style={{ fill: primary }} />
        <rect x="66" y="38" width="48" height="4" rx="2" opacity="0.85" style={{ fill: foreground }} />
        <rect x="122" y="38" width="20" height="4" rx="2" opacity="0.55" style={{ fill: muted }} />
        <circle cx="58" cy="54" r="3" style={{ fill: palette.secondary ?? muted }} />
        <rect x="66" y="52" width="38" height="4" rx="2" opacity="0.85" style={{ fill: foreground }} />
        <rect x="122" y="52" width="20" height="4" rx="2" opacity="0.55" style={{ fill: muted }} />
        <rect x="56" y="72" width="86" height="5" rx="2.5" opacity="0.25" style={{ fill: muted }} />
        <rect x="56" y="72" width="54" height="5" rx="2.5" style={{ fill: primary }} />
      </svg>
    </span>
  )
}
