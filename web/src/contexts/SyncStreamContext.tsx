/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { TorrentFilters, TorrentStreamMeta, TorrentStreamPayload } from "@/types"
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react"

const RETRY_BASE_DELAY_MS = 4000
const RETRY_MAX_DELAY_MS = 30000
const MAX_RETRY_ATTEMPTS = 6
const HANDOFF_GRACE_PERIOD_MS = 1200

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
  connect: (
    params: StreamParams,
    listener: StreamListener,
    options?: { preserveConnected?: boolean }
  ) => () => void
  getState: (key: string | null) => StreamState | undefined
  subscribe: (key: string, listener: (state: StreamState) => void) => () => void
}

interface StreamEntry {
  key: string
  params: StreamParams
  listeners: Set<StreamListener>
  connected: boolean
  error: string | null
  lastMeta?: TorrentStreamMeta
  handoffTimer?: number
  handoffPending?: boolean
}

interface StreamConnection {
  source?: EventSource
  handlers?: {
    payload: (event: MessageEvent) => void
    networkError: (event: Event) => void
  }
  signature?: string
  retryAttempt: number
  retryTimer?: number
  nextRetryAt?: number
}

const SyncStreamContext = createContext<SyncStreamContextValue | null>(null)

const DEFAULT_STREAM_STATE: StreamState = {
  connected: false,
  error: null,
  retrying: false,
  retryAttempt: 0,
  nextRetryAt: undefined,
}

export function SyncStreamProvider({ children }: { children: React.ReactNode }) {
  const streamsRef = useRef<Record<string, StreamEntry>>({})
  const stateSubscribersRef = useRef<Record<string, Set<(state: StreamState) => void>>>({})
  const connectionRef = useRef<StreamConnection>({ retryAttempt: 0 })
  const scheduleReconnectRef = useRef<() => void>(() => {})

  const getSnapshot = useCallback(
    (key: string): StreamState => {
      const entry = streamsRef.current[key]
      const connection = connectionRef.current
      if (!entry) {
        return {
          ...DEFAULT_STREAM_STATE,
          retrying: connection.retryTimer !== undefined,
          retryAttempt: connection.retryAttempt,
          nextRetryAt: connection.nextRetryAt,
        }
      }

      return {
        connected: entry.connected,
        error: entry.error,
        lastMeta: entry.lastMeta,
        retrying: connection.retryTimer !== undefined,
        retryAttempt: connection.retryAttempt,
        nextRetryAt: connection.nextRetryAt,
      }
    },
    []
  )

  const notifyStateSubscribers = useCallback(
    (key: string) => {
      const subscribers = stateSubscribersRef.current[key]
      if (!subscribers || subscribers.size === 0) {
        return
      }

      const snapshot = getSnapshot(key)

      subscribers.forEach(listener => {
        try {
          listener(snapshot)
        } catch (err) {
          console.error("SyncStream subscriber failed", err)
        }
      })
    },
    [getSnapshot]
  )

  const subscribeToState = useCallback(
    (key: string, listener: (state: StreamState) => void) => {
      if (!stateSubscribersRef.current[key]) {
        stateSubscribersRef.current[key] = new Set()
      }
      stateSubscribersRef.current[key].add(listener)

      return () => {
        const subscribers = stateSubscribersRef.current[key]
        if (!subscribers) {
          return
        }

        subscribers.delete(listener)
        if (subscribers.size === 0) {
          delete stateSubscribersRef.current[key]
        }
      }
    },
    []
  )

  const clearHandoffState = useCallback((entry: StreamEntry) => {
    if (entry.handoffTimer !== undefined) {
      if (typeof window !== "undefined") {
        window.clearTimeout(entry.handoffTimer)
      } else {
        clearTimeout(entry.handoffTimer)
      }
      entry.handoffTimer = undefined
    }
    entry.handoffPending = false
  }, [])

  const clearConnectionRetryState = useCallback(() => {
    const connection = connectionRef.current
    if (connection.retryTimer !== undefined) {
      if (typeof window !== "undefined") {
        window.clearTimeout(connection.retryTimer)
      } else {
        clearTimeout(connection.retryTimer)
      }
      connection.retryTimer = undefined
    }
    connection.retryAttempt = 0
    connection.nextRetryAt = undefined
  }, [])

  const notifyAllStateSubscribers = useCallback(() => {
    Object.keys(stateSubscribersRef.current).forEach(key => {
      notifyStateSubscribers(key)
    })
  }, [notifyStateSubscribers])

  const closeConnection = useCallback(
    (options: { preserveRetry?: boolean } = {}) => {
      const { preserveRetry = false } = options
      const connection = connectionRef.current
      if (!connection.source) {
        if (!preserveRetry) {
          clearConnectionRetryState()
        }
        connection.signature = undefined
        connection.handlers = undefined
        return
      }

      const { source, handlers } = connection
      if (handlers) {
        source.removeEventListener("init", handlers.payload)
        source.removeEventListener("update", handlers.payload)
        source.removeEventListener("error", handlers.payload)
      }

      source.onopen = null
      source.onerror = null
      source.close()

      connection.source = undefined
      connection.handlers = undefined
      connection.signature = undefined

      if (!preserveRetry) {
        clearConnectionRetryState()
      }
    },
    [clearConnectionRetryState]
  )

  const buildStreamPayload = (entries: StreamEntry[]) =>
    entries
      .map(entry => ({
        key: entry.key,
        instanceId: entry.params.instanceId,
        page: entry.params.page,
        limit: entry.params.limit,
        sort: entry.params.sort,
        order: entry.params.order,
        search: entry.params.search ?? "",
        filters: entry.params.filters ?? null,
      }))
      .sort((a, b) => a.key.localeCompare(b.key))

  const openConnection = useCallback(
    (
      entries: StreamEntry[],
      options: { preserveState?: boolean; resetRetry?: boolean } = {}
    ) => {
      const normalized = buildStreamPayload(entries)
      const signature = JSON.stringify(normalized)
      const connection = connectionRef.current

      if (connection.signature === signature && connection.source) {
        return
      }

      const preserveState = options.preserveState ?? Boolean(connection.source)
      const resetRetry = options.resetRetry ?? false

      if (typeof window === "undefined" || typeof EventSource === "undefined") {
        entries.forEach(entry => {
          entry.connected = false
          entry.error = "Server-sent events are not supported in this environment"
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
        closeConnection()
        return
      }

      if (resetRetry) {
        clearConnectionRetryState()
      } else if (connection.retryTimer !== undefined) {
        if (typeof window !== "undefined") {
          window.clearTimeout(connection.retryTimer)
        } else {
          clearTimeout(connection.retryTimer)
        }
        connection.retryTimer = undefined
        connection.nextRetryAt = undefined
      }

      if (preserveState) {
        entries.forEach(entry => {
          if (!entry.connected || entry.handoffPending) {
            return
          }
          entry.handoffPending = true
          if (entry.handoffTimer !== undefined) {
            if (typeof window !== "undefined") {
              window.clearTimeout(entry.handoffTimer)
            } else {
              clearTimeout(entry.handoffTimer)
            }
          }
          const timer = (typeof window !== "undefined"
            ? window.setTimeout
            : (setTimeout as unknown as (handler: () => void, timeout: number) => number))(() => {
              entry.handoffTimer = undefined
              if (!entry.handoffPending) {
                return
              }
              entry.handoffPending = false
              entry.connected = false
              notifyStateSubscribers(entry.key)
            }, HANDOFF_GRACE_PERIOD_MS)
          entry.handoffTimer = timer
        })
      } else {
        entries.forEach(entry => {
          entry.connected = false
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
      }

      const url = api.getTorrentsStreamBatchUrl(normalized)
      closeConnection({ preserveRetry: true })

      const payloadHandler = (event: MessageEvent) => {
        try {
          const payload = JSON.parse(event.data) as TorrentStreamPayload
          const streamKey = payload.meta?.streamKey
          if (!streamKey) {
            return
          }

          const entry = streamsRef.current[streamKey]
          if (!entry) {
            return
          }

          entry.lastMeta = payload.meta

          if (payload.type === "error" && payload.error) {
            entry.error = payload.error
            entry.connected = false
          } else {
            entry.error = null
            entry.connected = true
          }

          clearHandoffState(entry)
          entry.listeners.forEach(listener => listener(payload))
          notifyStateSubscribers(streamKey)
        } catch (err) {
          console.error("Failed to parse SSE payload", err)
        }
      }

      const handleNetworkError = (_event?: Event) => {
        closeConnection({ preserveRetry: true })

        Object.values(streamsRef.current).forEach(entry => {
          clearHandoffState(entry)
          if (!entry.error) {
            entry.error = "Stream disconnected"
          }
          entry.connected = false
          notifyStateSubscribers(entry.key)
        })

        scheduleReconnectRef.current()
      }

      const source = new EventSource(url, { withCredentials: true })
      source.addEventListener("init", payloadHandler)
      source.addEventListener("update", payloadHandler)
      source.addEventListener("error", payloadHandler)
      source.onopen = () => {
        clearConnectionRetryState()
        connection.retryAttempt = 0
        connection.nextRetryAt = undefined
        normalized.forEach(({ key }) => {
          const entry = streamsRef.current[key]
          if (!entry) {
            return
          }
          if (!entry.handoffPending) {
            entry.error = null
          }
          notifyStateSubscribers(key)
        })
      }
      source.onerror = handleNetworkError

      connection.source = source
      connection.handlers = {
        payload: payloadHandler,
        networkError: handleNetworkError,
      }
      connection.signature = signature
    },
    [clearConnectionRetryState, clearHandoffState, closeConnection, notifyStateSubscribers]
  )

  const ensureConnection = useCallback(
    (options: { preserveState?: boolean; resetRetry?: boolean } = {}) => {
      const entries = Object.values(streamsRef.current)
      if (entries.length === 0) {
        closeConnection()
        clearConnectionRetryState()
        notifyAllStateSubscribers()
        return
      }

      openConnection(entries, options)
    },
    [clearConnectionRetryState, closeConnection, notifyAllStateSubscribers, openConnection]
  )

  const scheduleReconnect = useCallback(() => {
    const connection = connectionRef.current
    if (connection.retryTimer !== undefined) {
      return
    }

    connection.retryAttempt = Math.min(connection.retryAttempt + 1, MAX_RETRY_ATTEMPTS)
    const exponent = Math.max(0, connection.retryAttempt - 1)
    const delay = Math.min(RETRY_BASE_DELAY_MS * Math.pow(2, exponent), RETRY_MAX_DELAY_MS)

    connection.nextRetryAt = Date.now() + delay

    const timer = (typeof window !== "undefined"
      ? window.setTimeout
      : (setTimeout as unknown as (handler: () => void, timeout: number) => number))(() => {
        connection.retryTimer = undefined
        connection.nextRetryAt = undefined

        if (Object.keys(streamsRef.current).length === 0) {
          clearConnectionRetryState()
          notifyAllStateSubscribers()
          return
        }

        ensureConnection({ preserveState: false })
        notifyAllStateSubscribers()
      }, delay)

    connection.retryTimer = timer
    notifyAllStateSubscribers()
  }, [clearConnectionRetryState, ensureConnection, notifyAllStateSubscribers])

  scheduleReconnectRef.current = scheduleReconnect

  const ensureStream = useCallback(
    (params: StreamParams, options: { preserveConnected?: boolean } = {}) => {
      const key = createStreamKey(params)
      let entry = streamsRef.current[key]

      if (!entry) {
        entry = {
          key,
          params,
          listeners: new Set(),
          connected: options.preserveConnected ?? false,
          error: null,
        }
        streamsRef.current[key] = entry
        ensureConnection({ preserveState: true })
      } else if (!isSameParams(entry.params, params)) {
        entry.params = params
        entry.error = null
        ensureConnection({ preserveState: true, resetRetry: true })
      } else {
        ensureConnection({ preserveState: true })
      }

      return entry
    },
    [ensureConnection]
  )

  const connect = useCallback(
    (
      params: StreamParams,
      listener: StreamListener,
      options: { preserveConnected?: boolean } = {}
    ) => {
      const entry = ensureStream(params, options)
      entry.listeners.add(listener)
      notifyStateSubscribers(entry.key)

      return () => {
        entry.listeners.delete(listener)
        if (entry.listeners.size === 0) {
          delete streamsRef.current[entry.key]
          clearHandoffState(entry)
          entry.connected = false
          entry.error = null
          notifyStateSubscribers(entry.key)
          ensureConnection({ preserveState: true })
        } else {
          notifyStateSubscribers(entry.key)
        }
      }
    },
    [clearHandoffState, ensureConnection, ensureStream, notifyStateSubscribers]
  )

  const getState = useCallback(
    (key: string | null) => {
      if (!key) {
        return undefined
      }

      const entry = streamsRef.current[key]
      if (!entry) {
        return undefined
      }

      const connection = connectionRef.current
      return {
        connected: entry.connected,
        error: entry.error,
        lastMeta: entry.lastMeta,
        retrying: connection.retryTimer !== undefined,
        retryAttempt: connection.retryAttempt,
        nextRetryAt: connection.nextRetryAt,
      }
    },
    []
  )

  const contextValue = useMemo<SyncStreamContextValue>(
    () => ({
      connect,
      getState,
      subscribe: subscribeToState,
    }),
    [connect, getState, subscribeToState]
  )

  useEffect(() => {
    return () => {
      closeConnection()
      Object.values(streamsRef.current).forEach(entry => {
        clearHandoffState(entry)
      })
      streamsRef.current = {}
    }
  }, [clearHandoffState, closeConnection])

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

  const [state, setState] = useState<StreamState>(() => {
    if (!enabled || !key) {
      return DEFAULT_STREAM_STATE
    }
    return context.getState(key) ?? DEFAULT_STREAM_STATE
  })

  const listenerRef = useRef<StreamListener | undefined>(onMessage)
  useEffect(() => {
    listenerRef.current = onMessage
  }, [onMessage])

  const lastStateRef = useRef<StreamState>(state)
  useEffect(() => {
    lastStateRef.current = state
  }, [state])

  const paramsRef = useRef<typeof params>(params)
  useEffect(() => {
    paramsRef.current = params
  }, [params])

  const previousParamsRef = useRef<StreamParams | null>(params ?? null)

  useEffect(() => {
    if (!enabled || !key || !paramsRef.current) {
      return
    }

    const nextParams = paramsRef.current
    const previousParams = previousParamsRef.current

    const canPreserve =
      previousParams !== null &&
      nextParams !== null &&
      previousParams.instanceId === nextParams.instanceId &&
      previousParams.page === nextParams.page &&
      previousParams.limit === nextParams.limit

    const shouldPreserve =
      canPreserve &&
      lastStateRef.current.connected &&
      !lastStateRef.current.error

    const connectOptions = shouldPreserve ? { preserveConnected: true } : undefined

    return context.connect(
      nextParams,
      payload => {
        listenerRef.current?.(payload)
      },
      connectOptions
    )
  }, [context, enabled, key])

  useEffect(() => {
    previousParamsRef.current = params ?? null
  }, [params])

  useEffect(() => {
    if (!enabled || !key) {
      setState(DEFAULT_STREAM_STATE)
      return
    }

    setState(context.getState(key) ?? DEFAULT_STREAM_STATE)

    return context.subscribe(key, snapshot => {
      setState(snapshot)
    })
  }, [context, enabled, key])

  return state
}

export function useSyncStreamManager(): SyncStreamContextValue {
  const context = useContext(SyncStreamContext)
  if (!context) {
    throw new Error("useSyncStreamManager must be used within a SyncStreamProvider")
  }
  return context
}

export function createStreamKey(params: StreamParams): string {
  return JSON.stringify({
    instanceId: params.instanceId,
    page: params.page,
    limit: params.limit,
    sort: params.sort,
    order: params.order,
    search: params.search ?? "",
    filters: params.filters ?? null,
  })
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
