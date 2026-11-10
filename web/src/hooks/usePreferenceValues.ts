/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"

import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import type { InstanceCapabilities } from "@/types"

// Typed reads of single preferences for the screens outside the preferences dialog.
// Each hook returns only the values that its screen shows, so the source of the
// preferences can change here without a change to the screens.

type PreferenceOptions = Parameters<typeof useInstancePreferences>[1]

export function useListenPort(instanceId: number | undefined, options?: PreferenceOptions): number | undefined {
  return useInstancePreferences(instanceId, options).preferences?.listen_port
}

export function useGlobalMaxRatio(instanceId: number | undefined, options?: PreferenceOptions): number | undefined {
  return useInstancePreferences(instanceId, options).preferences?.max_ratio
}

// The fallback applies when the preferences do not have use_subcategories, for
// example the value that the torrent list response reports.
export function useSubcategoriesEnabled(
  instanceId: number | undefined,
  capabilities: InstanceCapabilities | undefined,
  fallback?: boolean,
  options?: PreferenceOptions
): boolean {
  const { preferences } = useInstancePreferences(instanceId, options)
  return Boolean(
    capabilities?.supportsSubcategories &&
    (capabilities.subcategoriesAlwaysEnabled || (preferences?.use_subcategories ?? fallback ?? false))
  )
}

export function useQueueLimits(instanceId: number | undefined, options?: PreferenceOptions) {
  const { preferences } = useInstancePreferences(instanceId, options)
  return {
    enabled: preferences?.queueing_enabled ?? false,
    maxActiveDownloads: preferences?.max_active_downloads ?? 0,
    maxActiveUploads: preferences?.max_active_uploads ?? 0,
    maxActiveTorrents: preferences?.max_active_torrents ?? 0,
  }
}

export function useAddTorrentPreferences(instanceId: number | undefined, options?: PreferenceOptions) {
  const { preferences } = useInstancePreferences(instanceId, options)
  return useMemo(() => ({
    loaded: preferences !== undefined,
    defaults: {
      autoTMM: preferences?.auto_tmm_enabled ?? true,
      savePath: preferences?.save_path || "",
      contentLayout: preferences?.torrent_content_layout || "",
      tempPathEnabled: preferences?.temp_path_enabled ?? false,
      tempPath: preferences?.temp_path || "",
      startPaused: preferences?.add_stopped_enabled ?? preferences?.start_paused_enabled ?? false,
    },
    autoAddTrackers: Boolean(preferences?.add_trackers_enabled && preferences?.add_trackers),
    excludedFileNamesEnabled: preferences?.excluded_file_names_enabled ?? false,
    excludedFileNames: preferences?.excluded_file_names ?? "",
  }), [preferences])
}

export function useRSSPreferences(instanceId: number | undefined, options?: PreferenceOptions) {
  const { preferences } = useInstancePreferences(instanceId, options)
  return {
    processingEnabled: preferences?.rss_processing_enabled ?? true,
    autoDownloadingEnabled: preferences?.rss_auto_downloading_enabled ?? true,
  }
}
