/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext, useState } from "react"
import type { ReactNode } from "react"

interface TorrentSelectionContextType {
  isSelectionMode: boolean
  setIsSelectionMode: (value: boolean) => void
}

const TorrentSelectionContext = createContext<TorrentSelectionContextType | undefined>(undefined)

export function TorrentSelectionProvider({ children }: { children: ReactNode }) {
  const [isSelectionMode, setIsSelectionMode] = useState(false)

  return (
    <TorrentSelectionContext.Provider value={{ isSelectionMode, setIsSelectionMode }}>
      {children}
    </TorrentSelectionContext.Provider>
  )
}

export function useTorrentSelection() {
  const context = useContext(TorrentSelectionContext)
  if (context === undefined) {
    throw new Error("useTorrentSelection must be used within a TorrentSelectionProvider")
  }
  return context
}