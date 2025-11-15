/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useMemo, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AppPreferences, Category } from "@/types"

export interface InstanceMetadata {
  categories: Record<string, Category>
  tags: string[]
  preferences?: AppPreferences
}

const DEFAULT_PREF_FALLBACK_DELAY_MS = 400

interface UseInstanceMetadataOptions {
  fallbackDelayMs?: number
}

/**
 * Shared hook for fetching instance metadata (categories, tags, preferences)
 * This prevents duplicate API calls when multiple components need the same data
 * Note: Counts are now included in the torrents response, so we don't fetch them separately
 */
export function useInstanceMetadata(instanceId: number, options: UseInstanceMetadataOptions = {}) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(() => ["instance-metadata", instanceId] as const, [instanceId])
  const fallbackDelay = options.fallbackDelayMs ?? DEFAULT_PREF_FALLBACK_DELAY_MS

  const [metadata, setMetadata] = useState<InstanceMetadata | undefined>(() =>
    queryClient.getQueryData<InstanceMetadata>(queryKey)
  )
  const [error, setError] = useState<Error | null>(null)
  const [isFetchingFallback, setIsFetchingFallback] = useState(false)

  const fallbackRef = useRef<{
    timeoutId: ReturnType<typeof setTimeout> | null
    inflight: boolean
  }>({ timeoutId: null, inflight: false })

  useEffect(() => {
    setError(null)
    fallbackRef.current.inflight = false
    if (fallbackRef.current.timeoutId !== null) {
      (typeof window === "undefined" ? clearTimeout : window.clearTimeout)(fallbackRef.current.timeoutId)
      fallbackRef.current.timeoutId = null
    }
  }, [instanceId])

  useEffect(() => {
    if (!instanceId) {
      setMetadata(undefined)
      return
    }

    setMetadata(queryClient.getQueryData<InstanceMetadata>(queryKey))

    const unsubscribe = queryClient.getQueryCache().subscribe(event => {
      if (event.type !== "updated") {
        return
      }

      const key = event.query.queryKey
      if (!Array.isArray(key) || key.length < 2) {
        return
      }

      if (key[0] !== "instance-metadata" || key[1] !== instanceId) {
        return
      }

      setMetadata(event.query.state.data as InstanceMetadata | undefined)
    })

    return () => {
      unsubscribe?.()
    }
  }, [instanceId, queryClient, queryKey])

  useEffect(() => {
    if (!instanceId) {
      return
    }

    if (metadata?.preferences) {
      if (fallbackRef.current.timeoutId !== null) {
        (typeof window === "undefined" ? clearTimeout : window.clearTimeout)(fallbackRef.current.timeoutId)
        fallbackRef.current.timeoutId = null
      }
      fallbackRef.current.inflight = false
      return
    }

    if (!Number.isFinite(fallbackDelay) || fallbackDelay < 0) {
      return
    }

    if (fallbackRef.current.inflight || fallbackRef.current.timeoutId !== null) {
      return
    }

    const timeoutId = (typeof window === "undefined" ? setTimeout : window.setTimeout)(async () => {
      fallbackRef.current.timeoutId = null
      fallbackRef.current.inflight = true
      setIsFetchingFallback(true)

      try {
        const preferences = await api.getInstancePreferences(instanceId)

        setMetadata(previous => {
          const cached = queryClient.getQueryData<InstanceMetadata>(queryKey)
          const next: InstanceMetadata = {
            categories: cached?.categories ?? previous?.categories ?? {},
            tags: cached?.tags ?? previous?.tags ?? [],
            preferences,
          }
          queryClient.setQueryData(queryKey, next)
          return previous
        })
        setError(null)
      } catch (err) {
        if (err instanceof Error) {
          setError(err)
        } else {
          setError(new Error("Failed to load instance preferences"))
        }
      } finally {
        fallbackRef.current.inflight = false
        setIsFetchingFallback(false)
      }
    }, fallbackDelay)

    fallbackRef.current.timeoutId = timeoutId

    return () => {
      if (fallbackRef.current.timeoutId !== null) {
        (typeof window === "undefined" ? clearTimeout : window.clearTimeout)(fallbackRef.current.timeoutId)
        fallbackRef.current.timeoutId = null
      }
      fallbackRef.current.inflight = false
    }
  }, [fallbackDelay, instanceId, metadata?.preferences, queryClient, queryKey])

  const isLoading = !metadata?.preferences && (isFetchingFallback || !metadata)

  return {
    data: metadata,
    isLoading,
    error,
  }
}
