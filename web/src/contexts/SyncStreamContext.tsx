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
  handoffTimer?: number
  handoffPending?: boolean
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

  const getSnapshot = useCallback(
    (key: string): StreamState => {
      const entry = streamsRef.current[key]
      if (!entry) {
        return DEFAULT_STREAM_STATE
      }

      return {
        connected: entry.connected,
        error: entry.error,
        lastMeta: entry.lastMeta,
        retrying: entry.retryTimer !== undefined,
        retryAttempt: entry.retryAttempt,
        nextRetryAt: entry.nextRetryAt,
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
    clearHandoffState(entry)
  }, [clearHandoffState])

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
    clearHandoffState(entry)
  }, [clearHandoffState])

  const openStream = useCallback(
    (
      entry: StreamEntry,
      options: { resetRetry?: boolean; preserveConnected?: boolean } = {}
    ) => {
      const resetRetry = options.resetRetry ?? false
      const preserveConnected = options.preserveConnected ?? false

      if (typeof window === "undefined" || typeof EventSource === "undefined") {
        entry.error = "Server-sent events are not supported in this environment"
        entry.connected = false
        clearRetryState(entry)
        notifyStateSubscribers(entry.key)
        return
      }

      if (resetRetry) {
        clearRetryState(entry)
      } else {
        clearHandoffState(entry)
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
        clearHandoffState(entry)

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

        notifyStateSubscribers(entry.key)
      }

      try {
        const source = new EventSource(url, { withCredentials: true })

        const payloadHandler = (event: MessageEvent) => {
          try {
            const payload = JSON.parse(event.data) as TorrentStreamPayload
            entry.lastMeta = payload.meta

            if (payload.type === "error" && payload.error) {
              entry.error = payload.error
            } else {
              entry.error = null
            }

            if (payload.type !== "error") {
              entry.connected = true
              entry.retryAttempt = 0
              entry.nextRetryAt = undefined
              clearHandoffState(entry)
            }

            entry.listeners.forEach(listener => listener(payload))
            notifyStateSubscribers(entry.key)
          } catch (err) {
            console.error("Failed to parse SSE payload", err)
          }
        }

        const networkErrorHandler = (_event?: Event) => {
          clearHandoffState(entry)
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

          clearHandoffState(entry)

          if (entry.retryTimer !== undefined) {
            window.clearTimeout(entry.retryTimer)
            entry.retryTimer = undefined
          }

          notifyStateSubscribers(entry.key)
        }
        source.onerror = networkErrorHandler

        entry.source = source
        entry.handlers = {
          payload: payloadHandler,
          networkError: networkErrorHandler,
        }

        if (preserveConnected) {
          entry.handoffPending = true
          if (entry.handoffTimer !== undefined) {
            window.clearTimeout(entry.handoffTimer)
          }
          entry.handoffTimer = window.setTimeout(() => {
            entry.handoffTimer = undefined
            if (!entry.handoffPending) {
              return
            }
            entry.handoffPending = false
            entry.connected = false
            if (resetRetry) {
              entry.error = null
            }
            notifyStateSubscribers(entry.key)
          }, HANDOFF_GRACE_PERIOD_MS)
          if (resetRetry) {
            entry.error = null
          }
          notifyStateSubscribers(entry.key)
        } else {
          entry.connected = false
          entry.handoffPending = false
          if (resetRetry) {
            entry.error = null
          }
          notifyStateSubscribers(entry.key)
        }
      } catch (err) {
        entry.connected = false
        entry.error = err instanceof Error ? err.message : "Failed to open stream"
        entry.retryAttempt = Math.min(entry.retryAttempt + 1, MAX_RETRY_ATTEMPTS)

        clearHandoffState(entry)

        const exponent = Math.max(0, entry.retryAttempt - 1)
        const delay = Math.min(RETRY_BASE_DELAY_MS * Math.pow(2, exponent), RETRY_MAX_DELAY_MS)

        entry.nextRetryAt = Date.now() + delay
        entry.retryTimer = window.setTimeout(() => {
          entry.retryTimer = undefined
          entry.nextRetryAt = undefined

          if (streamsRef.current[entry.key] !== entry || entry.listeners.size === 0) {
            notifyStateSubscribers(entry.key)
            return
          }

          openStream(entry)
        }, delay)

        notifyStateSubscribers(entry.key)
      }
    },
    [clearHandoffState, clearRetryState, closeStream, notifyStateSubscribers]
  )

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
          retryAttempt: 0,
        }
        streamsRef.current[key] = entry
        openStream(entry, {
          resetRetry: true,
          preserveConnected: options.preserveConnected,
        })
      } else if (!isSameParams(entry.params, params)) {
        closeStream(entry)
        entry.params = params
        openStream(entry, {
          resetRetry: true,
          preserveConnected: options.preserveConnected,
        })
      }

      return entry
    },
    [closeStream, openStream]
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
          closeStream(entry)
          clearRetryState(entry)
          delete streamsRef.current[entry.key]
        }
        notifyStateSubscribers(entry.key)
      }
    },
    [clearRetryState, closeStream, ensureStream, notifyStateSubscribers]
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
      subscribe: subscribeToState,
    }),
    [connect, getState, subscribeToState]
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
