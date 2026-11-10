/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { Category } from "@/types"

export interface InstanceMetadata {
  categories: Record<string, Category>
  tags: string[]
  // Set only on the entry that holds the place until the torrent list or the fallback loads the metadata.
  placeholder?: true
}

// The flag, not the object identity, marks the placeholder: React Query keeps the old object when
// the new data is deep-equal, for example an instance with no categories and no tags.
const EMPTY_METADATA: InstanceMetadata = { categories: {}, tags: [], placeholder: true }

const DEFAULT_FALLBACK_DELAY_MS = 400

interface UseInstanceMetadataOptions {
  fallbackDelayMs?: number
}

/**
 * Shared hook for fetching instance metadata (categories, tags)
 * This prevents duplicate API calls when multiple components need the same data
 * Note: Counts are now included in the torrents response, so we don't fetch them separately
 */
export function useInstanceMetadata(instanceId: number, options: UseInstanceMetadataOptions = {}) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(() => ["instance-metadata", instanceId] as const, [instanceId])
  const fallbackDelay = options.fallbackDelayMs ?? DEFAULT_FALLBACK_DELAY_MS

  const [error, setError] = useState<Error | null>(null)

  const getSnapshot = useCallback(
    () => {
      if (!instanceId) {
        return undefined
      }
      return queryClient.getQueryData<InstanceMetadata>(queryKey)
    },
    [instanceId, queryClient, queryKey]
  )

  const { data: metadata, refetch: refetchMetadata } = useQuery<InstanceMetadata | undefined>({
    queryKey,
    queryFn: async () => getSnapshot() ?? EMPTY_METADATA,
    initialData: () => getSnapshot() ?? EMPTY_METADATA,
    placeholderData: previous => previous ?? EMPTY_METADATA,
    enabled: Boolean(instanceId),
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

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

  const isLoaded = metadata !== undefined && !metadata.placeholder

  useEffect(() => {
    if (!instanceId) {
      return
    }

    if (isLoaded) {
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

      try {
        // The torrent-list stream normally hydrates this cache. When no stream has
        // populated it (e.g. add-torrent / RSS / workflow / dir-scan opened before
        // the torrents view), fetch categories and tags directly too, otherwise those
        // selectors render permanently empty.
        const [categories, tags] = await Promise.all([
          api.getCategories(instanceId),
          api.getTags(instanceId),
        ])

        const cached = queryClient.getQueryData<InstanceMetadata>(queryKey)
        const next: InstanceMetadata = {
          categories: categories ?? cached?.categories ?? metadata?.categories ?? {},
          tags: tags ?? cached?.tags ?? metadata?.tags ?? [],
        }
        queryClient.setQueryData(queryKey, next)
        setError(null)
      } catch (err) {
        if (err instanceof Error) {
          setError(err)
        } else {
          setError(new Error("Failed to load instance metadata"))
        }
      } finally {
        fallbackRef.current.inflight = false
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
  }, [fallbackDelay, instanceId, isLoaded, queryClient, queryKey])

  const isLoading = Boolean(instanceId) && !isLoaded

  return {
    data: instanceId ? metadata : undefined,
    isLoading,
    isError: error !== null,
    error,
    refreshMetadata: refetchMetadata,
  }
}
