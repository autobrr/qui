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
