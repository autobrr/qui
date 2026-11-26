/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentFilters } from "@/types"

import type { TorrentActionTarget } from "./torrent-action-targets"

export type TorrentFieldName = "name" | "hash" | "full_path" | "magnet_uri"

export interface TorrentFieldScope {
  isCrossInstance: boolean
  instanceIds?: number[]
  sort?: string
  order?: "asc" | "desc"
  search?: string
  filters?: TorrentFilters
  excludeHashes?: string[]
  excludeTargets?: TorrentActionTarget[]
}

export interface TorrentFieldSelection {
  hashes: string[]
  targets: TorrentActionTarget[]
}

export function buildTorrentFieldRequest(scope: TorrentFieldScope, selection?: TorrentFieldSelection) {
  const { isCrossInstance, ...rest } = scope
  if (selection) {
    return isCrossInstance
      ? { targets: selection.targets, instanceIds: scope.instanceIds }
      : { hashes: selection.hashes }
  }

  return isCrossInstance ? rest : { ...rest, excludeTargets: undefined, instanceIds: undefined }
}
