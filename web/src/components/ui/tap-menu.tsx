/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { type ReactNode, useRef, useState } from "react"

/**
 * A menu that a tap on its row opens. Radix opens a dropdown on pointerdown, so
 * a finger that starts a scroll on the row would open it. For touch and pen it
 * opens on click instead: a browser fires no click after a scroll. A mouse
 * keeps the Radix behavior.
 */
export function TapMenu({ children, content }: { children: ReactNode; content: ReactNode }) {
  const [open, setOpen] = useState(false)
  const pointerType = useRef("")

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        asChild
        tabIndex={0}
        // A press and hold selects the page text on iOS
        className="cursor-pointer select-none"
        onPointerDown={(e) => {
          pointerType.current = e.pointerType
          if (e.pointerType !== "mouse") e.preventDefault()
        }}
        onClick={() => {
          if (pointerType.current !== "mouse") setOpen(true)
        }}
      >
        {/* Its own element: a row component need not pass the trigger props on */}
        <div>{children}</div>
      </DropdownMenuTrigger>
      <DropdownMenuContent>{content}</DropdownMenuContent>
    </DropdownMenu>
  )
}
