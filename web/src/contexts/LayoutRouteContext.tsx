/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useCallback, useContext, useState } from "react"
import type { ReactNode } from "react"

type LayoutRouteState = {
  showInstanceControls: boolean
  instanceId: number | null
}

const DEFAULT_STATE: LayoutRouteState = {
  showInstanceControls: false,
  instanceId: null,
}

interface LayoutRouteContextValue {
  state: LayoutRouteState
  setLayoutRouteState: (next: LayoutRouteState) => void
  resetLayoutRouteState: () => void
}

const LayoutRouteContext = createContext<LayoutRouteContextValue | undefined>(undefined)

export function LayoutRouteProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<LayoutRouteState>(DEFAULT_STATE)

  const setLayoutRouteState = useCallback((next: LayoutRouteState) => {
    setState(next)
  }, [])

  const resetLayoutRouteState = useCallback(() => {
    setState(DEFAULT_STATE)
  }, [])

  return (
    <LayoutRouteContext.Provider value={{ state, setLayoutRouteState, resetLayoutRouteState }}>
      {children}
    </LayoutRouteContext.Provider>
  )
}

export function useLayoutRoute() {
  const context = useContext(LayoutRouteContext)
  if (!context) {
    throw new Error("useLayoutRoute must be used within a LayoutRouteProvider")
  }
  return context
}
