/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { DashboardSettings, DashboardSettingsInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

const QUERY_KEY = ["dashboard-settings"]

// Default settings to use while loading or on error
export const DEFAULT_DASHBOARD_SETTINGS: DashboardSettings = {
  id: 0,
  userId: 0,
  sectionVisibility: {
    "server-stats": true,
    "tracker-breakdown": true,
    "global-stats": true,
    "instances": true,
  },
  sectionOrder: ["server-stats", "tracker-breakdown", "global-stats", "instances"],
  sectionCollapsed: {},
  trackerBreakdownSortColumn: "uploaded",
  trackerBreakdownSortDirection: "desc",
  trackerBreakdownItemsPerPage: 15,
  createdAt: "",
  updatedAt: "",
}

/**
 * Hook for fetching the current user's dashboard settings
 */
export function useDashboardSettings() {
  return useQuery<DashboardSettings>({
    queryKey: QUERY_KEY,
    queryFn: () => api.getDashboardSettings(),
    staleTime: 60000, // 1 minute - settings don't change often
    gcTime: 300000, // Keep in cache for 5 minutes
    placeholderData: DEFAULT_DASHBOARD_SETTINGS,
  })
}

/**
 * Hook for updating dashboard settings with optimistic updates
 */
export function useUpdateDashboardSettings() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: DashboardSettingsInput) => api.updateDashboardSettings(data),
    onMutate: async (newData) => {
      // Cancel any outgoing refetches
      await queryClient.cancelQueries({ queryKey: QUERY_KEY })

      // Snapshot the previous value
      const previousSettings = queryClient.getQueryData<DashboardSettings>(QUERY_KEY)

      // Optimistically update to the new value
      if (previousSettings) {
        queryClient.setQueryData<DashboardSettings>(QUERY_KEY, {
          ...previousSettings,
          ...newData,
          sectionVisibility: newData.sectionVisibility ?? previousSettings.sectionVisibility,
          sectionOrder: newData.sectionOrder ?? previousSettings.sectionOrder,
          sectionCollapsed: newData.sectionCollapsed ?? previousSettings.sectionCollapsed,
          trackerBreakdownSortColumn: newData.trackerBreakdownSortColumn ?? previousSettings.trackerBreakdownSortColumn,
          trackerBreakdownSortDirection: newData.trackerBreakdownSortDirection ?? previousSettings.trackerBreakdownSortDirection,
          trackerBreakdownItemsPerPage: newData.trackerBreakdownItemsPerPage ?? previousSettings.trackerBreakdownItemsPerPage,
        })
      }

      return { previousSettings }
    },
    onError: (err, _newData, context) => {
      console.error("[DashboardSettings] Update failed:", err)
      // Rollback on error
      if (context?.previousSettings) {
        queryClient.setQueryData(QUERY_KEY, context.previousSettings)
      }
    },
    onSettled: () => {
      // Refetch after error or success
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
  })
}
