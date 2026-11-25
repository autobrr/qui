/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TrackerCustomization } from "@/types"

export interface TrackerDisplayInfo {
  displayName: string
  primaryDomain: string
  isCustomized: boolean
}

export type TrackerCustomizationLookup = Map<string, TrackerDisplayInfo>

export function extractTrackerHost(tracker: string | undefined | null): string {
  if (!tracker) {
    return ""
  }

  const trimmed = tracker.trim()
  if (!trimmed) {
    return ""
  }

  const urlString = trimmed.includes("://") ? trimmed : `//${trimmed}`
  try {
    const url = new URL(urlString, "http://placeholder")
    return url.hostname.toLowerCase()
  } catch {
    return hostBeforePathAndPort(trimmed)
  }
}

function hostBeforePathAndPort(tracker: string): string {
  let host = tracker.toLowerCase()
  const pathIndex = host.indexOf("/")
  if (pathIndex !== -1) {
    host = host.substring(0, pathIndex)
  }
  const portIndex = host.lastIndexOf(":")
  if (portIndex !== -1) {
    host = host.substring(0, portIndex)
  }
  return host
}

export function buildTrackerCustomizationLookup(
  customizations?: TrackerCustomization[] | null
): TrackerCustomizationLookup {
  const lookup = new Map<string, TrackerDisplayInfo>()

  if (!customizations) {
    return lookup
  }

  for (const custom of customizations) {
    const domains = custom.domains
    if (!domains || domains.length === 0) {
      continue
    }

    const primaryDomain = domains[0].toLowerCase()

    for (const domain of domains) {
      const lowerDomain = domain.toLowerCase()
      lookup.set(lowerDomain, {
        displayName: custom.displayName,
        primaryDomain,
        isCustomized: true,
      })
    }
  }

  return lookup
}

export function resolveTrackerDisplay(
  host: string | undefined | null,
  lookup: TrackerCustomizationLookup
): TrackerDisplayInfo {
  if (!host) {
    return {
      displayName: "",
      primaryDomain: "",
      isCustomized: false,
    }
  }

  const lowerHost = host.toLowerCase()
  const customization = lookup.get(lowerHost)

  if (customization) {
    return customization
  }

  return {
    displayName: lowerHost,
    primaryDomain: lowerHost,
    isCustomized: false,
  }
}

export function getTrackerCustomizationsCacheKey(
  customizations?: TrackerCustomization[] | null
): string {
  if (!customizations || customizations.length === 0) {
    return ""
  }

  const sorted = [...customizations].sort((a, b) => a.id - b.id)
  return sorted.map((c) => `${c.id}:${c.updatedAt}`).join("|")
}
