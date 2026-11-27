/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api, type StreamSubscription } from "@/lib/api"
import { invalidateAllActivity, invalidateForActivity } from "@/lib/activity-invalidation"
import { applyStreamDelta, normalizeStreamedSnapshot } from "@/lib/cross-instance-torrents"
import { isClientConnectionErrorCode, STREAM_ERROR_DISCONNECTED, STREAM_ERROR_RETRY_EXHAUSTED, STREAM_ERROR_UNSUPPORTED } from "@/lib/sync-stream-state"
import type { ActivityEvent, ActivityStreamPayload, TorrentFilters, TorrentResponse, TorrentStreamMeta, TorrentStreamPayload, TorrentStreamVersion } from "@/types"
import { useQueryClient } from "@tanstack/react-query"
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react"

const RETRY_BASE_DELAY_MS = 4000
const RETRY_MAX_DELAY_MS = 30000
const MAX_RETRY_ATTEMPTS = 6
const HANDOFF_GRACE_PERIOD_MS = 1200
const ENTRY_TEARDOWN_DELAY_MS = 200

const STREAM_STALE_TIMEOUT_MS = 15000
const STREAM_DATA_STALE_TIMEOUT_MS = 120000

const STREAM_INITIAL_TIMEOUT_MS = 60000

export interface StreamParams {
  instanceId: number
  instanceIds?: number[]
  page: number
  limit: number
  sort: string
  order: "asc" | "desc"
  search?: string
  filters?: TorrentFilters
  summary?: boolean
  groupCopies?: boolean
  uniqueSize?: boolean
}

function normalizeInstanceIds(instanceIds?: number[]): number[] | undefined {
  if (!instanceIds || instanceIds.length === 0) {
    return undefined
  }
  const normalized = Array.from(new Set(instanceIds.filter(id => id > 0))).sort((a, b) => a - b)
  return normalized.length > 0 ? normalized : undefined
}

function isStreamVersion(value: TorrentStreamVersion | undefined): value is TorrentStreamVersion {
  return Boolean(
    value &&
    Number.isSafeInteger(value.major) &&
    value.major > 0 &&
    Number.isSafeInteger(value.minor) &&
    value.minor > 0
  )
}

function sameStreamVersion(a: TorrentStreamVersion | undefined, b: TorrentStreamVersion | undefined) {
  return Boolean(a && b && a.major === b.major && a.minor === b.minor)
}

type StreamListener = (payload: TorrentStreamPayload) => void

export interface StreamState {
  connected: boolean
  initialized: boolean
  dataStalled: boolean
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
  registerActivity: () => () => void
  subscribeActivity: (listener: (event: ActivityEvent) => void) => () => void
}

interface StreamEntry {
  key: string
  params: StreamParams
  listeners: Set<StreamListener>
  connected: boolean
  initialized: boolean
  dataStalled: boolean
  error: string | null
  lastMeta?: TorrentStreamMeta
  version?: TorrentStreamVersion
  snapshot?: TorrentResponse
  dataDeadline?: number
  dataTimer?: number
  handoffGraceUsed?: boolean
  sourceGeneration: number
  handoffTimer?: number
  handoffPending?: boolean
  handoffOpened?: boolean
  teardownTimer?: number
}

interface StreamConnection {
  recovering?: boolean
  source?: EventSource
  handlers?: {
    payload: (event: MessageEvent | Event) => void
    networkError: (event: Event) => void
    heartbeat: (event: MessageEvent | Event) => void
    activity: (event: MessageEvent | Event) => void
    connection: (event: MessageEvent | Event) => void
  }
  signature?: string
  connectionId?: string
  streams?: StreamSubscription[]
  activity?: boolean
  changes?: Promise<void>
  joining?: Set<string>
  retryAttempt: number
  retryTimer?: number
  nextRetryAt?: number
  staleTimer?: number
  firstEventSeen?: boolean
}

interface PendingConnectionUpdate {
  timer?: number
  preserveState?: boolean
  resetRetry?: boolean
  force?: boolean
}

const SyncStreamContext = createContext<SyncStreamContextValue | null>(null)

const DEFAULT_STREAM_STATE: StreamState = {
  connected: false,
  initialized: false,
  dataStalled: false,
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
  const ensureConnectionRef = useRef<(options?: { preserveState?: boolean; force?: boolean }) => void>(() => {})
  const requestRecoveryRef = useRef<(key: string) => void>(() => {})
  const resetStaleTimerRef = useRef<() => void>(() => {})
  const pendingConnectionUpdateRef = useRef<PendingConnectionUpdate | null>(null)
  const activityCountRef = useRef(0)
  const activityReconnectArmedRef = useRef(false)
  const activityListenersRef = useRef(new Set<(event: ActivityEvent) => void>())
  const queryClient = useQueryClient()
  const queryClientRef = useRef(queryClient)
  useEffect(() => {
    queryClientRef.current = queryClient
  }, [queryClient])
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
        initialized: entry.initialized,
        dataStalled: entry.dataStalled,
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

  const clearDataWatchdog = useCallback((entry: StreamEntry) => {
    if (entry.dataTimer === undefined) {
      return
    }
    if (typeof window !== "undefined") {
      window.clearTimeout(entry.dataTimer)
    } else {
      clearTimeout(entry.dataTimer)
    }
    entry.dataTimer = undefined
  }, [])

  const markDataStalled = useCallback(
    (entry: StreamEntry) => {
      if (
        streamsRef.current[entry.key] !== entry ||
        entry.dataStalled ||
        !entry.initialized ||
        entry.error
      ) {
        return
      }

      clearDataWatchdog(entry)
      entry.dataStalled = true
      notifyStateSubscribers(entry.key)
    },
    [clearDataWatchdog, notifyStateSubscribers]
  )

  const armDataWatchdog = useCallback(
    (entry: StreamEntry) => {
      clearDataWatchdog(entry)
      if (
        entry.dataDeadline === undefined ||
        !entry.initialized ||
        entry.error ||
        entry.dataStalled
      ) {
        return
      }

      const generation = entry.sourceGeneration
      const schedule = typeof window !== "undefined"
        ? window.setTimeout
        : (setTimeout as unknown as (handler: () => void, timeout: number) => number)
      entry.dataTimer = schedule(() => {
        entry.dataTimer = undefined
        if (entry.sourceGeneration !== generation) {
          return
        }
        markDataStalled(entry)
      }, Math.max(0, entry.dataDeadline - Date.now()))
    },
    [clearDataWatchdog, markDataStalled]
  )

  const acceptStreamData = useCallback(
    (entry: StreamEntry) => {
      entry.dataDeadline = Date.now() + STREAM_DATA_STALE_TIMEOUT_MS
      entry.handoffGraceUsed = false
      entry.dataStalled = false
      armDataWatchdog(entry)
    },
    [armDataWatchdog]
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
    entry.handoffOpened = false
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
    connection.recovering = false
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

      if (connection.staleTimer !== undefined) {
        if (typeof window !== "undefined") {
          window.clearTimeout(connection.staleTimer)
        } else {
          clearTimeout(connection.staleTimer)
        }
        connection.staleTimer = undefined
      }

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
        source.removeEventListener("delta", handlers.payload)
        source.removeEventListener("stream-error", handlers.payload)
        source.removeEventListener("heartbeat", handlers.heartbeat)
        source.removeEventListener("activity", handlers.activity)
        source.removeEventListener("connection", handlers.connection)
      }

      source.onopen = null
      source.onerror = null
      source.close()

      connection.source = undefined
      connection.handlers = undefined
      connection.signature = undefined
      connection.connectionId = undefined
      connection.streams = undefined
      connection.changes = undefined
      connection.joining = undefined

      if (!preserveRetry) {
        clearConnectionRetryState()
      }
    },
    [clearConnectionRetryState]
  )

  const buildStreamPayload = (entries: StreamEntry[]) =>
    entries
      .filter(entry => entry.params.instanceId > 0 || (normalizeInstanceIds(entry.params.instanceIds) !== undefined))
      .map(entry => ({
        key: entry.key,
        instanceId: entry.params.instanceId,
        instanceIds: normalizeInstanceIds(entry.params.instanceIds) ?? null,
        page: entry.params.page,
        limit: entry.params.limit,
        sort: entry.params.sort,
        order: entry.params.order,
        search: entry.params.search ?? "",
        filters: entry.params.filters ?? null,
        summary: entry.params.summary ?? false,
        groupCopies: entry.params.groupCopies,
        uniqueSize: entry.params.uniqueSize,
      }))
      .sort((a, b) => a.key.localeCompare(b.key))

  const openConnection = useCallback(
    (
      entries: StreamEntry[],
      options: { preserveState?: boolean; resetRetry?: boolean; force?: boolean } = {}
    ) => {
      const normalized = buildStreamPayload(entries)
      const connection = connectionRef.current
      const wantActivity = activityCountRef.current > 0

      if (normalized.length === 0 && !wantActivity) {
        entries.forEach(entry => {
          if (entry.connected) {
            entry.connected = false
          }
          entry.initialized = false
          entry.dataStalled = false
          clearDataWatchdog(entry)
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
        closeConnection()
        return
      }

      const signature = JSON.stringify({ streams: normalized, activity: wantActivity })

      if (connection.recovering && connection.retryTimer !== undefined) {
        return
      }

      if (!options.force && connection.signature === signature && connection.source) {
        return
      }

      if (!options.force && connection.source && connection.connectionId && connection.activity === wantActivity) {
        const { source, connectionId } = connection
        const previous = new Set(connection.streams?.map(stream => stream.key))
        const kept = normalized.filter(stream => streamsRef.current[stream.key]?.teardownTimer === undefined)
        const next = new Set(kept.map(stream => stream.key))
        const add = kept.filter(stream => !previous.has(stream.key))
        const remove = [...previous].filter(key => !next.has(key))
        connection.streams = kept
        connection.signature = JSON.stringify({ streams: kept, activity: wantActivity })
        if (add.length === 0 && remove.length === 0) {
          return
        }
        connection.joining = new Set([...(connection.joining ?? [])].filter(key => next.has(key)))
        add.forEach(stream => connection.joining?.add(stream.key))
        resetStaleTimerRef.current()
        connection.changes = (connection.changes ?? Promise.resolve()).then(async () => {
          if (connectionRef.current.source !== source) {
            return
          }
          const abort = new AbortController()
          const timer = window.setTimeout(
            () => abort.abort(new DOMException("The stream subscription change timed out.", "TimeoutError")),
            STREAM_INITIAL_TIMEOUT_MS
          )
          try {
            await api.changeStreamSubscriptions(connectionId, add, remove, abort.signal)
          } catch (err) {
            if (connectionRef.current.source === source) {
              console.warn("Stream subscription change failed, reopening the stream:", err)
              ensureConnectionRef.current({ preserveState: true, force: true })
            }
          } finally {
            window.clearTimeout(timer)
          }
        })
        return
      }

      const preserveState = options.preserveState ?? Boolean(connection.source)
      const resetRetry = options.resetRetry ?? false

      if (typeof window === "undefined" || typeof EventSource === "undefined") {
        entries.forEach(entry => {
          entry.connected = false
          entry.initialized = false
          entry.dataStalled = false
          clearDataWatchdog(entry)
          entry.error = STREAM_ERROR_UNSUPPORTED
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
          entry.handoffOpened = false
          if (entry.handoffTimer !== undefined) {
            if (typeof window !== "undefined") {
              window.clearTimeout(entry.handoffTimer)
            } else {
              clearTimeout(entry.handoffTimer)
            }
          }
          const timer = (typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number))(() => {
            entry.handoffTimer = undefined
            if (!entry.handoffPending) {
              return
            }
            if (entry.handoffOpened) {
              return
            }
            entry.handoffPending = false
            entry.connected = false
            entry.initialized = false
            entry.dataStalled = false
            clearDataWatchdog(entry)
            notifyStateSubscribers(entry.key)
          }, HANDOFF_GRACE_PERIOD_MS)
          entry.handoffTimer = timer
        })
      } else {
        entries.forEach(entry => {
          entry.connected = false
          entry.initialized = false
          entry.dataStalled = false
          clearDataWatchdog(entry)
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
      }

      const url = api.getTorrentsStreamBatchUrl(normalized, { activity: wantActivity })
      closeConnection({ preserveRetry: true })

      const handleNetworkError = () => {
        closeConnection({ preserveRetry: true })

        Object.values(streamsRef.current).forEach(entry => {
          clearHandoffState(entry)
          entry.error = STREAM_ERROR_DISCONNECTED
          entry.connected = false
          entry.initialized = false
          entry.dataStalled = false
          clearDataWatchdog(entry)
          notifyStateSubscribers(entry.key)
        })

        scheduleReconnectRef.current()
      }

      const handleSourceError = () => {
        const source = connectionRef.current.source
        if (source && source.readyState === EventSource.CONNECTING) {
          return
        }
        handleNetworkError()
      }

      const resetStaleTimer = () => {
        const conn = connectionRef.current
        if (conn.staleTimer !== undefined) {
          if (typeof window !== "undefined") {
            window.clearTimeout(conn.staleTimer)
          } else {
            clearTimeout(conn.staleTimer)
          }
          conn.staleTimer = undefined
        }
        if (typeof document !== "undefined" && document.visibilityState !== "visible") {
          return
        }
        const timeout = conn.firstEventSeen && !conn.joining?.size ? STREAM_STALE_TIMEOUT_MS : STREAM_INITIAL_TIMEOUT_MS
        const schedule = typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number)
        conn.staleTimer = schedule(() => {
          conn.staleTimer = undefined
          handleNetworkError()
        }, timeout)
      }
      resetStaleTimerRef.current = resetStaleTimer

      const payloadHandler = (event: MessageEvent | Event) => {
        connectionRef.current.firstEventSeen = true
        resetStaleTimer()
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
        if (connectionRef.current.joining?.delete(streamKey)) {
          resetStaleTimer()
        }

        const entry = streamsRef.current[streamKey]
        if (!entry) {
          return
        }

        entry.lastMeta = payload.meta

        if (payload.type === "stream-error" && payload.error) {
          entry.error = payload.error
          entry.connected = false
          entry.dataStalled = false
          clearDataWatchdog(entry)
        } else if (payload.type === "init" || payload.type === "update" || payload.type === "delta") {
          const nextVersion = payload.version
          const baseVersion = payload.delta?.baseVersion
          const validFullFrame =
            payload.type !== "delta" &&
            isStreamVersion(nextVersion)
          const validDeltaFrame =
            payload.type === "delta" &&
            isStreamVersion(nextVersion) &&
            isStreamVersion(baseVersion) &&
            entry.initialized &&
            sameStreamVersion(entry.version, baseVersion) &&
            nextVersion.major === baseVersion.major &&
            nextVersion.minor === baseVersion.minor + 1

          if (!validFullFrame && !validDeltaFrame) {
            entry.error = null
            entry.connected = true
            entry.initialized = false
            entry.dataStalled = false
            entry.version = undefined
            clearDataWatchdog(entry)
            clearHandoffState(entry)
            notifyStateSubscribers(streamKey)
            requestRecoveryRef.current(streamKey)
            return
          }

          const restoreSnapshot = payload.type === "delta" && Boolean(entry.error)
          entry.error = null
          entry.connected = true
          entry.initialized = true
          entry.version = nextVersion
          if (payload.data) {
            entry.snapshot = payload.type === "delta" && entry.snapshot
              ? applyStreamDelta(entry.snapshot, payload, Boolean(entry.params.instanceIds?.length)).data
              : normalizeStreamedSnapshot(payload.data)
          }
          if (restoreSnapshot && entry.snapshot) {
            payload = { ...payload, type: "init", data: entry.snapshot, delta: undefined }
          }
          if (connection.recovering && (connection.streams ?? normalized).every(({ key }) => {
            const candidate = streamsRef.current[key]
            return !candidate || candidate.initialized
          })) {
            clearConnectionRetryState()
          }
          acceptStreamData(entry)
        }

        clearHandoffState(entry)

        let listenerIndex = 0
        entry.listeners.forEach(listener => {
          const index = listenerIndex++
          try {
            listener(payload)
          } catch (listenerErr) {
            console.error(`SSE listener #${index} for stream "${streamKey}" failed:`, listenerErr)
          }
        })

        notifyStateSubscribers(streamKey)
      }

      const heartbeatHandler = () => {
        connectionRef.current.firstEventSeen = true
        resetStaleTimer()
      }

      const activityHandler = (event: MessageEvent | Event) => {
        connectionRef.current.firstEventSeen = true
        resetStaleTimer()
        if (!("data" in event)) {
          return
        }
        const rawData = typeof event.data === "string" ? event.data.trim() : ""
        if (rawData.length === 0) {
          return
        }

        let payload: ActivityStreamPayload
        try {
          payload = JSON.parse(rawData) as ActivityStreamPayload
        } catch (parseErr) {
          console.error("Failed to parse SSE activity payload JSON:", parseErr)
          return
        }

        if (payload.type !== "activity" || !payload.activity) {
          return
        }

        invalidateForActivity(queryClientRef.current, payload.activity)
        for (const listener of activityListenersRef.current) {
          listener(payload.activity)
        }
      }

      const connectionHandler = (event: MessageEvent | Event) => {
        resetStaleTimer()
        const rawData = "data" in event && typeof event.data === "string" ? event.data.trim() : ""
        if (rawData.length === 0) {
          return
        }
        try {
          const payload = JSON.parse(rawData) as { connectionId?: string }
          const conn = connectionRef.current
          const retried = conn.connectionId !== undefined
          conn.connectionId = payload.connectionId || undefined
          if (retried) {
            conn.streams = normalized
            conn.signature = signature
            ensureConnectionRef.current({ preserveState: true })
          }
        } catch (parseErr) {
          console.error("Failed to parse SSE connection payload JSON:", parseErr)
        }
      }

      normalized.forEach(({ key }) => {
        const entry = streamsRef.current[key]
        if (!entry) {
          return
        }
        entry.sourceGeneration += 1
        clearDataWatchdog(entry)
        if (!entry.handoffGraceUsed) {
          entry.dataDeadline = Date.now() + STREAM_DATA_STALE_TIMEOUT_MS
          entry.handoffGraceUsed = true
        }
        armDataWatchdog(entry)
      })

      const source = new EventSource(url, { withCredentials: true })
      source.addEventListener("init", payloadHandler)
      source.addEventListener("update", payloadHandler)
      source.addEventListener("delta", payloadHandler)
      source.addEventListener("stream-error", payloadHandler)
      source.addEventListener("heartbeat", heartbeatHandler)
      source.addEventListener("activity", activityHandler)
      source.addEventListener("connection", connectionHandler)
      source.onopen = () => {
        const openedAfterFailures = connection.retryAttempt > 0
        connection.firstEventSeen = false
        resetStaleTimer()
        if (!connection.recovering) {
          clearConnectionRetryState()
        }
        if (activityCountRef.current > 0) {
          if (activityReconnectArmedRef.current || openedAfterFailures) {
            invalidateAllActivity(queryClientRef.current)
          }
          activityReconnectArmedRef.current = true
        }
        normalized.forEach(({ key }) => {
          const entry = streamsRef.current[key]
          if (!entry) {
            return
          }
          if (entry.handoffPending) {
            entry.handoffOpened = true
            if (entry.handoffTimer !== undefined) {
              if (typeof window !== "undefined") {
                window.clearTimeout(entry.handoffTimer)
              } else {
                clearTimeout(entry.handoffTimer)
              }
              entry.handoffTimer = undefined
            }
          } else {
            entry.error = null
          }
          notifyStateSubscribers(key)
        })
      }
      source.onerror = handleSourceError

      connection.source = source
      connection.firstEventSeen = false
      connection.handlers = {
        payload: payloadHandler,
        networkError: handleNetworkError,
        heartbeat: heartbeatHandler,
        activity: activityHandler,
        connection: connectionHandler,
      }
      connection.signature = signature
      connection.streams = normalized
      connection.activity = wantActivity

      resetStaleTimer()
    },
    [acceptStreamData, armDataWatchdog, clearConnectionRetryState, clearDataWatchdog, clearHandoffState, closeConnection, notifyStateSubscribers]
  )

  const ensureConnection = useCallback(
    (options: { preserveState?: boolean; resetRetry?: boolean; force?: boolean } = {}) => {
      const entries = Object.values(streamsRef.current)
      if (entries.length === 0 && activityCountRef.current === 0) {
        closeConnection()
        clearConnectionRetryState()
        notifyAllStateSubscribers()
        return
      }

      openConnection(entries, options)
    },
    [clearConnectionRetryState, closeConnection, notifyAllStateSubscribers, openConnection]
  )
  ensureConnectionRef.current = ensureConnection

  const queueConnectionUpdate = useCallback(
    (options: { preserveState?: boolean; resetRetry?: boolean; force?: boolean } = {}) => {
      const pending: PendingConnectionUpdate =
        pendingConnectionUpdateRef.current ?? {
          timer: undefined,
          preserveState: undefined,
          resetRetry: undefined,
          force: undefined,
        }
      pending.preserveState = pending.preserveState || options.preserveState
      pending.resetRetry = pending.resetRetry || options.resetRetry
      pending.force = pending.force || options.force

      if (pending.timer === undefined) {
        const schedule =
          typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number)
        pending.timer = schedule(() => {
          const { preserveState, resetRetry, force } = pendingConnectionUpdateRef.current ?? {}
          pendingConnectionUpdateRef.current = null
          ensureConnection({
            preserveState,
            resetRetry,
            force,
          })
        }, 0)
      }

      pendingConnectionUpdateRef.current = pending
    },
    [ensureConnection]
  )

  const requestRecovery = useCallback(
    (key: string) => {
      if (!streamsRef.current[key]) {
        return
      }
      connectionRef.current.recovering = true
      closeConnection({ preserveRetry: true })
      Object.values(streamsRef.current).forEach(entry => {
        entry.connected = false
        entry.initialized = false
        entry.version = undefined
        if (entry.error && !isClientConnectionErrorCode(entry.error)) {
          entry.error = STREAM_ERROR_DISCONNECTED
        }
        clearHandoffState(entry)
      })
      notifyAllStateSubscribers()
      scheduleReconnectRef.current()
    },
    [clearHandoffState, closeConnection, notifyAllStateSubscribers]
  )
  requestRecoveryRef.current = requestRecovery

  const scheduleReconnect = useCallback(() => {
    const connection = connectionRef.current
    if (connection.retryTimer !== undefined) {
      return
    }

    connection.retryAttempt = Math.min(connection.retryAttempt + 1, MAX_RETRY_ATTEMPTS)

    if (connection.retryAttempt >= MAX_RETRY_ATTEMPTS) {
      Object.values(streamsRef.current).forEach(entry => {
        entry.error = STREAM_ERROR_RETRY_EXHAUSTED
        notifyStateSubscribers(entry.key)
      })
    }

    const exponent = Math.max(0, connection.retryAttempt - 1)
    const delay = Math.min(RETRY_BASE_DELAY_MS * Math.pow(2, exponent), RETRY_MAX_DELAY_MS)

    connection.nextRetryAt = Date.now() + delay

    const timer = (typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number))(() => {
      connection.retryTimer = undefined
      connection.nextRetryAt = undefined

      if (Object.keys(streamsRef.current).length === 0 && activityCountRef.current === 0) {
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
          initialized: false,
          dataStalled: false,
          error: null,
          sourceGeneration: 0,
        }
        streamsRef.current[key] = entry
        queueConnectionUpdate({ preserveState: true })
      } else if (!isSameParams(entry.params, params)) {
        entry.params = params
        entry.error = null
        entry.initialized = false
        entry.dataStalled = false
        entry.version = undefined
        clearDataWatchdog(entry)
        queueConnectionUpdate({ preserveState: true, resetRetry: true })
      } else {
        queueConnectionUpdate({ preserveState: true })
      }

      clearEntryTeardown(entry)
      return entry
    },
    [clearDataWatchdog, clearEntryTeardown, queueConnectionUpdate]
  )

  const scheduleEntryRemoval = useCallback(
    (entry: StreamEntry) => {
      clearEntryTeardown(entry)
      const schedule =
        typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number)

      const timer = schedule(() => {
        entry.teardownTimer = undefined
        delete streamsRef.current[entry.key]
        clearHandoffState(entry)
        entry.connected = false
        entry.initialized = false
        entry.dataStalled = false
        entry.version = undefined
        clearDataWatchdog(entry)
        entry.error = null
        notifyStateSubscribers(entry.key)
        queueConnectionUpdate({ preserveState: true })
      }, ENTRY_TEARDOWN_DELAY_MS)
      entry.teardownTimer = timer
    },
    [clearDataWatchdog, clearEntryTeardown, clearHandoffState, notifyStateSubscribers, queueConnectionUpdate]
  )

  const connect = useCallback(
    (
      params: StreamParams,
      listener: StreamListener,
      options: { preserveConnected?: boolean } = {}
    ) => {
      const entry = ensureStream(params, options)
      const needsInit = entry.connected || entry.initialized
      entry.listeners.add(listener)
      if (entry.initialized && entry.snapshot) {
        queueMicrotask(() => {
          if (!entry.listeners.has(listener) || !entry.initialized || !entry.snapshot || !entry.connected || entry.error) {
            return
          }
          try {
            listener({ type: "init", data: entry.snapshot, version: entry.version, meta: entry.lastMeta })
          } catch (err) {
            console.error("SyncStream snapshot listener failed", err)
          }
        })
      } else if (needsInit) {
        entry.initialized = false
        entry.dataStalled = false
        entry.version = undefined
        clearDataWatchdog(entry)
        const subscribed = connectionRef.current.streams?.some(stream => stream.key === entry.key) ?? false
        queueConnectionUpdate({ preserveState: true, force: subscribed })
      }
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
    [clearDataWatchdog, ensureStream, notifyStateSubscribers, queueConnectionUpdate, scheduleEntryRemoval]
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
        initialized: entry.initialized,
        dataStalled: entry.dataStalled,
        error: entry.error,
        lastMeta: entry.lastMeta,
        retrying: connection.retryTimer !== undefined,
        retryAttempt: connection.retryAttempt,
        nextRetryAt: connection.nextRetryAt,
      }
    },
    []
  )

  const registerActivity = useCallback(() => {
    activityCountRef.current += 1
    if (activityCountRef.current === 1) {
      queueConnectionUpdate({ preserveState: true })
    }

    return () => {
      activityCountRef.current = Math.max(0, activityCountRef.current - 1)
      if (activityCountRef.current === 0) {
        activityReconnectArmedRef.current = false
        queueConnectionUpdate({ preserveState: true })
      }
    }
  }, [queueConnectionUpdate])

  const subscribeActivity = useCallback((listener: (event: ActivityEvent) => void) => {
    activityListenersRef.current.add(listener)
    return () => {
      activityListenersRef.current.delete(listener)
    }
  }, [])

  const contextValue = useMemo<SyncStreamContextValue>(
    () => ({
      connect,
      getState,
      subscribe: subscribeToState,
      registerActivity,
      subscribeActivity,
    }),
    [connect, getState, subscribeToState, registerActivity, subscribeActivity]
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

  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    const handleVisibilityChange = () => {
      const connection = connectionRef.current

      if (document.visibilityState !== "visible") {
        if (connection.staleTimer !== undefined) {
          if (typeof window !== "undefined") {
            window.clearTimeout(connection.staleTimer)
          } else {
            clearTimeout(connection.staleTimer)
          }
          connection.staleTimer = undefined
        }
        return
      }

      const hasStreams = Object.keys(streamsRef.current).length > 0

      if (!hasStreams && activityCountRef.current === 0) {
        return
      }

      const source = connection.source
      const isDisconnected = !source || source.readyState === EventSource.CLOSED

      if (isDisconnected) {
        ensureConnection({ preserveState: false, resetRetry: true })
        return
      }

      resetStaleTimerRef.current()

      const now = Date.now()
      Object.values(streamsRef.current).forEach(entry => {
        if (!entry.initialized || entry.error || entry.dataStalled || entry.dataDeadline === undefined) {
          return
        }
        if (now >= entry.dataDeadline) {
          markDataStalled(entry)
        } else {
          armDataWatchdog(entry)
        }
      })
    }

    document.addEventListener("visibilitychange", handleVisibilityChange)
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange)
    }
  }, [armDataWatchdog, ensureConnection, markDataStalled])

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
        clearDataWatchdog(entry)
        clearEntryTeardown(entry)
        clearHandoffState(entry)
      })
      streamsRef.current = {}
    }
  }, [clearDataWatchdog, clearEntryTeardown, clearHandoffState, closeConnection])

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
      !lastStateRef.current.dataStalled &&
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

export function useActivityStream(enabled: boolean = true): void {
  const context = useContext(SyncStreamContext)
  if (!context) {
    throw new Error("useActivityStream must be used within a SyncStreamProvider")
  }

  const { registerActivity } = context

  useEffect(() => {
    if (!enabled) {
      return
    }
    return registerActivity()
  }, [enabled, registerActivity])
}

export function createStreamKey(params: StreamParams): string {
  const instanceIds = normalizeInstanceIds(params.instanceIds)
  try {
    return JSON.stringify({
      instanceId: params.instanceId,
      instanceIds: instanceIds ?? null,
      page: params.page,
      limit: params.limit,
      sort: params.sort,
      order: params.order,
      search: params.search ?? "",
      filters: params.filters ?? null,
      summary: params.summary ?? false,
      ...(params.groupCopies ? { groupCopies: true } : {}),
      ...(params.uniqueSize ? { uniqueSize: true } : {}),
    })
  } catch (err) {
    console.error("Failed to serialize stream params, using degraded key:", err, params)
    const idsKey = instanceIds ? instanceIds.join(",") : params.instanceId
    return `${idsKey}-${params.page}-${params.limit}-${params.sort}-${params.order}-${Date.now()}`
  }
}

function isSameParams(a: StreamParams, b: StreamParams): boolean {
  if (
    a.instanceId !== b.instanceId ||
    a.page !== b.page ||
    a.limit !== b.limit ||
    a.sort !== b.sort ||
    a.order !== b.order ||
    (a.search || "") !== (b.search || "") ||
    Boolean(a.groupCopies) !== Boolean(b.groupCopies) ||
    Boolean(a.uniqueSize) !== Boolean(b.uniqueSize)
  ) {
    return false
  }

  const aIds = normalizeInstanceIds(a.instanceIds)
  const bIds = normalizeInstanceIds(b.instanceIds)
  if ((aIds ? aIds.join(",") : "") !== (bIds ? bIds.join(",") : "")) {
    return false
  }

  const aFilters = a.filters ? JSON.stringify(a.filters) : ""
  const bFilters = b.filters ? JSON.stringify(b.filters) : ""
  return aFilters === bFilters
}
