/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type ArrInstanceType = "sonarr" | "radarr"

export type ArrTestStatus = "unknown" | "ok" | "error"

export interface ArrInstance {
  id: number
  type: ArrInstanceType
  name: string
  base_url: string
  enabled: boolean
  priority: number
  timeout_seconds: number
  last_test_at?: string
  last_test_status: ArrTestStatus
  last_test_error?: string
  created_at: string
  updated_at: string
}

export interface ArrInstanceFormData {
  type: ArrInstanceType
  name: string
  base_url: string
  api_key: string
  enabled?: boolean
  priority?: number
  timeout_seconds?: number
}

export interface ArrInstanceUpdateData {
  name?: string
  base_url?: string
  api_key?: string
  enabled?: boolean
  priority?: number
  timeout_seconds?: number
}

export interface ArrTestConnectionRequest {
  type: ArrInstanceType
  base_url: string
  api_key: string
}

export interface ArrTestResponse {
  success: boolean
  error?: string
}

export interface ArrResolveRequest {
  title: string
  content_type: "movie" | "tv"
}

export interface ArrExternalIDs {
  imdb_id?: string
  tmdb_id?: number
  tvdb_id?: number
  tvmaze_id?: number
}

export interface ArrIDCacheEntry {
  id: number
  title_hash: string
  content_type: string
  arr_instance_id?: number
  external_ids: ArrExternalIDs
  is_negative: boolean
  cached_at: string
  expires_at: string
}

export interface ArrInstanceResult {
  instance_id: number
  instance_name: string
  instance_type: string
  ids?: ArrExternalIDs
  error?: string
}

export interface ArrResolveResponse {
  title: string
  title_hash: string
  content_type: "movie" | "tv"
  cache_hit: boolean
  cache_entry?: ArrIDCacheEntry
  instances_available: number
  instance_results?: ArrInstanceResult[]
  error?: string
}
