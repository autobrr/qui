/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ServerState } from "@/types"
import { useMemo, useRef } from "react"

export interface UseEffectiveServerStateParams {
  instanceId: number
  serverState: ServerState | null | undefined
}

/**
 * Ref-caches the most recent server state per instance so transient gaps in the
 * data stream don't blank out the footer/status displays. When `serverState` is
 * missing for the current instance, the last known value is returned; switching
 * instances clears the cache to null.
 */
export function useEffectiveServerState({ instanceId, serverState }: UseEffectiveServerStateParams): ServerState | null {
  const serverStateRef = useRef<{ instanceId: number, state: ServerState | null }>({
    instanceId,
    state: null,
  })

  return useMemo(() => {
    const cached = serverStateRef.current
    const instanceChanged = cached.instanceId !== instanceId

    if (serverState != null) {
      serverStateRef.current = { instanceId, state: serverState }
      return serverState
    }

    if (serverState === null) {
      serverStateRef.current = { instanceId, state: null }
      return null
    }

    if (instanceChanged) {
      serverStateRef.current = { instanceId, state: null }
      return null
    }

    return cached.state
  }, [serverState, instanceId])
}
