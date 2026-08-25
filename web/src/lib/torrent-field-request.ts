/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentFilters } from "@/types"

import type { TorrentActionTarget } from "./torrent-action-targets"

// Fields the context menu copies on demand. The /torrents/field endpoint also
// accepts "tags" (bulk tag baselines); api.ts keeps that wider union.
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

// Builds the /torrents/field request body. An explicit selection resolves by
// hash on a single instance and by instance/hash targets on the unified view;
// without a selection the request carries the active filter scope (select-all).
export function buildTorrentFieldRequest(scope: TorrentFieldScope, selection?: TorrentFieldSelection) {
  const { isCrossInstance, ...rest } = scope
  if (selection) {
    return isCrossInstance
      ? { targets: selection.targets, instanceIds: scope.instanceIds }
      : { hashes: selection.hashes }
  }

  return isCrossInstance ? rest : { ...rest, excludeTargets: undefined, instanceIds: undefined }
}
