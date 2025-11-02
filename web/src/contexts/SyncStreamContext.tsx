/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TorrentFilters, TorrentStreamMeta, TorrentStreamPayload } from "@/types"
import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef } from "react"

const RETRY_BASE_DELAY_MS = 4000
const RETRY_MAX_DELAY_MS = 30000
const MAX_RETRY_ATTEMPTS = 6

export interface StreamParams {
  instanceId: number
  page: number
  limit: number
  sort: string
  order: "asc" | "desc"
  search?: string
  filters?: TorrentFilters
}

type StreamListener = (payload: TorrentStreamPayload) => void

export interface StreamState {
  connected: boolean
  error: string | null
  lastMeta?: TorrentStreamMeta
  retrying: boolean
  retryAttempt: number
  nextRetryAt?: number
}

interface SyncStreamContextValue {
  connect: (params: StreamParams, listener: StreamListener) => () => void
  getState: (key: string | null) => StreamState | undefined
}

interface StreamEntry {
  key: string
  params: StreamParams
  listeners: Set<StreamListener>
  source?: EventSource
  connected: boolean
  error: string | null
  lastMeta?: TorrentStreamMeta
  retryAttempt: number
  retryTimer?: number
  nextRetryAt?: number
  handlers?: {
    payload: (event: MessageEvent) => void
    networkError: (event: Event) => void
  }
}

const SyncStreamContext = createContext<SyncStreamContextValue | null>(null)

export function SyncStreamProvider({ children }: { children: React.ReactNode }) {
  const streamsRef = useRef<Record<string, StreamEntry>>({})
  const [, forceUpdate] = useReducer((count: number) => count + 1, 0)

  const clearRetryState = useCallback((entry: StreamEntry) => {
    if (entry.retryTimer !== undefined) {
      if (typeof window !== "undefined") {
        window.clearTimeout(entry.retryTimer)
      } else {
        clearTimeout(entry.retryTimer)
      }
      entry.retryTimer = undefined
    }

    entry.retryAttempt = 0
    entry.nextRetryAt = undefined
  }, [])

  const closeStream = useCallback((entry: StreamEntry) => {
    if (!entry.source) {
      return
    }

    const { source, handlers } = entry
    if (handlers) {
      source.removeEventListener("init", handlers.payload)
      source.removeEventListener("update", handlers.payload)
      source.removeEventListener("error", handlers.payload)
    }

    source.onopen = null
    source.onerror = null
    source.close()

    entry.source = undefined
    entry.connected = false
    entry.handlers = undefined
  }, [])

  const openStream = useCallback(
    (entry: StreamEntry, options: { resetRetry?: boolean } = {}) => {
      const resetRetry = options.resetRetry ?? false

      if (typeof window === "undefined" || typeof EventSource === "undefined") {
        entry.error = "Server-sent events are not supported in this environment"
        entry.connected = false
        clearRetryState(entry)
        forceUpdate()
        return
      }

      if (resetRetry) {
        clearRetryState(entry)
      }

      if (entry.retryTimer !== undefined) {
        window.clearTimeout(entry.retryTimer)
        entry.retryTimer = undefined
      }
      entry.nextRetryAt = undefined

      const url = api.getTorrentsStreamUrl(entry.params.instanceId, {
        page: entry.params.page,
        limit: entry.params.limit,
        sort: entry.params.sort,
        order: entry.params.order,
        search: entry.params.search,
        filters: entry.params.filters,
      })

      const scheduleReconnect = () => {
        if (entry.listeners.size === 0) {
          return
        }
        if (streamsRef.current[entry.key] !== entry) {
          return
        }
        if (entry.retryTimer !== undefined) {
          return
        }

        closeStream(entry)
        entry.connected = false

        entry.retryAttempt = Math.min(entry.retryAttempt + 1, MAX_RETRY_ATTEMPTS)
        const exponent = Math.max(0, entry.retryAttempt - 1)
        const delay = Math.min(RETRY_BASE_DELAY_MS * Math.pow(2, exponent), RETRY_MAX_DELAY_MS)

        if (!entry.error) {
          entry.error = "Stream disconnected"
        }

        entry.nextRetryAt = Date.now() + delay

        entry.retryTimer = window.setTimeout(() => {
          entry.retryTimer = undefined
          entry.nextRetryAt = undefined

          if (streamsRef.current[entry.key] !== entry || entry.listeners.size === 0) {
            return
          }

          openStream(entry)
        }, delay)

        forceUpdate()
      }

      try {
        const source = new EventSource(url, { withCredentials: true })

        const payloadHandler = (event: MessageEvent) => {
          try {
            const payload = JSON.parse(event.data) as TorrentStreamPayload
            entry.lastMeta = payload.meta

            if (payload.type === "error" && payload.err) {
              entry.error = payload.err
            } else {
              entry.error = null
            }

            if (payload.type !== "error") {
              entry.connected = true
              entry.retryAttempt = 0
              entry.nextRetryAt = undefined
            }

            entry.listeners.forEach(listener => listener(payload))
            forceUpdate()
          } catch (err) {
            console.error("Failed to parse SSE payload", err)
          }
        }

        const networkErrorHandler = (_event?: Event) => {
          scheduleReconnect()
        }

        source.addEventListener("init", payloadHandler)
        source.addEventListener("update", payloadHandler)
        source.addEventListener("error", payloadHandler)
        source.onopen = () => {
          entry.connected = true
          entry.error = null
          entry.retryAttempt = 0
          entry.nextRetryAt = undefined

          if (entry.retryTimer !== undefined) {
            window.clearTimeout(entry.retryTimer)
            entry.retryTimer = undefined
          }

          forceUpdate()
        }
        source.onerror = networkErrorHandler

        entry.source = source
        entry.handlers = {
          payload: payloadHandler,
          networkError: networkErrorHandler,
        }
        entry.connected = false
        if (resetRetry) {
          entry.error = null
        }
        forceUpdate()
      } catch (err) {
        entry.connected = false
        entry.error = err instanceof Error ? err.message : "Failed to open stream"
        entry.retryAttempt = Math.min(entry.retryAttempt + 1, MAX_RETRY_ATTEMPTS)

        const exponent = Math.max(0, entry.retryAttempt - 1)
        const delay = Math.min(RETRY_BASE_DELAY_MS * Math.pow(2, exponent), RETRY_MAX_DELAY_MS)

        entry.nextRetryAt = Date.now() + delay
        entry.retryTimer = window.setTimeout(() => {
          entry.retryTimer = undefined
          entry.nextRetryAt = undefined

          if (streamsRef.current[entry.key] !== entry || entry.listeners.size === 0) {
            return
          }

          openStream(entry)
        }, delay)

        forceUpdate()
      }
    },
    [clearRetryState, closeStream, forceUpdate]
  )

  const ensureStream = useCallback(
    (params: StreamParams) => {
      const key = createStreamKey(params)
      let entry = streamsRef.current[key]

      if (!entry) {
        entry = {
          key,
          params,
          listeners: new Set(),
          connected: false,
          error: null,
          retryAttempt: 0,
        }
        streamsRef.current[key] = entry
        openStream(entry, { resetRetry: true })
      } else if (!isSameParams(entry.params, params)) {
        closeStream(entry)
        entry.params = params
        openStream(entry, { resetRetry: true })
      }

      return entry
    },
    [closeStream, openStream]
  )

  const connect = useCallback(
    (params: StreamParams, listener: StreamListener) => {
      const entry = ensureStream(params)
      entry.listeners.add(listener)
      forceUpdate()

      return () => {
        entry.listeners.delete(listener)
        if (entry.listeners.size === 0) {
          closeStream(entry)
          clearRetryState(entry)
          delete streamsRef.current[entry.key]
        }
        forceUpdate()
      }
    },
    [clearRetryState, closeStream, ensureStream]
  )

  const getState = useCallback((key: string | null) => {
    if (!key) {
      return undefined
    }

    const entry = streamsRef.current[key]
    if (!entry) {
      return undefined
    }

    return {
      connected: entry.connected,
      error: entry.error,
      lastMeta: entry.lastMeta,
      retrying: entry.retryTimer !== undefined,
      retryAttempt: entry.retryAttempt,
      nextRetryAt: entry.nextRetryAt,
    }
  }, [])

  const contextValue = useMemo<SyncStreamContextValue>(
    () => ({
      connect,
      getState,
    }),
    [connect, getState]
  )

  return <SyncStreamContext.Provider value={contextValue}>{children}</SyncStreamContext.Provider>
}

export function useSyncStream(
  params: StreamParams | null,
  options: { enabled?: boolean; onMessage?: StreamListener } = {}
) {
  const context = useContext(SyncStreamContext)
  if (!context) {
    throw new Error("useSyncStream must be used within a SyncStreamProvider")
  }

  const { enabled = true, onMessage } = options

  const key = useMemo(() => (params ? createStreamKey(params) : null), [params])

  const listenerRef = useRef<StreamListener | undefined>(onMessage)
  useEffect(() => {
    listenerRef.current = onMessage
  }, [onMessage])

  const paramsRef = useRef<typeof params>(params)
  useEffect(() => {
    paramsRef.current = params
  }, [params])

  useEffect(() => {
    if (!enabled || !key || !paramsRef.current) {
      return
    }

    return context.connect(paramsRef.current, payload => {
      listenerRef.current?.(payload)
    })
  }, [context, enabled, key])

  return (
    context.getState(key) ?? {
      connected: false,
      error: null,
      retrying: false,
      retryAttempt: 0,
      nextRetryAt: undefined,
    }
  )
}

export function useSyncStreamManager(): SyncStreamContextValue {
  const context = useContext(SyncStreamContext)
  if (!context) {
    throw new Error("useSyncStreamManager must be used within a SyncStreamProvider")
  }
  return context
}

export function createStreamKey(params: StreamParams): string {
  const filtersKey = params.filters ? JSON.stringify(params.filters) : "__none__"
  const search = params.search ?? ""
  return [params.instanceId, params.page, params.limit, params.sort, params.order, search, filtersKey].join("|")
}

function isSameParams(a: StreamParams, b: StreamParams): boolean {
  if (
    a.instanceId !== b.instanceId ||
    a.page !== b.page ||
    a.limit !== b.limit ||
    a.sort !== b.sort ||
    a.order !== b.order ||
    (a.search || "") !== (b.search || "")
  ) {
    return false
  }

  const aFilters = a.filters ? JSON.stringify(a.filters) : ""
  const bFilters = b.filters ? JSON.stringify(b.filters) : ""
  return aFilters === bFilters
}
