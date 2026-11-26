/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent, TorrentResponse } from "@/types"
import { getLinuxIsoName, getLinuxSavePath } from "./incognito"

type HashSource = {
  hash?: string | null
  infohash_v1?: string | null
  infohash_v2?: string | null
}

export function normalizeTorrentHash(value?: string | null): string {
  if (!value) return ""
  const trimmed = value.trim()
  return trimmed.length > 0 ? trimmed : ""
}

function toHashSource(source?: HashSource | null): HashSource | undefined {
  if (!source) return undefined

  const { hash, infohash_v1, infohash_v2 } = source
  const hasValue = [hash, infohash_v1, infohash_v2].some(value => normalizeTorrentHash(value) !== "")
  if (!hasValue) return undefined

  return { hash, infohash_v1, infohash_v2 }
}

function deriveLegacyInfohashV1(source?: HashSource | null): string | undefined {
  if (!source) return undefined

  const candidate = normalizeTorrentHash(source.hash)
  if (!candidate) return undefined

  const v2 = normalizeTorrentHash(source.infohash_v2)
  if (v2 && v2 === candidate) return undefined

  return candidate
}

export function resolveTorrentHashes(primary?: HashSource | null, fallback?: HashSource | null) {
  const primarySource = toHashSource(primary)
  const fallbackSource = toHashSource(fallback)

  const infohashV1 = [
    primarySource?.infohash_v1,
    fallbackSource?.infohash_v1,
    deriveLegacyInfohashV1(primarySource),
    deriveLegacyInfohashV1(fallbackSource),
  ].map(normalizeTorrentHash).find(Boolean) || ""

  const infohashV2 = [
    primarySource?.infohash_v2,
    fallbackSource?.infohash_v2,
  ].map(normalizeTorrentHash).find(Boolean) || ""

  const canonicalHash =
    infohashV1 ||
    infohashV2 ||
    normalizeTorrentHash(primarySource?.hash) ||
    normalizeTorrentHash(fallbackSource?.hash)

  return {
    infohashV1,
    infohashV2,
    canonicalHash,
  }
}

export function getTorrentDisplayHash(primary?: HashSource | null, fallback?: HashSource | null): string {
  return resolveTorrentHashes(primary, fallback).canonicalHash
}

export function getTorrentFullPaths(torrents: Torrent[], incognitoMode: boolean): string[] {
  return torrents
    .map(t => incognitoMode ? `${getLinuxSavePath(t.hash)}/${getLinuxIsoName(t.hash)}` : t.content_path.trim())
    .filter(Boolean)
}

export function parseTorrentTags(tags?: string | null): string[] {
  if (!tags) return []
  return tags.split(",").map(tag => tag.trim()).filter(Boolean)
}

export function torrentHasTag(torrent: Torrent, tag: string): boolean {
  const normalized = tag.trim()
  if (!normalized) return false
  return parseTorrentTags(torrent.tags).includes(normalized)
}

export function anyTorrentHasTag(torrents: Torrent[], tag: string): boolean {
  if (torrents.length === 0) return false
  return torrents.some(torrent => torrentHasTag(torrent, tag))
}

export function getTorrentHashesWithTag(torrents: Torrent[], tag: string): string[] {
  if (torrents.length === 0) return []
  return torrents.filter(torrent => torrentHasTag(torrent, tag)).map(torrent => torrent.hash)
}

export function getCommonCategory(torrents: Torrent[]): string {
  if (torrents.length === 0) return ""
  if (torrents.length === 1) return torrents[0].category || ""

  const firstCategory = torrents[0].category || ""

  for (let i = 1; i < torrents.length; i++) {
    if ((torrents[i].category || "") !== firstCategory) {
      return ""
    }
  }

  return firstCategory
}

export function getCommonSavePath(torrents: Torrent[]): string {
  if (torrents.length === 0) return ""
  if (torrents.length === 1) return torrents[0].save_path || ""

  const firstPath = torrents[0].save_path || ""

  for (let i = 1; i < torrents.length; i++) {
    if ((torrents[i].save_path || "") !== firstPath) {
      return ""
    }
  }

  return firstPath
}

export function getTotalSize(torrents: Torrent[]): number {
  if (torrents.length === 0) return 0

  return torrents.reduce((total, torrent) => total + (torrent.size || 0), 0)
}

export function getToggleSelectionState(values: boolean[], stateUnknown: boolean): { allEnabled: boolean; mixed: boolean } {
  return {
    allEnabled: values.length > 0 && values.every(Boolean),
    mixed: stateUnknown || values.some(value => value !== values[0]),
  }
}

export function resolveStreamRow(previous: Torrent | null, data: Pick<TorrentResponse, "torrents" | "total">): Torrent | null {
  const next = data.torrents?.[0]
  if (next) {
    return next
  }
  return data.total === 0 ? null : previous
}
