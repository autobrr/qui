/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { extractTrackerHost } from "@/lib/tracker-customizations"

export function resolveTrackerIconSrc(
  trackerIcons: Record<string, string> | undefined,
  ...candidates: Array<string | undefined | null>
): string | null {
  if (!trackerIcons) {
    return null
  }

  for (const candidate of candidates) {
    if (!candidate) {
      continue
    }

    const key = extractTrackerHost(candidate)
    if (!key) {
      continue
    }

    if (trackerIcons[key]) {
      return trackerIcons[key]
    }

    // Try the www/bare hostname variant as a fallback.
    const alias = key.startsWith("www.") ? key.slice(4) : `www.${key}`
    if (trackerIcons[alias]) {
      return trackerIcons[alias]
    }
  }

  return null
}

export function pickTrackerIconDomain(
  trackerIcons: Record<string, string> | undefined,
  domains: string[],
  fallback?: string
): string {
  const candidates = [...domains, fallback].filter(Boolean) as string[]
  if (candidates.length === 0) {
    return ""
  }

  for (const candidate of candidates) {
    if (resolveTrackerIconSrc(trackerIcons, candidate)) {
      return extractTrackerHost(candidate)
    }
  }

  // Fall back to the first parseable host/domain.
  for (const candidate of candidates) {
    const key = extractTrackerHost(candidate)
    if (key) {
      return key
    }
  }

  return ""
}
