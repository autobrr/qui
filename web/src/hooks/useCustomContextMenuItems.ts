/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useCallback } from "react"
import type { CustomContextMenuItem } from "@/types/contextMenu"
import { substituteTorrentVariables } from "@/types/contextMenu"
import type { Torrent } from "@/types"
import { api } from "@/lib/api"

const STORAGE_KEY = 'qui-custom-context-menu-items'

export function useCustomContextMenuItems() {
  const [customMenuItems, setCustomMenuItems] = useState<CustomContextMenuItem[]>([])
  const [loading, setLoading] = useState(true)

  // Load saved menu items from localStorage
  const loadMenuItems = useCallback(() => {
    try {
      const saved = localStorage.getItem(STORAGE_KEY)
      if (saved) {
        const parsed = JSON.parse(saved) as CustomContextMenuItem[]
        setCustomMenuItems(parsed)
      } else {
        setCustomMenuItems([])
      }
    } catch (error) {
      console.error('Failed to load custom context menu items:', error)
      setCustomMenuItems([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Save menu items to localStorage
  const saveMenuItems = useCallback((items: CustomContextMenuItem[]) => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(items))
      setCustomMenuItems(items)
    } catch (error) {
      console.error('Failed to save custom context menu items:', error)
      throw error
    }
  }, [])

  // Get only enabled menu items
  const enabledMenuItems = customMenuItems.filter(item => item.enabled)

  useEffect(() => {
    loadMenuItems()

    // Listen for storage changes to update menu items when settings change
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) {
        loadMenuItems()
      }
    }

    window.addEventListener('storage', handleStorageChange)
    return () => window.removeEventListener('storage', handleStorageChange)
  }, [loadMenuItems])

  return {
    customMenuItems,
    enabledMenuItems,
    loading,
    saveMenuItems,
    refreshMenuItems: loadMenuItems,
  }
}

export function executeCustomMenuAction(
  item: CustomContextMenuItem,
  torrent: Torrent
): string {
  let finalPath = torrent.save_path || ''

  // Apply path mapping if configured
  if (item.pathMapping) {
    const [serverPath, localPath] = item.pathMapping.split(':')
    if (serverPath && localPath) {
      const pathSeparator = navigator.platform.toLowerCase().includes('win') ? '\\' : '/'
      const mappedLocalPath = localPath.replace(/{pathSeparator}/g, pathSeparator)
      finalPath = finalPath.replace(serverPath, mappedLocalPath)
    }
  }

  // Substitute torrent variables in arguments
  let processedArguments = item.arguments
  if (processedArguments) {
    processedArguments = substituteTorrentVariables(processedArguments, torrent)
  }

  // Build the command that would be executed
  let command = item.executable
  if (processedArguments) {
    command += ` ${processedArguments}`
  }
  
  // Add the file path as the last argument if it's not already included in the arguments
  if (!processedArguments?.includes(finalPath)) {
    command += ` "${finalPath}"`
  }

  return command
}

// Execute custom menu action via API
export async function executeCustomMenuActionAPI(
  item: CustomContextMenuItem,
  torrent: Torrent,
  instanceId: number
): Promise<{
  success: boolean
  message: string
  command: string
  output?: string
  error?: string
  exitCode?: number
}> {
  return api.executeCustomAction(
    instanceId,
    torrent,
    item.executable,
    item.arguments,
    item.pathMapping,
    undefined, // workingDir
    item.highPrivileges,
    item.useCommandLine,
    item.keepTerminalOpen
  )
}