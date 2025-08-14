/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from '@/types'

// Human-friendly labels for qBittorrent torrent states
export const TORRENT_STATE_LABELS: Record<string, string> = {
  // Downloading related
  downloading: 'Downloading',
  metaDL: 'Fetching Metadata',
  allocating: 'Allocating',
  stalledDL: 'Stalled',
  queuedDL: 'Queued',
  checkingDL: 'Checking',
  forcedDL: '(F) Downloading',

  // Uploading / Seeding related
  uploading: 'Seeding',
  stalledUP: 'Stalled',
  queuedUP: 'Queued',
  checkingUP: 'Checking',
  forcedUP: '(F) Seeding',

  // Paused / Stopped
  pausedDL: 'Paused',
  pausedUP: 'Paused',
  stoppedDL: 'Stopped',
  stoppedUP: 'Stopped',

  // Other
  error: 'Error',
  missingFiles: 'Missing Files',
  checkingResumeData: 'Checking Resume Data',
  moving: 'Moving',
}

export function getStateLabel(state: string): string {
  return TORRENT_STATE_LABELS[state] ?? state
}

// State groups for easier checking
export const DOWNLOADING_STATES = [
  'downloading',
  'stalledDL', 
  'metaDL',
  'queuedDL',
  'allocating',
  'checkingDL',
  'forcedDL'
] as const

export const SEEDING_STATES = [
  'uploading',
  'stalledUP',
  'queuedUP',
  'checkingUP',
  'forcedUP'
] as const

export const PAUSED_STATES = [
  'pausedDL',
  'pausedUP',
  'stoppedDL',
  'stoppedUP'
] as const

export const ACTIVE_STATES = [
  'downloading',
  'uploading',
  'forcedDL',
  'forcedUP'
] as const

export const ERROR_STATES = [
  'error',
  'missingFiles'
] as const

export const CHECKING_STATES = [
  'checkingDL',
  'checkingUP',
  'checkingResumeData'
] as const

// Helper functions to check torrent state
export function isDownloading(state: string): boolean {
  return DOWNLOADING_STATES.includes(state as any)
}

export function isSeeding(state: string): boolean {
  return SEEDING_STATES.includes(state as any)
}

export function isPaused(state: string): boolean {
  return PAUSED_STATES.includes(state as any)
}

export function isActive(state: string): boolean {
  return ACTIVE_STATES.includes(state as any)
}

export function isError(state: string): boolean {
  return ERROR_STATES.includes(state as any)
}

export function isChecking(state: string): boolean {
  return CHECKING_STATES.includes(state as any)
}

// Determine the appropriate paused state for a torrent
export function getPausedState(torrent: Torrent | any): string {
  // Use stoppedDL/stoppedUP (what qBittorrent actually uses for pause action)
  const isIncomplete = torrent.progress < 1 || 
    torrent.state.includes('DL') || 
    isDownloading(torrent.state)
  
  return isIncomplete ? 'stoppedDL' : 'stoppedUP'
}

// Determine the appropriate resumed state for a torrent (optimistic)
export function getResumedState(torrent: Torrent | any): string {
  // Set to stalled initially (more realistic, will be corrected by refetch)
  return torrent.progress < 1 ? 'stalledDL' : 'stalledUP'
}

// Check if a torrent matches a status filter
export function matchesStatusFilter(torrent: Torrent | any, statusFilter: string): boolean {
  const state = torrent.state
  
  switch (statusFilter) {
    case 'all':
      return true
    case 'downloading':
      return isDownloading(state)
    case 'seeding':
      return isSeeding(state)
    case 'completed':
      return torrent.progress === 1
    case 'paused':
      return isPaused(state)
    case 'active':
      return isActive(state)
    case 'inactive':
      return !isActive(state)
    case 'resumed':
      return !isPaused(state)
    case 'stalled':
      return state === 'stalledDL' || state === 'stalledUP'
    case 'stalled_uploading':
      return state === 'stalledUP'
    case 'stalled_downloading':
      return state === 'stalledDL'
    case 'errored':
      return isError(state)
    case 'checking':
      return isChecking(state)
    case 'moving':
      return state === 'moving'
    default:
      return state === statusFilter
  }
}

// Optimistically update a torrent's state for an action
export function getOptimisticTorrentState(
  torrent: Torrent | any,
  action: string,
  payload?: any
): Torrent | any {
  switch (action) {
    case 'pause':
      return {
        ...torrent,
        state: getPausedState(torrent),
        dlspeed: 0,
        upspeed: 0
      }
    
    case 'resume':
      return {
        ...torrent,
        state: getResumedState(torrent)
      }
    
    case 'recheck':
      // Set to checking state
      return {
        ...torrent,
        state: torrent.progress < 1 ? 'checkingDL' : 'checkingUP',
        dlspeed: 0,
        upspeed: 0
      }
    
    case 'setCategory':
      return {
        ...torrent,
        category: payload?.category || ''
      }
    
    case 'addTags':
      // Add tags to existing tags
      const currentTags = torrent.tags ? torrent.tags.split(', ').filter(Boolean) : []
      const newTags = payload?.tags ? payload.tags.split(',').map((t: string) => t.trim()) : []
      const combinedTags = [...new Set([...currentTags, ...newTags])]
      return {
        ...torrent,
        tags: combinedTags.join(', ')
      }
    
    case 'removeTags':
      // Remove tags from existing tags
      const existingTags = torrent.tags ? torrent.tags.split(', ').filter(Boolean) : []
      const tagsToRemove = payload?.tags ? payload.tags.split(',').map((t: string) => t.trim()) : []
      const remainingTags = existingTags.filter((tag: string) => !tagsToRemove.includes(tag))
      return {
        ...torrent,
        tags: remainingTags.join(', ')
      }
    
    case 'setTags':
      // Replace all tags
      return {
        ...torrent,
        tags: payload?.tags || ''
      }
    
    case 'toggleAutoTMM':
      return {
        ...torrent,
        auto_tmm: payload?.enable || false
      }
    
    default:
      return torrent
  }
}

// Filter torrents based on whether they should remain visible after an action
export function shouldRemainVisible(
  torrent: Torrent | any,
  statusFilters: string[],
  action: 'pause' | 'resume',
  isActionTarget: boolean
): boolean {
  // If not a target of the action, always remain visible
  if (!isActionTarget) return true
  
  // If no status filter, always remain visible
  if (statusFilters.length === 0) return true
  
  // Get the new state after the action
  const newState = action === 'pause' ? getPausedState(torrent) : getResumedState(torrent)
  const updatedTorrent = { ...torrent, state: newState }
  
  // Check if the torrent still matches any of the active filters
  return statusFilters.some(filter => matchesStatusFilter(updatedTorrent, filter))
}

// Process optimistic updates for a list of torrents
export function applyOptimisticUpdates(
  torrents: any[],
  targetHashes: string[],
  action: string,
  statusFilters: string[] = [],
  payload?: any
): { torrents: any[], removedCount: number } {
  let removedCount = 0
  
  // For delete action, just filter out the torrents
  if (action === 'delete' || action === 'deleteWithFiles') {
    const filteredTorrents = torrents.filter(torrent => {
      const isTarget = targetHashes.includes(torrent.hash)
      if (isTarget) {
        removedCount++
        return false
      }
      return true
    })
    return { torrents: filteredTorrents, removedCount }
  }
  
  const updatedTorrents = torrents
    .map(torrent => {
      const isTarget = targetHashes.includes(torrent.hash)
      if (isTarget) {
        return getOptimisticTorrentState(torrent, action, payload)
      }
      return torrent
    })
    .filter(torrent => {
      // Only filter for pause/resume actions based on status filters
      if (action === 'pause' || action === 'resume') {
        const isTarget = targetHashes.includes(torrent.hash)
        const remains = shouldRemainVisible(torrent, statusFilters, action as 'pause' | 'resume', isTarget)
        if (isTarget && !remains) {
          removedCount++
        }
        return remains
      }
      return true
    })
  
  return { torrents: updatedTorrents, removedCount }
}

// Apply optimistic state updates without filtering (for count queries)
export function applyOptimisticStateUpdates(
  torrents: any[],
  targetHashes: string[],
  action: string,
  payload?: any
): any[] {
  // For delete, filter out the torrents
  if (action === 'delete' || action === 'deleteWithFiles') {
    return torrents.filter(torrent => !targetHashes.includes(torrent.hash))
  }
  
  return torrents.map(torrent => {
    if (targetHashes.includes(torrent.hash)) {
      return getOptimisticTorrentState(torrent, action, payload)
    }
    return torrent
  })
}