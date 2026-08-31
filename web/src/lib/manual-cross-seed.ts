/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Manual match (cross-seed) helpers shared by the add-torrent dialog gate and
// the manual cross-seed dialog.

export interface ManualCrossSeedGateInput {
  fileCount: number
  urlText: string
}

// The Manual match flow applies to exactly one uploaded .torrent file.
// Magnets, URLs, and multi-file adds are excluded; the caller gates on the
// file tab.
export function canOfferManualCrossSeed(input: ManualCrossSeedGateInput): boolean {
  return input.fileCount === 1 && input.urlText.trim() === ""
}

export function overlapPercent(overlapFraction: number): number {
  return Math.round(Math.min(Math.max(overlapFraction, 0), 1) * 100)
}

export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      // Strip the data URL prefix ("data:...;base64,").
      const separator = result.indexOf(",")
      resolve(separator >= 0 ? result.slice(separator + 1) : result)
    }
    reader.onerror = () => reject(reader.error ?? new Error("failed to read file"))
    reader.readAsDataURL(file)
  })
}
