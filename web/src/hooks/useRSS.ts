/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useRef, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import { RSSEventSource, type FeedsUpdatePayload } from "@/lib/rss-events"
import type {
  AddRSSFeedRequest,
  AddRSSFolderRequest,
  MoveRSSItemRequest,
  RemoveRSSItemRequest,
  RefreshRSSItemRequest,
  MarkRSSAsReadRequest,
  SetRSSRuleRequest,
  RenameRSSRuleRequest,
  SetRSSFeedURLRequest,
} from "@/types"

// Query keys
export const rssKeys = {
  all: ["rss"] as const,
  feeds: (instanceId: number) => [...rssKeys.all, "feeds", instanceId] as const,
  rules: (instanceId: number) => [...rssKeys.all, "rules", instanceId] as const,
  matching: (instanceId: number, ruleName: string) =>
    [...rssKeys.all, "matching", instanceId, ruleName] as const,
}

// ============================================================================
// Queries
// ============================================================================

export function useRSSFeeds(instanceId: number, options?: { enabled?: boolean; withData?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0
  const withData = options?.withData ?? true
  const queryClient = useQueryClient()
  const eventSourceRef = useRef<RSSEventSource | null>(null)
  const [sseStatus, setSseStatus] = useState<"disabled" | "connecting" | "live" | "reconnecting" | "disconnected">(
    "disabled"
  )
  const [sseReconnectAttempt, setSseReconnectAttempt] = useState(0)

  // Handle SSE updates - update query cache directly
  const handleFeedsUpdate = useCallback(
    (data: FeedsUpdatePayload) => {
      if (data.instanceId === instanceId) {
        // Set the data and invalidate to ensure re-render
        queryClient.setQueryData(rssKeys.feeds(instanceId), data.items)
        queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId), refetchType: "none" })
      }
    },
    [instanceId, queryClient]
  )

  // Setup SSE connection
  useEffect(() => {
    if (!shouldEnable) {
      setSseStatus("disabled")
      setSseReconnectAttempt(0)
      return
    }

    const eventSource = new RSSEventSource(instanceId, {
      onFeedsUpdate: handleFeedsUpdate,
      onConnected: () => {
        console.debug(`RSS SSE connected for instance ${instanceId}`)
        setSseStatus("live")
        setSseReconnectAttempt(0)
      },
      onDisconnected: () => {
        console.debug(`RSS SSE disconnected for instance ${instanceId}`)
        setSseStatus("disconnected")
      },
      onError: () => {
        console.warn(`RSS SSE error for instance ${instanceId}`)
        setSseStatus("reconnecting")
      },
      onReconnecting: ({ attempt }) => {
        setSseStatus("reconnecting")
        setSseReconnectAttempt(attempt)
      },
      onMaxReconnectAttempts: () => {
        setSseStatus("disconnected")
      },
    })

    setSseStatus("connecting")
    eventSource.connect()
    eventSourceRef.current = eventSource

    return () => {
      eventSource.disconnect()
      eventSourceRef.current = null
      setSseStatus("disabled")
      setSseReconnectAttempt(0)
    }
  }, [instanceId, shouldEnable, handleFeedsUpdate])

  // Initial data fetch - SSE handles subsequent updates
  const query = useQuery({
    queryKey: rssKeys.feeds(instanceId),
    queryFn: () => api.getRSSItems(instanceId, withData),
    enabled: shouldEnable,
    staleTime: Infinity, // SSE handles freshness, no automatic refetching
    // No refetchInterval - SSE replaces polling
  })

  return {
    ...query,
    sseStatus,
    sseReconnectAttempt,
  }
}

export function useRSSRules(instanceId: number, options?: { enabled?: boolean }) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0

  return useQuery({
    queryKey: rssKeys.rules(instanceId),
    queryFn: () => api.getRSSRules(instanceId),
    enabled: shouldEnable,
    staleTime: 30_000,
  })
}

export function useRSSMatchingArticles(
  instanceId: number,
  ruleName: string,
  options?: { enabled?: boolean }
) {
  const shouldEnable = (options?.enabled ?? true) && instanceId > 0 && !!ruleName

  return useQuery({
    queryKey: rssKeys.matching(instanceId, ruleName),
    queryFn: () => api.getRSSMatchingArticles(instanceId, ruleName),
    enabled: shouldEnable,
    staleTime: 5_000,
  })
}

// ============================================================================
// Feed Mutations
// ============================================================================

export function useAddRSSFeed(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: AddRSSFeedRequest) => api.addRSSFeed(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

export function useAddRSSFolder(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: AddRSSFolderRequest) => api.addRSSFolder(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

export function useRemoveRSSItem(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: RemoveRSSItemRequest) => api.removeRSSItem(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

export function useMoveRSSItem(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: MoveRSSItemRequest) => api.moveRSSItem(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

export function useRefreshRSSFeed(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: RefreshRSSItemRequest) => api.refreshRSSItem(instanceId, data),
    onSuccess: () => {
      // Invalidate after a short delay to allow qBittorrent to process the refresh
      setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
      }, 1000)
    },
  })
}

export function useSetRSSFeedURL(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: SetRSSFeedURLRequest) => api.setRSSFeedURL(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

// ============================================================================
// Article Mutations
// ============================================================================

export function useMarkRSSAsRead(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: MarkRSSAsReadRequest) => api.markRSSAsRead(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}

// ============================================================================
// Rule Mutations
// ============================================================================

export function useSetRSSRule(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: SetRSSRuleRequest) => api.setRSSRule(instanceId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.rules(instanceId) })
    },
  })
}

export function useRenameRSSRule(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: ({ ruleName, data }: { ruleName: string; data: RenameRSSRuleRequest }) =>
      api.renameRSSRule(instanceId, ruleName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.rules(instanceId) })
    },
  })
}

export function useRemoveRSSRule(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (ruleName: string) => api.removeRSSRule(instanceId, ruleName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: rssKeys.rules(instanceId) })
    },
  })
}

export function useReprocessRSSRules(instanceId: number) {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: () => api.reprocessRSSRules(instanceId),
    onSuccess: () => {
      // Invalidate feeds to reflect any changes from auto-download
      queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
    },
  })
}
