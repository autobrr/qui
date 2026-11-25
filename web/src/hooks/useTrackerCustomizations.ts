/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TrackerCustomization, TrackerCustomizationInput } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const QUERY_KEY = ["tracker-customizations"]

export function useTrackerCustomizations() {
  return useQuery<TrackerCustomization[]>({
    queryKey: QUERY_KEY,
    queryFn: () => api.listTrackerCustomizations(),
    staleTime: 30000,
    gcTime: 300000,
  })
}

export function useCreateTrackerCustomization() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: TrackerCustomizationInput) => api.createTrackerCustomization(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Create failed:", error)
      toast.error(t("trackerCustomizations.toasts.createFailed"))
    },
  })
}

export function useUpdateTrackerCustomization() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: TrackerCustomizationInput }) =>
      api.updateTrackerCustomization(id, data),
    onSuccess: (updated) => {
      queryClient.setQueryData<TrackerCustomization[]>(QUERY_KEY, old => old?.map(c => c.id === updated.id ? updated : c))
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Update failed:", error)
      toast.error(t("trackerCustomizations.toasts.updateFailed"))
    },
  })
}

export function useDeleteTrackerCustomization() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: number) => api.deleteTrackerCustomization(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Delete failed:", error)
      toast.error(t("trackerCustomizations.toasts.deleteFailed"))
    },
  })
}

export interface TrackerCustomizationEntry {
  displayName: string
  domains: string[]
  id: number
}

export interface TrackerCustomizationMaps {
  domainToCustomization: Map<string, TrackerCustomizationEntry>
  secondaryDomains: Set<string>
}

export function buildTrackerCustomizationMaps(customizations?: TrackerCustomization[] | null): TrackerCustomizationMaps {
  const domainToCustomization = new Map<string, TrackerCustomizationEntry>()
  const secondaryDomains = new Set<string>()

  for (const custom of customizations ?? []) {
    const domains: string[] = []
    const seen = new Set<string>()
    for (const rawDomain of custom.domains) {
      const normalized = rawDomain.trim().toLowerCase()
      if (!normalized) continue
      if (seen.has(normalized)) continue
      seen.add(normalized)
      domains.push(normalized)
    }
    if (domains.length === 0) continue

    for (let i = 0; i < domains.length; i++) {
      const domain = domains[i]
      domainToCustomization.set(domain, {
        displayName: custom.displayName,
        domains,
        id: custom.id,
      })
      if (i > 0) {
        secondaryDomains.add(domain)
      }
    }
  }

  return { domainToCustomization, secondaryDomains }
}

export function trackerGroupKey(tracker: string, domainToCustomization: Map<string, TrackerCustomizationEntry>): string {
  const lowerTracker = tracker.toLowerCase()
  const customization = domainToCustomization.get(lowerTracker)
  return customization ? `custom:${customization.id}` : lowerTracker
}
