/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"

interface SelectAllHotkeyProps {
  onSelectAll: () => void
  enabled?: boolean
  isMac?: boolean
}

/**
 * Registers a global keyboard listener that invokes `onSelectAll` when the user presses Ctrl/Cmd + A outside editable fields and certain widgets.
 *
 * The listener is active only when `enabled` is true. Platform detection for the Command key can be forced via `isMac`; otherwise it is inferred from the user agent when available.
 *
 * @param onSelectAll - Callback invoked when the select-all hotkey is triggered.
 * @param enabled - Whether the hotkey listener is active. Defaults to `true`.
 * @param isMac - Optional override to treat the platform as macOS (affects whether `metaKey` is considered the modifier).
 * @returns `null` (this component renders nothing)
 */
export function SelectAllHotkey({
  onSelectAll,
  enabled = true,
  isMac,
}: SelectAllHotkeyProps) {
  useEffect(() => {
    if (!enabled) {
      return
    }

    const platformIsMac =
      typeof isMac === "boolean"
        ? isMac
        : typeof window !== "undefined" &&
          /Mac|iPhone|iPad|iPod/.test(window.navigator.userAgent)

    const handleSelectAllHotkey = (event: KeyboardEvent) => {
      if (event.key !== "a" && event.key !== "A") {
        return
      }

      const target = event.target
      const elementTarget = target instanceof Element ? target : null

      if (
        elementTarget &&
        (elementTarget.tagName === "INPUT" ||
          elementTarget.tagName === "TEXTAREA" ||
          elementTarget.tagName === "SELECT" ||
          elementTarget instanceof HTMLElement && elementTarget.isContentEditable ||
          elementTarget.closest("[role=\"dialog\"]") ||
          elementTarget.closest("[role=\"combobox\"]"))
      ) {
        return
      }

      if (platformIsMac && event.ctrlKey && !event.metaKey) {
        return
      }

      const usesSelectModifier = platformIsMac ? event.metaKey : event.ctrlKey
      if (!usesSelectModifier) {
        return
      }

      event.preventDefault()
      event.stopPropagation()
      onSelectAll()
    }

    window.addEventListener("keydown", handleSelectAllHotkey)

    return () => {
      window.removeEventListener("keydown", handleSelectAllHotkey)
    }
  }, [onSelectAll, enabled, isMac])

  return null
}
