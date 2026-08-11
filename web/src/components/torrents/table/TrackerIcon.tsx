/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { memo, useState } from "react"

const trackerIconSizeClasses = {
  xs: "h-3 w-3 text-[8px]",
  sm: "h-[14px] w-[14px] text-[9px]",
  md: "h-4 w-4 text-[10px]",
} as const

export type TrackerIconSize = keyof typeof trackerIconSizeClasses

export interface TrackerIconProps {
  title: string
  fallback: string
  src: string | null
  size?: TrackerIconSize
  className?: string
}

export const TrackerIcon = memo(({ title, fallback, src, size = "md", className }: TrackerIconProps) => {
  // Track the specific src that failed to load. Keying error state to the src
  // value (rather than resetting a boolean in an effect) means a new src renders
  // its image immediately, with no one-frame fallback flash.
  const [failedSrc, setFailedSrc] = useState<string | null>(null)

  return (
    <div className={cn("flex items-center justify-center", className)} title={title}>
      <div
        className={cn(
          "flex items-center justify-center rounded-sm border border-border/40 bg-muted font-medium uppercase leading-none select-none",
          trackerIconSizeClasses[size]
        )}
      >
        {src && failedSrc !== src ? (
          <img
            src={src}
            alt=""
            className="h-full w-full rounded-[2px] object-cover"
            loading="lazy"
            draggable={false}
            onError={() => setFailedSrc(src)}
          />
        ) : (
          <span aria-hidden="true">{fallback}</span>
        )}
      </div>
    </div>
  )
}, (prev, next) =>
  prev.title === next.title &&
  prev.fallback === next.fallback &&
  prev.src === next.src &&
  prev.size === next.size &&
  prev.className === next.className
)
