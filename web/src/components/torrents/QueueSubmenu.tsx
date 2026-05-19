/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo } from "react"
import {
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuItem
} from "@/components/ui/context-menu"
import {
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuItem
} from "@/components/ui/dropdown-menu"
import { List, ChevronsUp, ArrowUp, ArrowDown, ChevronsDown } from "lucide-react"

interface QueueSubmenuProps {
  type: "context" | "dropdown"
  hashCount: number
  onQueueAction: (action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => void
  isPending?: boolean
}

export const QueueSubmenu = memo(function QueueSubmenu({
  type,
  hashCount,
  onQueueAction,
  isPending = false,
}: QueueSubmenuProps) {
  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <List className="mr-4 h-4 w-4" />
        Queue
      </SubTrigger>
      <SubContent>
        <MenuItem
          onClick={() => onQueueAction("topPriority")}
          disabled={isPending}
        >
          <ChevronsUp className="mr-2 h-4 w-4" />
          Top Priority {hashCount > 1 ? `(${hashCount})` : ""}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("increasePriority")}
          disabled={isPending}
        >
          <ArrowUp className="mr-2 h-4 w-4" />
          Increase Priority {hashCount > 1 ? `(${hashCount})` : ""}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("decreasePriority")}
          disabled={isPending}
        >
          <ArrowDown className="mr-2 h-4 w-4" />
          Decrease Priority {hashCount > 1 ? `(${hashCount})` : ""}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("bottomPriority")}
          disabled={isPending}
        >
          <ChevronsDown className="mr-2 h-4 w-4" />
          Bottom Priority {hashCount > 1 ? `(${hashCount})` : ""}
        </MenuItem>
      </SubContent>
    </Sub>
  )
})