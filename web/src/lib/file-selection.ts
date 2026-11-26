/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface RangeRow {
  node: { file?: { index: number } }
}

export function collectFileIndicesInRange(
  rows: ReadonlyArray<RangeRow>,
  anchorPos: number,
  currentPos: number
): number[] {
  const start = Math.min(anchorPos, currentPos)
  const end = Math.max(anchorPos, currentPos)
  const indices: number[] = []
  for (let i = start; i <= end; i++) {
    const file = rows[i]?.node.file
    if (file) {
      indices.push(file.index)
    }
  }
  return indices
}
