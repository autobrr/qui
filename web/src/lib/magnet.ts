/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function normalizeMagnetLink(input: unknown): string | null {
  if (typeof input !== "string") return null

  let value = input.trim()
  if (!value) return null

  // Some launch flows may double-encode the URL placeholder, so try decoding a couple times.
  for (let i = 0; i < 2; i++) {
    if (/^magnet:/i.test(value)) return value
    if (!/%[0-9a-f]{2}/i.test(value)) break

    try {
      const decoded = decodeURIComponent(value)
      if (decoded === value) break
      value = decoded.trim()
    } catch {
      break
    }
  }

  return /^magnet:/i.test(value) ? value : null
}

export function extractMagnetFromTargetURL(targetURL: unknown): string | null {
  const direct = normalizeMagnetLink(targetURL)
  if (direct) return direct

  if (typeof targetURL !== "string" || !targetURL.trim()) return null

  try {
    const url = new URL(targetURL, window.location.origin)
    const candidate = url.searchParams.get("magnet") ?? url.searchParams.get("url")
    return normalizeMagnetLink(candidate)
  } catch {
    return null
  }
}

