/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentTableColumn } from "@/components/torrents/tanstackTableFeatures"

/** Display label for a column: its headerString meta, a string header, or the id. */
export function columnLabel(column: TorrentTableColumn): string {
  const meta = column.columnDef.meta as { headerString?: string } | undefined
  if (meta?.headerString) return meta.headerString
  if (typeof column.columnDef.header === "string") return column.columnDef.header
  return column.id
}
