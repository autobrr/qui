/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog } from "@/components/ui/dialog"
import { hasOnlyMod, latinKeyOf } from "@/lib/keyboard"
import { useEffect, useState } from "react"
import { CommandPaletteContent } from "./CommandPaletteContent"
import { onOpenCommandPalette } from "./open"

const OTHER_DIALOG = "[data-slot$=\"dialog-content\"]:not([data-command-palette]), [data-slot=\"sheet-content\"], [data-vaul-drawer]"

/** Opens on ⌘K (macOS) or Ctrl+K (Windows, Linux) on every page of the app, and closes on the same keys. A button opens it with `openCommandPalette`. */
export function CommandPalette() {
  // The text that the search field starts with. Null while the palette is closed.
  const [startText, setStartText] = useState<string | null>(null)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!hasOnlyMod(event) || latinKeyOf(event) !== "k") return
      event.preventDefault()
      // The palette changes the page, and a page change throws away the form of an open dialog.
      if (document.querySelector(OTHER_DIALOG)) return
      setStartText(current => (current === null ? "" : null))
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])
  useEffect(() => onOpenCommandPalette(setStartText), [])

  const close = () => setStartText(null)
  return (
    // Radix asks only for the close: on Escape, and on a press outside the palette.
    <Dialog open={startText !== null} onOpenChange={close}>
      {startText !== null && <CommandPaletteContent startText={startText} close={close} />}
    </Dialog>
  )
}
