/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"

interface SelectAllHotkeyProps {
  onSelectAll: () => void
  enabled?: boolean
  isMac?: boolean
}

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
      const usesSelectModifier = event.ctrlKey || (platformIsMac && event.metaKey)
      if (!usesSelectModifier) {
        return
      }

      if (event.key !== "a" && event.key !== "A") {
        return
      }

      const target = event.target
      if (!(target instanceof HTMLElement)) {
        return
      }

      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.tagName === "SELECT" ||
        target.isContentEditable ||
        target.closest("[role=\"dialog\"]") ||
        target.closest("[role=\"combobox\"]")
      ) {
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
