/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TrackerCustomization } from "@/types"

/**
 * Resolved tracker display information
 */
export interface TrackerDisplayInfo {
  /** Display name (custom name if configured, otherwise the hostname) */
  displayName: string
  /** Primary domain for icon lookup (first domain in customization, or the hostname) */
  primaryDomain: string
  /** Whether this tracker has a custom display name */
  isCustomized: boolean
}

/**
 * Tracker customization lookup map
 * Maps domain (lowercase) -> display info
 */
export type TrackerCustomizationLookup = Map<string, TrackerDisplayInfo>

/**
 * Extracts the hostname from a tracker URL or returns the raw value if parsing fails.
 * Handles full URLs (http://...), scheme-less URLs (tracker.com/announce),
 * and URLs with ports (tracker.com:443/announce).
 * Always returns lowercase.
 * @param tracker - The tracker URL or hostname
 * @returns The lowercase hostname
 */
export function extractTrackerHost(tracker: string | undefined | null): string {
  if (!tracker) {
    return ""
  }

  const trimmed = tracker.trim()
  if (!trimmed) {
    return ""
  }

  // Try to parse as URL - prepend "//" if no scheme to enable URL parsing
  const urlString = trimmed.includes("://") ? trimmed : `//${trimmed}`
  try {
    const url = new URL(urlString, "http://placeholder")
    return url.hostname.toLowerCase()
  } catch {
    // Fall through - extract manually
  }

  // Manual extraction: strip path and port
  let host = trimmed.toLowerCase()
  // Remove path
  const pathIndex = host.indexOf("/")
  if (pathIndex !== -1) {
    host = host.substring(0, pathIndex)
  }
  // Remove port
  const portIndex = host.lastIndexOf(":")
  if (portIndex !== -1) {
    host = host.substring(0, portIndex)
  }
  return host
}

/**
 * Builds a lookup map from tracker customizations for efficient display name resolution.
 * Maps every domain (lowercase) to its display name and primary domain.
 *
 * @param customizations - Array of tracker customizations
 * @returns Map of domain -> { displayName, primaryDomain, isCustomized }
 */
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

    // Primary domain is the first one in the list
    const primaryDomain = domains[0].toLowerCase()

    // Map all domains to the same display info
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

/**
 * Resolves the display information for a tracker hostname.
 * Returns custom display name and primary domain if configured, otherwise falls back to the hostname.
 *
 * @param host - The tracker hostname (should be lowercase, but will be normalized)
 * @param lookup - The customization lookup map
 * @returns Display info with displayName, primaryDomain, and isCustomized flag
 */
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

  // No customization found - return the host as both display name and primary domain
  return {
    displayName: lowerHost,
    primaryDomain: lowerHost,
    isCustomized: false,
  }
}

/**
 * Convenience function that extracts host from a tracker URL and resolves its display info.
 *
 * @param tracker - The tracker URL or hostname
 * @param lookup - The customization lookup map
 * @returns Display info with displayName, primaryDomain, and isCustomized flag
 */
export function resolveTrackerDisplayFromURL(
  tracker: string | undefined | null,
  lookup: TrackerCustomizationLookup
): TrackerDisplayInfo {
  const host = extractTrackerHost(tracker)
  return resolveTrackerDisplay(host, lookup)
}

/**
 * Generates a stable cache key from tracker customizations.
 * This key changes whenever any customization is added, removed, or modified.
 *
 * @param customizations - Array of tracker customizations
 * @returns A string key that changes when customizations change
 */
export function getTrackerCustomizationsCacheKey(
  customizations?: TrackerCustomization[] | null
): string {
  if (!customizations || customizations.length === 0) {
    return ""
  }

  // Build a key from id:updatedAt pairs, sorted by id for stability
  const sorted = [...customizations].sort((a, b) => a.id - b.id)
  return sorted.map((c) => `${c.id}:${c.updatedAt}`).join("|")
}
