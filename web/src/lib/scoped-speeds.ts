/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Resolve the download/upload speeds shown in the torrent list footer.
 *
 * Single-instance views read live rates from the instance's serverState. Aggregate
 * (all-instances / cross-instance) views have no single serverState, so they must
 * use the aggregated stats totals instead - otherwise the footer reads 0 even while
 * torrents are actively transferring.
 */
export function resolveFooterSpeeds(
  isAggregate: boolean,
  stats: { totalDownloadSpeed?: number; totalUploadSpeed?: number } | null | undefined,
  serverState: { dl_info_speed?: number; up_info_speed?: number } | null | undefined
): { downloadSpeed: number; uploadSpeed: number } {
  if (isAggregate) {
    return {
      downloadSpeed: stats?.totalDownloadSpeed ?? 0,
      uploadSpeed: stats?.totalUploadSpeed ?? 0,
    }
  }

  return {
    downloadSpeed: serverState?.dl_info_speed ?? 0,
    uploadSpeed: serverState?.up_info_speed ?? 0,
  }
}
