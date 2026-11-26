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

export function CommandPalette() {
  const [startText, setStartText] = useState<string | null>(null)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!hasOnlyMod(event) || latinKeyOf(event) !== "k") return
      event.preventDefault()
      if (document.querySelector(OTHER_DIALOG)) return
      setStartText(current => (current === null ? "" : null))
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])
  useEffect(() => onOpenCommandPalette(setStartText), [])

  const close = () => setStartText(null)
  return (
    <Dialog open={startText !== null} onOpenChange={close}>
      {startText !== null && <CommandPaletteContent startText={startText} close={close} />}
    </Dialog>
  )
}
