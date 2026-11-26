/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { type ReactNode, useRef, useState } from "react"

export function TapMenu({ children, content }: { children: ReactNode; content: ReactNode }) {
  const [open, setOpen] = useState(false)
  const pointerType = useRef("")

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        asChild
        tabIndex={0}
        className="cursor-pointer select-none"
        onPointerDown={(e) => {
          pointerType.current = e.pointerType
          if (e.pointerType !== "mouse") e.preventDefault()
        }}
        onClick={() => {
          if (pointerType.current !== "mouse") setOpen(true)
        }}
      >
        <div>{children}</div>
      </DropdownMenuTrigger>
      <DropdownMenuContent>{content}</DropdownMenuContent>
    </DropdownMenu>
  )
}
