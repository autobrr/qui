/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { FilterView, FilterViewInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const QUERY_KEY = ["filter-views"]

/** Saved filter views, ordered by name by the backend. */
export function useFilterViews() {
  return useQuery<FilterView[]>({
    queryKey: QUERY_KEY,
    queryFn: () => api.listFilterViews(),
    staleTime: 30000, // overrides the 5s global default in App.tsx
  })
}

export function useCreateFilterView() {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: FilterViewInput) => api.createFilterView(data),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error: Error) => {
      console.error("[FilterViews] Create failed:", error)
      toast.error(t("filterSidebar.views.toasts.createFailed"), { description: error.message })
    },
  })
}

export function useUpdateFilterView() {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: FilterViewInput }) => api.updateFilterView(id, data),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error: Error) => {
      console.error("[FilterViews] Update failed:", error)
      toast.error(t("filterSidebar.views.toasts.updateFailed"), { description: error.message })
    },
  })
}

export function useDeleteFilterView() {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: number) => api.deleteFilterView(id),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error: Error) => {
      console.error("[FilterViews] Delete failed:", error)
      toast.error(t("filterSidebar.views.toasts.deleteFailed"), { description: error.message })
    },
  })
}
