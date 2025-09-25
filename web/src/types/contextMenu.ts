/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

export interface CustomContextMenuItem {
  id: string
  name: string
  executable: string
  arguments: string
  pathMapping: string
  enabled: boolean
  highPrivileges: boolean
  useCommandLine: boolean
  keepTerminalOpen: boolean
}

export const DEFAULT_CUSTOM_MENU_ITEM: Omit<CustomContextMenuItem, 'id'> = {
  name: '',
  executable: '',
  arguments: '',
  pathMapping: '',
  enabled: true,
  highPrivileges: false,
  useCommandLine: true,
  keepTerminalOpen: false,
}

// Available torrent variables for substitution
export const TORRENT_VARIABLES = {
  'torrent.hash': 'Torrent hash',
  'torrent.name': 'Torrent name',
  'torrent.save_path': 'Save path',
  'torrent.category': 'Category',
  'torrent.tags': 'Tags (comma-separated)',
  'torrent.size': 'Total size in bytes',
  'torrent.progress': 'Progress (0-1)',
  'torrent.dlspeed': 'Download speed',
  'torrent.upspeed': 'Upload speed',
  'torrent.priority': 'Priority',
  'torrent.num_seeds': 'Number of seeds',
  'torrent.num_leechs': 'Number of leechers',
  'torrent.ratio': 'Share ratio',
  'torrent.eta': 'ETA in seconds',
  'torrent.state': 'Torrent state',
  'torrent.downloaded': 'Downloaded bytes',
  'torrent.uploaded': 'Uploaded bytes',
  'torrent.availability': 'Availability',
  'torrent.force_start': 'Force start flag',
  'torrent.super_seeding': 'Super seeding flag'
} as const

export type TorrentVariable = keyof typeof TORRENT_VARIABLES

/**
 * Substitute torrent variables in a string with actual torrent values
 */
export function substituteTorrentVariables(text: string, torrent: Torrent): string {
  let result = text
  
  // Replace each variable with its corresponding torrent property
  Object.keys(TORRENT_VARIABLES).forEach(variable => {
    const placeholder = `{${variable}}`
    if (result.includes(placeholder)) {
      const value = getTorrentVariableValue(variable as TorrentVariable, torrent)
      result = result.replace(new RegExp(placeholder.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'g'), String(value))
    }
  })
  
  return result
}

/**
 * Get the value of a torrent variable
 */
function getTorrentVariableValue(variable: TorrentVariable, torrent: Torrent): string | number {
  const path = variable.split('.').slice(1) // Remove 'torrent.' prefix
  let value: any = torrent
  
  for (const key of path) {
    value = value?.[key]
  }
  
  // Handle special cases
  if (variable === 'torrent.tags' && Array.isArray(value)) {
    return value.join(',')
  }
  
  return value ?? ''
}