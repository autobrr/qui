/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { invalidateAllActivity, invalidateForActivity } from "@/lib/activity-invalidation"
import type { ActivityEvent, ActivityStreamPayload, TorrentFilters, TorrentStreamMeta, TorrentStreamPayload, TorrentStreamVersion } from "@/types"
import { useQueryClient } from "@tanstack/react-query"
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react"

const RETRY_BASE_DELAY_MS = 4000
const RETRY_MAX_DELAY_MS = 30000
const MAX_RETRY_ATTEMPTS = 6
const HANDOFF_GRACE_PERIOD_MS = 1200
const ENTRY_TEARDOWN_DELAY_MS = 200

// Client-side connection-state error codes. These are stable machine identifiers,
// not human-readable prose: the UI maps them to localized streamStatus.* copy so
// non-English locales never see English text. entry.error stays truthy for these
// so the existing polling-fallback gates keep treating the stream as unhealthy.
// Genuine backend payload errors (stream-error events) are stored verbatim because
// they are dynamic server text that cannot be translated client-side.
export const STREAM_ERROR_UNSUPPORTED = "client:eventsource-unsupported"
export const STREAM_ERROR_DISCONNECTED = "client:disconnected"
export const STREAM_ERROR_RETRY_EXHAUSTED = "client:retry-exhausted"

const CLIENT_CONNECTION_ERROR_CODES: ReadonlySet<string> = new Set([
  STREAM_ERROR_UNSUPPORTED,
  STREAM_ERROR_DISCONNECTED,
  STREAM_ERROR_RETRY_EXHAUSTED,
])

// isClientConnectionErrorCode reports whether a StreamState.error value is one of
// the client-side connection-state codes above (as opposed to backend payload
// error text). Components use it to decide whether to render the localized
// connection-status message or the raw backend error.
export function isClientConnectionErrorCode(error: string | null | undefined): boolean {
  return error != null && CLIENT_CONNECTION_ERROR_CODES.has(error)
}

// The backend emits a heartbeat every 5s. Once any event has arrived, if none
// follows within this window the connection is considered dead even when the
// browser still reports it open, so we force a reconnect.
const STREAM_STALE_TIMEOUT_MS = 15000

// Grace window applied until the FIRST event of a fresh connection arrives. The
// init snapshot is a single large SSE event (hundreds of KB on big instances), and
// SSE is ordered, so the 5s heartbeats are queued behind it and cannot arrive until
// it fully drains. A flat 15s watchdog therefore fires mid-init on a slow link,
// tears down the in-flight download, and reconnects into a fresh full init - a
// self-sustaining reconnect storm that saturates the (shared, h2) connection and
// starves every other request. This longer pre-first-event budget lets the init
// land; REST polling still serves data meanwhile (the stream isn't "connected"
// until its first event), and the normal 15s budget resumes once events flow.
const STREAM_INITIAL_TIMEOUT_MS = 60000

export interface StreamParams {
  // Single-instance subscription is keyed by instanceId. For an aggregated
  // (all-instances / cross-instance) subscription, set instanceId to 0 and provide
  // the concrete member ids in instanceIds.
  instanceId: number
  instanceIds?: number[]
  page: number
  limit: number
  sort: string
  order: "asc" | "desc"
  search?: string
  filters?: TorrentFilters
}

// normalizeInstanceIds returns a sorted, de-duplicated, positive-only copy used for
// stable stream keys and payloads (or undefined when there are no valid ids).
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
  // registerActivity keeps the multiplexed EventSource open to receive qui-owned
  // server activity events even when no torrent view is mounted. Returns an
  // unsubscribe that releases that interest.
  registerActivity: () => () => void
  // subscribeActivity delivers every server activity event to the listener, for
  // reactions that are not a query refetch (a toast, for example). Returns an
  // unsubscribe.
  subscribeActivity: (listener: (event: ActivityEvent) => void) => () => void
}

interface StreamEntry {
  key: string
  params: StreamParams
  listeners: Set<StreamListener>
  connected: boolean
  initialized: boolean
  error: string | null
  lastMeta?: TorrentStreamMeta
  version?: TorrentStreamVersion
  handoffTimer?: number
  handoffPending?: boolean
  // Set once the replacement connection's socket has demonstrably opened during a
  // view-parameter swap. A connection that has opened but is still awaiting a slow
  // first snapshot must not be declared offline, so the handoff timer skips the
  // flip when this is true and onopen clears the timer outright.
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
  }
  signature?: string
  retryAttempt: number
  retryTimer?: number
  nextRetryAt?: number
  staleTimer?: number
  // False until the first event of the current source arrives. While false the
  // watchdog uses the longer STREAM_INITIAL_TIMEOUT_MS so a large init has time to
  // drain; the first event flips it true and the normal STREAM_STALE_TIMEOUT_MS
  // applies thereafter. Reset to false whenever a new EventSource is created.
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
  const requestRecoveryRef = useRef<(key: string) => void>(() => {})
  // Re-armable handle to the live connection's stale watchdog. openConnection owns
  // resetStaleTimer (it closes over that connection's handlers); we mirror it here so
  // the visibility effect can re-arm a FRESH timer on tab refocus without reaching
  // into openConnection's closure. It always reads connectionRef.current internally.
  const resetStaleTimerRef = useRef<() => void>(() => {})
  const pendingConnectionUpdateRef = useRef<PendingConnectionUpdate | null>(null)
  // Count of active activity subscribers. While > 0 the EventSource stays open
  // (in activity-only mode if there are no torrent streams) so server events keep
  // flowing. When 0, behaviour is identical to before this feature.
  const activityCountRef = useRef(0)
  // Armed once an activity-capable connection has opened. A *subsequent* open is a
  // reconnect: events emitted while the stream was down were never replayed, so we
  // reconcile every activity-backed query. Reset when activity interest drops to 0
  // so the next fresh session's first open doesn't redundantly refetch.
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
      // Defense in depth: the backend rejects the entire multiplexed batch if an
      // entry has neither a positive instanceId nor a non-empty instanceIds list,
      // which would make the shared EventSource reconnect forever. Drop invalid
      // entries so one buggy consumer can't poison the stream.
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
        // No streamable entries (e.g. only invalid instanceId <= 0 entries) and no
        // activity interest. Tear the connection down instead of opening a doomed one
        // that the backend rejects and the client reconnects against forever.
        entries.forEach(entry => {
          if (entry.connected) {
            entry.connected = false
          }
          entry.initialized = false
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
        closeConnection()
        return
      }

      const signature = JSON.stringify({ streams: normalized, activity: wantActivity })

      // The scheduled recovery owns the next connection attempt. It reads live
      // subscriptions when it fires, so intervening mounts and view changes coalesce.
      if (connection.recovering && connection.retryTimer !== undefined) {
        return
      }

      if (!options.force && connection.signature === signature && connection.source) {
        return
      }

      const preserveState = options.preserveState ?? Boolean(connection.source)
      const resetRetry = options.resetRetry ?? false

      if (typeof window === "undefined" || typeof EventSource === "undefined") {
        entries.forEach(entry => {
          entry.connected = false
          entry.initialized = false
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
          // Fresh swap: the replacement socket has not opened yet.
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
            // The replacement socket opened during the grace window: keep the stream
            // connected and let the slow first snapshot (or the 15s stale watchdog)
            // resolve it, instead of flipping offline and re-enabling REST polling.
            if (entry.handoffOpened) {
              return
            }
            entry.handoffPending = false
            entry.connected = false
            entry.initialized = false
            notifyStateSubscribers(entry.key)
          }, HANDOFF_GRACE_PERIOD_MS)
          entry.handoffTimer = timer
        })
      } else {
        entries.forEach(entry => {
          entry.connected = false
          entry.initialized = false
          clearHandoffState(entry)
          notifyStateSubscribers(entry.key)
        })
      }

      const url = api.getTorrentsStreamBatchUrl(normalized, { activity: wantActivity })
      closeConnection({ preserveRetry: true })

      const handleNetworkError = (_event?: Event) => {
        closeConnection({ preserveRetry: true })

        Object.values(streamsRef.current).forEach(entry => {
          clearHandoffState(entry)
          if (!entry.error) {
            entry.error = STREAM_ERROR_DISCONNECTED
          }
          entry.connected = false
          entry.initialized = false
          notifyStateSubscribers(entry.key)
        })

        scheduleReconnectRef.current()
      }

      // The browser fires onerror on transient drops while it is already
      // auto-reconnecting (readyState stays CONNECTING; native retry is ~1-3s and
      // invisible). Calling closeConnection() here would source.close() and abort
      // that native retry, flipping the UI offline and re-enabling REST polling.
      // So: while CONNECTING, do nothing and let native retry proceed. Only escalate
      // (teardown + our own backoff) when the source is terminal/CLOSED or gone.
      // The 15s stale watchdog still escalates a connection stuck in CONNECTING.
      const handleSourceError = (event?: Event) => {
        const source = connectionRef.current.source
        if (source && source.readyState === EventSource.CONNECTING) {
          return
        }
        handleNetworkError(event)
      }

      // Watchdog: any inbound event (init/update/delta/stream-error/heartbeat) proves
      // the connection is alive and resets the timer. If it elapses, the connection is
      // treated as dead and reconnected even when the browser still reports it open.
      // Before the first event the longer STREAM_INITIAL_TIMEOUT_MS applies so a large
      // init has time to drain without being killed mid-download (see that constant).
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
        // While the tab is hidden, background timer throttling makes the watchdog
        // unreliable: incoming heartbeats keep calling this, so a re-armed timer
        // could false-fire (flipping the stream offline and re-enabling REST
        // polling in the background) or fire overdue on refocus and kill a healthy
        // connection. Stay disarmed while hidden; the visibilitychange handler
        // re-arms a fresh timer on refocus (or reconnects a closed source).
        if (typeof document !== "undefined" && document.visibilityState !== "visible") {
          return
        }
        const timeout = conn.firstEventSeen ? STREAM_STALE_TIMEOUT_MS : STREAM_INITIAL_TIMEOUT_MS
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

        const entry = streamsRef.current[streamKey]
        if (!entry) {
          return
        }

        entry.lastMeta = payload.meta

        if (payload.type === "stream-error" && payload.error) {
          entry.error = payload.error
          entry.connected = false
          entry.initialized = false
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
            entry.version = undefined
            clearHandoffState(entry)
            notifyStateSubscribers(streamKey)
            requestRecoveryRef.current(streamKey)
            return
          }

          entry.error = null
          entry.connected = true
          entry.initialized = true
          entry.version = nextVersion
          if (connection.recovering && normalized.every(({ key }) => {
            const candidate = streamsRef.current[key]
            return !candidate || candidate.initialized
          })) {
            clearConnectionRetryState()
          }
        }

        clearHandoffState(entry)

        // Notify listeners with individual error handling to prevent one failure from affecting others.
        // listeners is a Set, whose forEach yields the value as the second arg (not an index), so track it manually.
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

      // Heartbeats carry no torrent data; they exist solely to keep the watchdog alive.
      const heartbeatHandler = () => {
        connectionRef.current.firstEventSeen = true
        resetStaleTimer()
      }

      // Activity events are qui-owned server signals (not torrent data). They keep
      // the watchdog alive and invalidate the matching cached query so it refetches
      // on demand instead of polling.
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

      const source = new EventSource(url, { withCredentials: true })
      source.addEventListener("init", payloadHandler)
      source.addEventListener("update", payloadHandler)
      source.addEventListener("delta", payloadHandler)
      source.addEventListener("stream-error", payloadHandler)
      source.addEventListener("heartbeat", heartbeatHandler)
      source.addEventListener("activity", activityHandler)
      source.onopen = () => {
        // A non-zero retryAttempt means this open followed one or more failed
        // attempts, so events could have been missed before we ever connected -
        // reconcile even on the first successful open, not just on later reconnects.
        const openedAfterFailures = connection.retryAttempt > 0
        resetStaleTimer()
        // An open socket does not prove that rejected frames have recovered.
        // Keep the retry budget until every subscription accepts a baseline.
        if (!connection.recovering) {
          clearConnectionRetryState()
        }
        if (activityCountRef.current > 0) {
          if (activityReconnectArmedRef.current || openedAfterFailures) {
            // Reconnect (or a first open that followed failures): the previous
            // connection's per-session activity topic is gone, so any event published
            // while we were down was dropped. Idle feeds dropped their refetch
            // interval, so reconcile them all once now. Mounted queries refetch; the
            // rest are just marked stale.
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
            // The replacement socket has opened but its first snapshot may still be
            // in flight. Mark the handoff as opened and clear the fixed grace timer so
            // a slow init can no longer flip the stream offline; the stale watchdog
            // still guards a socket that opens but never delivers any event.
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
      // Fresh source: no event has arrived yet, so the watchdog grants the longer
      // init grace until the first one lands (see STREAM_INITIAL_TIMEOUT_MS).
      connection.firstEventSeen = false
      connection.handlers = {
        payload: payloadHandler,
        networkError: handleNetworkError,
        heartbeat: heartbeatHandler,
        activity: activityHandler,
      }
      connection.signature = signature

      // Arm the stale watchdog now, at connection creation - not only in onopen. A
      // connection whose server is unreachable from the start never fires onopen,
      // and handleSourceError deliberately defers to the browser's native retry
      // while readyState is CONNECTING. Without a watchdog armed here, such a
      // cold-start outage (or a replacement socket from a view swap that never
      // opens) would never escalate to qui's own backoff/offline state. The timer
      // is reset on every inbound event and on a successful open, so this only
      // fires if the connection produces nothing for STREAM_STALE_TIMEOUT_MS.
      resetStaleTimer()
    },
    [clearConnectionRetryState, clearHandoffState, closeConnection, notifyStateSubscribers]
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
      // Recovery stops the shared source, so no subscription has a live
      // baseline during backoff, including those that accepted their last frame.
      Object.values(streamsRef.current).forEach(entry => {
        entry.connected = false
        entry.initialized = false
        entry.version = undefined
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

    // Notify user when max retries reached
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
          error: null,
        }
        streamsRef.current[key] = entry
        queueConnectionUpdate({ preserveState: true })
      } else if (!isSameParams(entry.params, params)) {
        entry.params = params
        entry.error = null
        entry.initialized = false
        entry.version = undefined
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
        typeof window !== "undefined"? window.setTimeout: (setTimeout as unknown as (handler: () => void, timeout: number) => number)

      const timer = schedule(() => {
        entry.teardownTimer = undefined
        delete streamsRef.current[entry.key]
        clearHandoffState(entry)
        entry.connected = false
        entry.initialized = false
        entry.version = undefined
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
      const needsInit = entry.connected || entry.initialized
      entry.listeners.add(listener)
      if (needsInit) {
        entry.initialized = false
        entry.version = undefined
        queueConnectionUpdate({ preserveState: true, force: true })
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
    [ensureStream, notifyStateSubscribers, queueConnectionUpdate, scheduleEntryRemoval]
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
      // First subscriber: ensure the connection exists (opening it in activity-only
      // mode if no torrent view is mounted).
      queueConnectionUpdate({ preserveState: true })
    }

    return () => {
      activityCountRef.current = Math.max(0, activityCountRef.current - 1)
      if (activityCountRef.current === 0) {
        // Last subscriber gone: re-evaluate; the connection closes only if there are
        // also no torrent streams. Disarm reconnect reconciliation so a future fresh
        // activity session's first open doesn't refetch queries that just mounted.
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

  // Reconnect when tab becomes visible again
  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    const handleVisibilityChange = () => {
      const connection = connectionRef.current

      if (document.visibilityState !== "visible") {
        // Background tabs throttle timers, so the stale watchdog's remaining delay is
        // meaningless. Clear it now; we re-arm a fresh one on refocus. Otherwise an
        // overdue throttled timer could fire on return and kill a healthy connection.
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
        // Dead/closed source: reset retry state and force an immediate reconnection.
        ensureConnection({ preserveState: false, resetRetry: true })
        return
      }

      // Source is OPEN and healthy: re-arm a FRESH stale timer so an overdue
      // throttled watchdog cannot immediately declare the live connection dead.
      resetStaleTimerRef.current()
    }

    document.addEventListener("visibilitychange", handleVisibilityChange)
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange)
    }
  }, [ensureConnection])

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

// useActivityStream registers interest in qui-owned server activity events for
// the lifetime of the calling component. While at least one component is
// registered, the shared EventSource stays open (in activity-only mode if no
// torrent view is mounted) and incoming events invalidate the matching cached
// queries. Hooks that previously polled qui-owned state call this and drop their
// idle refetch interval.
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
    })
  } catch (err) {
    // Fallback for non-serializable filters - log for debugging
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
    (a.search || "") !== (b.search || "")
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
