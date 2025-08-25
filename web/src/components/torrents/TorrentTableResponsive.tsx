/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useState, } from "react"
import { TorrentTableOptimized, } from "./TorrentTableOptimized"
import { TorrentCardsMobile, } from "./TorrentCardsMobile"
import type { Torrent, } from "@/types"

interface TorrentTableResponsiveProps {
  instanceId: number
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: any, categories?: any, tags?: string[]) => void
  filterButton?: React.ReactNode
}

export function TorrentTableResponsive(props: TorrentTableResponsiveProps,) {
  const [isMobile, setIsMobile,] = useState(() => window.innerWidth < 768,)

  // Debounced resize/orientation handler
  useEffect(() => {
    // Use number for timeoutId in browser
    let timeoutId: number | null = null
    const checkMobile = () => setIsMobile(window.innerWidth < 768,)
    const handleResizeOrOrientation = () => {
      if (timeoutId) clearTimeout(timeoutId,)
      timeoutId = window.setTimeout(checkMobile, 100,)
    }
    window.addEventListener("resize", handleResizeOrOrientation,)
    window.addEventListener("orientationchange", handleResizeOrOrientation,)
    checkMobile()
    return () => {
      window.removeEventListener("resize", handleResizeOrOrientation,)
      window.removeEventListener("orientationchange", handleResizeOrOrientation,)
      if (timeoutId) clearTimeout(timeoutId,)
    }
  }, [],)

  // Media query for more accurate detection
  useEffect(() => {
    const mediaQuery = window.matchMedia("(max-width: 767px)",)
    const handleChange = (e: MediaQueryListEvent,) => setIsMobile(e.matches,)
    setIsMobile(mediaQuery.matches,)
    if (mediaQuery.addEventListener) {
      mediaQuery.addEventListener("change", handleChange,)
      return () => mediaQuery.removeEventListener("change", handleChange,)
    } else if (mediaQuery.addListener) {
      mediaQuery.addListener(handleChange,)
      return () => mediaQuery.removeListener(handleChange,)
    }
  }, [],)

  // Memoize props to avoid unnecessary re-renders
  const memoizedProps = props // If props are stable, this is fine; otherwise use useMemo

  if (isMobile) {
    return <TorrentCardsMobile {...memoizedProps} />
  }
  return <TorrentTableOptimized {...memoizedProps} />
}