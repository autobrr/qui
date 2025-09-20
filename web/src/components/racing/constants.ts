/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// API fetch limits - how many results to fetch from the backend
export const DEFAULT_API_LIMIT = 5
export const API_LIMIT_OPTIONS = [5, 10, 15, 20, 25, 50] as const

// UI pagination - how many rows to show per page in tables
export const DEFAULT_TORRENTS_PAGE_SIZE = 10
export const DEFAULT_TRACKER_STATS_PAGE_SIZE = 15

// Since API already limits results, we can show all fetched torrents
// without pagination, or use a reasonable page size
export const SHOW_ALL_TORRENTS = 100 // Effectively shows all since API limits to 50 max