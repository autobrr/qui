/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { apiRequest } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"

export type ShareState = "active" | "ending" | "ended"

export type ShareTrackerKind = "own" | "outside"

export interface SharePeer {
  address: string
  progress: number
  seenAt: string
}

export interface Share {
  id: number
  instanceId: number
  hash: string
  name: string
  size: number
  tracker: ShareTrackerKind
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
  state: ShareState
  endReason: string
  endReasonCode: string
  createdAt: string
  expiresAt: string
  endedAt: string | null
  peers: SharePeer[]
  progress: number
  downloaded: number
  uploaded: number
}

export interface StartSharesRequest {
  instanceId: number
  hashes: string[]
  days: number
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
}

export interface StartSharesResponse {
  error?: string
  shares: Share[]
  refused: Array<{ hash: string; reason: string }>
}

export interface ShareDefaults {
  days: number
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
}

export interface ShareSettings {
  port: number
  publicAddress: string
  trustedProxies: string[]
  trackerUrl: string
  maxActive: number
  peersPerShare: number
  defaults: ShareDefaults
  trackerInUse: ShareTrackerKind | ""
}

export const sharesQueryKey = ["shares"] as const
export const shareSettingsQueryKey = ["shares", "settings"] as const

export function listShares(): Promise<Share[]> {
  return apiRequest<Share[]>("/shares")
}

export function startShares(request: StartSharesRequest): Promise<StartSharesResponse> {
  return apiRequest<StartSharesResponse>("/shares", { method: "POST", body: JSON.stringify(request) })
}

export function stopShare(id: number): Promise<void> {
  return apiRequest<void>(`/shares/${id}/stop`, { method: "POST" })
}

export function unlockShare(id: number): Promise<void> {
  return apiRequest<void>(`/shares/${id}/unlock`, { method: "POST" })
}

export function removeShare(id: number): Promise<void> {
  return apiRequest<void>(`/shares/${id}`, { method: "DELETE" })
}

export function getShareSettings(): Promise<ShareSettings> {
  return apiRequest<ShareSettings>("/shares/settings")
}

export function saveShareSettings(settings: ShareSettings): Promise<ShareSettings> {
  return apiRequest<ShareSettings>("/shares/settings", { method: "PUT", body: JSON.stringify(settings) })
}

export function shareTorrentUrl(id: number): string {
  return withBasePath(`/api/shares/${id}/torrent`)
}
