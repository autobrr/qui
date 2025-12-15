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
const ENTRY_TEARDOWN_DELAY_MS = 200

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
  teardownTimer?: number
}

interface StreamConnection {
  source?: EventSource
  handlers?: {
    payload: (event: MessageEvent | Event) => void
    networkError: (event: Event) => void
  }
  signature?: string
  retryAttempt: number
  retryTimer?: number
  nextRetryAt?: number
}

interface PendingConnectionUpdate {
  timer?: number
  preserveState?: boolean
  resetRetry?: boolean
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
  const pendingConnectionUpdateRef = useRef<PendingConnectionUpdate | null>(null)
  const clearEntryTeardown = useCallback((entry: StreamEntry) => {
    if (entry.teardownTimer === undefined) {
      return
    }
    if (typeof window !== "undefined") {
      window.clearTimeout(entry.teardownTimer)
    } else {
      clearTimeout(entry.teardownTimer)
    }
    entry.teardownTimer = undefined
  }, [])

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
        source.removeEventListener("stream-error", handlers.payload)
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

      const payloadHandler = (event: MessageEvent | Event) => {
        if (!("data" in event)) {
          return
        }
        const rawData = typeof event.data === "string" ? event.data.trim() : ""
        if (rawData.length === 0) {
          return
        }

        let payload: TorrentStreamPayload
        try {
          payload = JSON.parse(rawData) as TorrentStreamPayload
        } catch (parseErr) {
          console.error("Failed to parse SSE payload JSON:", parseErr, "raw data:", rawData.substring(0, 200))
          return
        }

        const streamKey = payload.meta?.streamKey
        if (!streamKey) {
          return
        }

        const entry = streamsRef.current[streamKey]
        if (!entry) {
          return
        }

        entry.lastMeta = payload.meta

        if (payload.type === "stream-error" && payload.error) {
          entry.error = payload.error
          entry.connected = false
        } else {
          entry.error = null
          entry.connected = true
        }

        clearHandoffState(entry)

        // Notify listeners with individual error handling to prevent one failure from affecting others
        entry.listeners.forEach((listener, index) => {
          try {
            listener(payload)
          } catch (listenerErr) {
            console.error(`SSE listener #${index} for stream "${streamKey}" failed:`, listenerErr)
          }
        })

        notifyStateSubscribers(streamKey)
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
      source.addEventListener("stream-error", payloadHandler)
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

  const queueConnectionUpdate = useCallback(
    (options: { preserveState?: boolean; resetRetry?: boolean } = {}) => {
      const pending: PendingConnectionUpdate =
        pendingConnectionUpdateRef.current ?? {
          timer: undefined,
          preserveState: undefined,
          resetRetry: undefined,
        }
      pending.preserveState = pending.preserveState || options.preserveState
      pending.resetRetry = pending.resetRetry || options.resetRetry

      if (pending.timer === undefined) {
        const schedule =
          typeof window !== "undefined"
            ? window.setTimeout
            : (setTimeout as unknown as (handler: () => void, timeout: number) => number)
        pending.timer = schedule(() => {
          const { preserveState, resetRetry } = pendingConnectionUpdateRef.current ?? {}
          pendingConnectionUpdateRef.current = null
          ensureConnection({
            preserveState,
            resetRetry,
          })
        }, 0)
      }

      pendingConnectionUpdateRef.current = pending
    },
    [ensureConnection]
  )

  const scheduleReconnect = useCallback(() => {
    const connection = connectionRef.current
    if (connection.retryTimer !== undefined) {
      return
    }

    connection.retryAttempt = Math.min(connection.retryAttempt + 1, MAX_RETRY_ATTEMPTS)

    // Notify user when max retries reached
    if (connection.retryAttempt >= MAX_RETRY_ATTEMPTS) {
      Object.values(streamsRef.current).forEach(entry => {
        entry.error = "Connection failed repeatedly. Check your network or server status."
        notifyStateSubscribers(entry.key)
      })
    }

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
  }, [clearConnectionRetryState, ensureConnection, notifyAllStateSubscribers, notifyStateSubscribers])

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
        queueConnectionUpdate({ preserveState: true })
      } else if (!isSameParams(entry.params, params)) {
        entry.params = params
        entry.error = null
        queueConnectionUpdate({ preserveState: true, resetRetry: true })
      } else {
        queueConnectionUpdate({ preserveState: true })
      }

      clearEntryTeardown(entry)
      return entry
    },
    [clearEntryTeardown, queueConnectionUpdate]
  )

  const scheduleEntryRemoval = useCallback(
    (entry: StreamEntry) => {
      clearEntryTeardown(entry)
      const schedule =
        typeof window !== "undefined"
          ? window.setTimeout
          : (setTimeout as unknown as (handler: () => void, timeout: number) => number)

      const timer = schedule(() => {
        entry.teardownTimer = undefined
        delete streamsRef.current[entry.key]
        clearHandoffState(entry)
        entry.connected = false
        entry.error = null
        notifyStateSubscribers(entry.key)
        queueConnectionUpdate({ preserveState: true })
      }, ENTRY_TEARDOWN_DELAY_MS)
      entry.teardownTimer = timer
    },
    [clearEntryTeardown, clearHandoffState, notifyStateSubscribers, queueConnectionUpdate]
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
          scheduleEntryRemoval(entry)
        } else {
          notifyStateSubscribers(entry.key)
        }
      }
    },
    [ensureStream, notifyStateSubscribers, scheduleEntryRemoval]
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
    if (typeof window === "undefined") {
      return
    }

    const handleBeforeUnload = () => {
      closeConnection()
    }

    window.addEventListener("beforeunload", handleBeforeUnload)
    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload)
    }
  }, [closeConnection])

  // Reconnect when tab becomes visible again
  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState !== "visible") {
        return
      }

      const connection = connectionRef.current
      const hasStreams = Object.keys(streamsRef.current).length > 0

      if (!hasStreams) {
        return
      }

      // Check if connection is dead or disconnected
      const source = connection.source
      const isDisconnected = !source || source.readyState === EventSource.CLOSED

      if (isDisconnected) {
        // Reset retry state and force immediate reconnection
        clearConnectionRetryState()
        ensureConnection({ preserveState: false, resetRetry: true })
      }
    }

    document.addEventListener("visibilitychange", handleVisibilityChange)
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange)
    }
  }, [clearConnectionRetryState, ensureConnection])

  useEffect(() => {
    return () => {
      const pending = pendingConnectionUpdateRef.current
      if (pending?.timer !== undefined) {
        if (typeof window !== "undefined") {
          window.clearTimeout(pending.timer)
        } else {
          clearTimeout(pending.timer)
        }
      }
      pendingConnectionUpdateRef.current = null
      closeConnection()
      Object.values(streamsRef.current).forEach(entry => {
        clearEntryTeardown(entry)
        clearHandoffState(entry)
      })
      streamsRef.current = {}
    }
  }, [clearEntryTeardown, clearHandoffState, closeConnection])

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
  try {
    return JSON.stringify({
      instanceId: params.instanceId,
      page: params.page,
      limit: params.limit,
      sort: params.sort,
      order: params.order,
      search: params.search ?? "",
      filters: params.filters ?? null,
    })
  } catch (err) {
    // Fallback for non-serializable filters - log for debugging
    console.error("Failed to serialize stream params, using degraded key:", err, params)
    return `${params.instanceId}-${params.page}-${params.limit}-${params.sort}-${params.order}-${Date.now()}`
  }
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
