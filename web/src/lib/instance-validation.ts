/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { InstanceReannounceSettings } from "@/types"
import { z } from "zod"

export const DEFAULT_REANNOUNCE_SETTINGS: InstanceReannounceSettings = {
  enabled: false,
  initialWaitSeconds: 15,
  reannounceIntervalSeconds: 7,
  maxAgeSeconds: 600,
  maxRetries: 50,
  aggressive: false,
  monitorAll: false,
  healthFocusTrackers: [],
  healthIgnoreTrackers: ["sptracker.cc"],
  excludeCategories: false,
  categories: [],
  excludeTags: false,
  tags: [],
  excludeTrackers: false,
  trackers: [],
}

// URL validation schema for instance host URLs
export const instanceUrlSchema = z
  .string()
  .min(1, "URL is required")
  .transform((value) => {
    return value.includes("://") ? value : `http://${value}`
  })
  .refine((url) => {
    try {
      new URL(url)
      return true
    } catch {
      return false
    }
  }, "Please enter a valid URL")
  .refine((url) => {
    const parsed = new URL(url)
    return parsed.protocol === "http:" || parsed.protocol === "https:"
  }, "Only HTTP and HTTPS protocols are supported")
  .refine((url) => {
    const parsed = new URL(url)
    const hostname = parsed.hostname

    // Validate each octet is 0-255
    const isIPv4 = /^(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)$/.test(hostname)
    // new URL() returns unbracketed IPv6 addresses (e.g., "::1" not "[::1]")
    const isIPv6 = hostname.includes(":") && !isIPv4

    if (isIPv4 || isIPv6) {
      // default ports such as 80 and 443 are omitted from the result of new URL()
      const hasExplicitPort = url.match(/:(\d+)(?:[/?#]|$)/)
      if (!hasExplicitPort) {
        return false
      }
    }

    return true
  }, "Port is required when using an IP address (e.g., :8080)")
