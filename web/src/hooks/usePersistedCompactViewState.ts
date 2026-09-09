/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { TORRENT_VIEW_MODE_KEYS, useClientSetting } from "@/lib/client-settings"

const MOBILE_VIEW_MODES = ["normal", "compact", "ultra-compact"] as const
const DESKTOP_VIEW_MODES = ["normal", "dense", "compact"] as const

export type ViewMode = typeof MOBILE_VIEW_MODES[number] | typeof DESKTOP_VIEW_MODES[number]
export type ViewModeLayout = "mobile" | "desktop"

interface ViewModeSettings {
  storageKey: string
  defaultMode: ViewMode
  modes: readonly ViewMode[]
  parse: (raw: string) => ViewMode
}

function parseMobileViewMode(raw: string): ViewMode {
  if (MOBILE_VIEW_MODES.includes(raw as typeof MOBILE_VIEW_MODES[number])) return raw as ViewMode
  throw new Error("invalid mobile view mode")
}

function parseDesktopViewMode(raw: string): ViewMode {
  if (DESKTOP_VIEW_MODES.includes(raw as typeof DESKTOP_VIEW_MODES[number])) return raw as ViewMode
  throw new Error("invalid desktop view mode")
}

const SETTINGS: Record<ViewModeLayout, ViewModeSettings> = {
  mobile: {
    storageKey: TORRENT_VIEW_MODE_KEYS.mobile,
    defaultMode: "compact",
    modes: MOBILE_VIEW_MODES,
    parse: parseMobileViewMode,
  },
  desktop: {
    storageKey: TORRENT_VIEW_MODE_KEYS.desktop,
    defaultMode: "normal",
    modes: DESKTOP_VIEW_MODES,
    parse: parseDesktopViewMode,
  },
}

export function usePersistedCompactViewState(layout: ViewModeLayout) {
  const { storageKey, defaultMode, modes, parse } = SETTINGS[layout]
  const [viewMode, setStoredMode] = useClientSetting<ViewMode>(storageKey, {
    defaultValue: defaultMode,
    parse,
    serialize: String,
  })

  const setViewMode = useCallback(
    (requested: ViewMode) => {
      setStoredMode(modes.includes(requested) ? requested : defaultMode)
    },
    [defaultMode, modes, setStoredMode]
  )

  const cycleViewMode = useCallback(() => {
    setViewMode(modes[(modes.indexOf(viewMode) + 1) % modes.length])
  }, [modes, setViewMode, viewMode])

  return {
    viewMode,
    setViewMode,
    cycleViewMode,
    viewModes: modes,
  } as const
}
