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
import { useTranslation } from "react-i18next"

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
  const { t } = useTranslation("torrents")
  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent
  const MenuItem = type === "context" ? ContextMenuItem : DropdownMenuItem

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <List className="mr-4 h-4 w-4" />
        {t("queueSubmenu.queue")}
      </SubTrigger>
      <SubContent>
        <MenuItem
          onClick={() => onQueueAction("topPriority")}
          disabled={isPending}
        >
          <ChevronsUp className="mr-2 h-4 w-4" />
          {hashCount > 1 ? t("queueSubmenu.topPriorityBatch", { count: hashCount }) : t("queueSubmenu.topPriority")}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("increasePriority")}
          disabled={isPending}
        >
          <ArrowUp className="mr-2 h-4 w-4" />
          {hashCount > 1 ? t("queueSubmenu.increasePriorityBatch", { count: hashCount }) : t("queueSubmenu.increasePriority")}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("decreasePriority")}
          disabled={isPending}
        >
          <ArrowDown className="mr-2 h-4 w-4" />
          {hashCount > 1 ? t("queueSubmenu.decreasePriorityBatch", { count: hashCount }) : t("queueSubmenu.decreasePriority")}
        </MenuItem>
        <MenuItem
          onClick={() => onQueueAction("bottomPriority")}
          disabled={isPending}
        >
          <ChevronsDown className="mr-2 h-4 w-4" />
          {hashCount > 1 ? t("queueSubmenu.bottomPriorityBatch", { count: hashCount }) : t("queueSubmenu.bottomPriority")}
        </MenuItem>
      </SubContent>
    </Sub>
  )
})
