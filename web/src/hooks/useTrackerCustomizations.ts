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

/**
 * Hook for fetching all tracker customizations (nicknames and merged domains)
 */
export function useTrackerCustomizations() {
  return useQuery<TrackerCustomization[]>({
    queryKey: QUERY_KEY,
    queryFn: () => api.listTrackerCustomizations(),
    staleTime: 30000, // 30 seconds
    gcTime: 300000, // Keep in cache for 5 minutes
  })
}

/**
 * Hook for creating a new tracker customization
 */
export function useCreateTrackerCustomization() {
  const { t } = useTranslation("dashboard")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: TrackerCustomizationInput) => api.createTrackerCustomization(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Create failed:", error)
      toast.error(t("trackerBreakdown.toasts.createFailed"))
    },
  })
}

/**
 * Hook for updating an existing tracker customization
 */
export function useUpdateTrackerCustomization() {
  const { t } = useTranslation("dashboard")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ id, data }: { id: number; data: TrackerCustomizationInput }) =>
      api.updateTrackerCustomization(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Update failed:", error)
      toast.error(t("trackerBreakdown.toasts.updateFailed"))
    },
  })
}

/**
 * Hook for deleting a tracker customization
 */
export function useDeleteTrackerCustomization() {
  const { t } = useTranslation("dashboard")
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (id: number) => api.deleteTrackerCustomization(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: QUERY_KEY })
    },
    onError: (error) => {
      console.error("[TrackerCustomization] Delete failed:", error)
      toast.error(t("trackerBreakdown.toasts.deleteFailed"))
    },
  })
}

export interface TrackerCustomizationEntry {
  displayName: string
  /** Normalized lowercase domains, primary is index 0 */
  domains: string[]
  id: number
}

export interface TrackerCustomizationMaps {
  /** Maps lowercase domain to its customization entry */
  domainToCustomization: Map<string, TrackerCustomizationEntry>
  /** Set of lowercase secondary domains (not the primary/first domain) */
  secondaryDomains: Set<string>
}

/**
 * Builds lookup maps from tracker customizations for merging and nicknames.
 * This pure function can be used inside useMemo to derive maps from customization data.
 */
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
