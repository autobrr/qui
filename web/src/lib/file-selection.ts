/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Minimal shape shared by the Content tab's flat-row lists (both the desktop
// TorrentFileTable and the vertical TorrentFileTree). Only the file index is
// needed for range selection.
export interface RangeRow {
  node: { file?: { index: number } }
}

// Collects TorrentFile indices for every FILE row between two positions
// (inclusive) in an ordered, visible flat-row list. Folder rows contribute
// nothing directly; their visible child file rows are already part of the list.
// Used by the Content tab's shift+click range selection.
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
