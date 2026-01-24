/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useEffect, useState } from "react"
import { resolveTrackerIconSrc } from "@/lib/tracker-icons"

interface TrackerIconImageProps {
  tracker: string
  trackerIcons?: Record<string, string>
}

export const TrackerIconImage = memo(({ tracker, trackerIcons }: TrackerIconImageProps) => {
  const [hasError, setHasError] = useState(false)

  useEffect(() => {
    setHasError(false)
  }, [tracker, trackerIcons])

  const trimmed = tracker.trim()
  const fallbackLetter = trimmed ? trimmed.charAt(0).toUpperCase() : "#"
  const src = resolveTrackerIconSrc(trackerIcons, trimmed)

  return (
    <div className="flex h-4 w-4 items-center justify-center rounded-sm border border-border/40 bg-muted text-[10px] font-medium uppercase leading-none shrink-0">
      {src && !hasError ? (
        <img
          src={src}
          alt=""
          className="h-full w-full rounded-[2px] object-cover"
          loading="lazy"
          draggable={false}
          onError={() => setHasError(true)}
        />
      ) : (
        <span aria-hidden="true">{fallbackLetter}</span>
      )}
    </div>
  )
})

TrackerIconImage.displayName = "TrackerIconImage"
