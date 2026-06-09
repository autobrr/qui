/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// RSS Types

export interface RSSArticle {
  id: string
  date: string
  title: string
  author?: string
  description?: string
  torrentURL?: string
  link?: string
  isRead: boolean
}

export interface RSSFeed {
  uid: string
  url: string
  title?: string
  lastBuildDate?: string
  hasError: boolean
  isLoading: boolean
  articles?: RSSArticle[]
}

// Hierarchical structure returned by qBittorrent
export interface RSSItems {
  [key: string]: RSSFeed | RSSItems
}

// Helper type to distinguish feeds from folders
export function isRSSFeed(item: RSSFeed | RSSItems): item is RSSFeed {
  return "url" in item && typeof item.url === "string"
}

export interface RSSRuleTorrentParams {
  category?: string
  tags?: string[]
  save_path?: string
  download_path?: string
  content_layout?: string
  operating_mode?: string
  skip_checking?: boolean
  upload_limit?: number
  download_limit?: number
  seeding_time_limit?: number
  inactive_seeding_time_limit?: number
  share_limit_action?: string
  ratio_limit?: number
  stopped?: boolean
  stop_condition?: string
  use_auto_tmm?: boolean
  use_download_path?: boolean
  add_to_top_of_queue?: boolean
}

export interface RSSAutoDownloadRule {
  enabled: boolean
  priority: number
  useRegex: boolean
  mustContain: string
  mustNotContain: string
  episodeFilter?: string
  affectedFeeds: string[]
  lastMatch?: string
  ignoreDays: number
  smartFilter: boolean
  previouslyMatchedEpisodes?: string[]
  torrentParams?: RSSRuleTorrentParams
  // Legacy fields
  addPaused?: boolean | null
  savePath?: string
  assignedCategory?: string
  torrentContentLayout?: string
}

export type RSSRules = Record<string, RSSAutoDownloadRule>

export type RSSMatchingArticles = Record<string, string[]>

// Request types for RSS API
export interface AddRSSFolderRequest {
  path: string
}

export interface AddRSSFeedRequest {
  url: string
  path?: string
}

export interface SetRSSFeedURLRequest {
  path: string
  url: string
}

export interface MoveRSSItemRequest {
  itemPath: string
  destPath?: string // Optional: empty moves item to root
}

export interface RemoveRSSItemRequest {
  path: string
}

export interface RefreshRSSItemRequest {
  itemPath: string
}

export interface MarkRSSAsReadRequest {
  itemPath: string
  articleId?: string
}

export interface SetRSSRuleRequest {
  name: string
  rule: RSSAutoDownloadRule
}

export interface RenameRSSRuleRequest {
  newName: string
}
