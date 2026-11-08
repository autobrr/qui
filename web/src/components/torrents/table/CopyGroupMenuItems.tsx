/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DropdownMenuCheckboxItem, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { ChevronsDownUp, ChevronsUpDown } from "lucide-react"
import { useTranslation } from "react-i18next"

interface CopyGroupMenuItemsProps {
  groupCopies: boolean
  expandAll: boolean
  onToggleGroupCopies: () => void
  onToggleExpandAll: () => void
}

// Both items keep the menu open, so the user sees Expand all appear after the switch.
const keepMenuOpen = (event: Event) => event.preventDefault()

export function CopyGroupMenuItems({ groupCopies, expandAll, onToggleGroupCopies, onToggleExpandAll }: CopyGroupMenuItemsProps) {
  const { t } = useTranslation("torrents")
  return (
    <>
      <DropdownMenuCheckboxItem
        checked={groupCopies}
        onCheckedChange={onToggleGroupCopies}
        onSelect={keepMenuOpen}
        className="text-sm"
      >
        {t("copyGroups.groupCopies")}
      </DropdownMenuCheckboxItem>
      {groupCopies && (
        <DropdownMenuItem onSelect={(event) => {
          keepMenuOpen(event)
          onToggleExpandAll()
        }} className="text-sm">
          {expandAll ? <ChevronsDownUp className="h-4 w-4" /> : <ChevronsUpDown className="h-4 w-4" />}
          {expandAll ? t("copyGroups.collapseAll") : t("copyGroups.expandAll")}
        </DropdownMenuItem>
      )}
    </>
  )
}
