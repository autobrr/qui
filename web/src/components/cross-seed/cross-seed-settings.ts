/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import type { Category, CrossSeedAutomationSettingsPatch } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export const CROSS_SEED_SETTINGS_KEY = ["cross-seed", "settings"] as const
export const CROSS_SEED_STATUS_KEY = ["cross-seed", "status"] as const
export const CROSS_SEED_SEARCH_SETTINGS_KEY = ["cross-seed", "search", "settings"] as const

export function useCrossSeedSettings() {
  return useQuery({
    queryKey: CROSS_SEED_SETTINGS_KEY,
    queryFn: () => api.getCrossSeedSettings(),
    staleTime: 5 * 60 * 1000,
    gcTime: 10 * 60 * 1000,
  })
}

export function useCrossSeedStatus() {
  return useQuery({
    queryKey: CROSS_SEED_STATUS_KEY,
    queryFn: () => api.getCrossSeedStatus(),
    refetchInterval: false,
  })
}

export function useCrossSeedSearchSettings() {
  return useQuery({
    queryKey: CROSS_SEED_SEARCH_SETTINGS_KEY,
    queryFn: () => api.getCrossSeedSearchSettings(),
    staleTime: 5 * 60 * 1000,
    gcTime: 10 * 60 * 1000,
  })
}

/** Saves one card's fields. The PATCH keeps every field the payload leaves out. */
export function usePatchCrossSeedSettings() {
  const { t } = useTranslation("crossseed")
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: CrossSeedAutomationSettingsPatch) => api.patchCrossSeedSettings(payload),
    onSuccess: (data) => {
      toast.success(t("toast.settingsUpdated"))
      queryClient.setQueryData(CROSS_SEED_SETTINGS_KEY, data)
      void queryClient.refetchQueries({ queryKey: CROSS_SEED_STATUS_KEY })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })
}

export function useCrossSeedSearchStatus() {
  return useQuery({
    queryKey: ["cross-seed", "search-status"],
    queryFn: () => api.getCrossSeedSearchStatus(),
    // Poll only while a search is actively running for smooth live progress; events drive idle transitions.
    refetchInterval: (query) => query.state.data?.running ? 5_000 : false,
  })
}

export function useActiveInstances() {
  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })
  const activeInstances = useMemo(
    () => (instances ?? []).filter(instance => instance.isActive),
    [instances]
  )
  const activeInstanceIds = useMemo(() => activeInstances.map(instance => instance.id), [activeInstances])
  return { instances, activeInstances, activeInstanceIds }
}

export function useEnabledIndexers() {
  const { data: indexers } = useQuery({
    queryKey: ["torznab", "indexers"],
    queryFn: () => api.listTorznabIndexers(),
  })
  return useMemo(() => (indexers ?? []).filter(indexer => indexer.enabled), [indexers])
}

type AggregatedInstanceMetadata = { categories: Record<string, Category>; tags: string[] }

/** Categories and tags merged across the given instances, for source filter pickers. */
export function useAggregatedInstanceMetadata(instanceIds: number[]) {
  return useQuery({
    queryKey: ["cross-seed", "instance-metadata", instanceIds],
    queryFn: async (): Promise<AggregatedInstanceMetadata> => {
      const results = await Promise.all(
        instanceIds.map(async (instanceId) => {
          const [categories, tags] = await Promise.all([
            api.getCategories(instanceId),
            api.getTags(instanceId),
          ])
          return { categories, tags }
        })
      )
      const categories: AggregatedInstanceMetadata["categories"] = {}
      const tags = new Set<string>()
      for (const result of results) {
        Object.assign(categories, result.categories)
        for (const tag of result.tags) tags.add(tag)
      }
      return { categories, tags: Array.from(tags) }
    },
    enabled: instanceIds.length > 0,
    staleTime: 5 * 60 * 1000,
  })
}

export function useFormatDateValue() {
  const { formatDate } = useDateTimeFormatters()
  return useCallback((value?: string | Date | null) => {
    if (!value) {
      return "—"
    }
    const date = value instanceof Date ? value : new Date(value)
    if (Number.isNaN(date.getTime())) {
      return "—"
    }
    return formatDate(date)
  }, [formatDate])
}

export function normalizeStringList(values: string[]): string[] {
  return Array.from(new Set(values.map(item => item.trim()).filter(Boolean)))
}

export function normalizeNumberList(values: Array<string | number>): number[] {
  return Array.from(new Set(
    values
      .map(value => Number(value))
      .filter(value => !Number.isNaN(value) && value > 0)
  ))
}

export function useMissingIndexersToast() {
  const { t } = useTranslation("crossseed")
  const notifyMissingIndexers = useCallback((context: string) => {
    toast.error(t("toast.noIndexersConfigured"), {
      description: t("toast.noIndexersConfiguredDescription", { context }),
    })
  }, [t])

  const handleIndexerError = useCallback((error: Error, context: string) => {
    const normalized = error.message?.toLowerCase?.() ?? ""
    if (normalized.includes("torznab indexers")) {
      notifyMissingIndexers(context)
      return true
    }
    return false
  }, [notifyMissingIndexers])

  return { notifyMissingIndexers, handleIndexerError }
}

export const MIN_RSS_INTERVAL_MINUTES = 30
export const DEFAULT_RSS_INTERVAL_MINUTES = 120

function formatDurationShort(ms: number): string {
  const totalSeconds = Math.ceil(Math.max(ms, 0) / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts: string[] = []
  if (hours > 0) {
    parts.push(`${hours}h`)
  }
  parts.push(`${String(minutes).padStart(2, "0")}m`)
  parts.push(`${String(seconds).padStart(2, "0")}s`)
  return parts.join(" ")
}

/** Manual RSS runs wait one run interval after the last run. Ticks once a second while the wait is on. */
export function useManualRunCooldown(runIntervalMinutes: number, lastRunStartedAt?: string) {
  const enforcedRunIntervalMinutes = Math.max(runIntervalMinutes, MIN_RSS_INTERVAL_MINUTES)
  const [now, setNow] = useState(() => Date.now())

  const nextManualRunAt = useMemo(() => {
    if (!lastRunStartedAt) {
      return null
    }
    const startedAt = new Date(lastRunStartedAt)
    if (Number.isNaN(startedAt.getTime())) {
      return null
    }
    return new Date(startedAt.getTime() + enforcedRunIntervalMinutes * 60 * 1000)
  }, [enforcedRunIntervalMinutes, lastRunStartedAt])

  const remainingMs = nextManualRunAt ? Math.max(nextManualRunAt.getTime() - now, 0) : 0
  const active = remainingMs > 0

  useEffect(() => {
    if (!active) {
      return
    }
    const tick = () => setNow(Date.now())
    tick()
    const interval = window.setInterval(tick, 1_000)
    return () => window.clearInterval(interval)
  }, [active, nextManualRunAt])

  return {
    active,
    display: active ? formatDurationShort(remainingMs) : "",
    enforcedRunIntervalMinutes,
  }
}
