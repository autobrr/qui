/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DropdownMenuCheckboxItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { isDemo } from "@/lib/demo"
import { useTranslation } from "react-i18next"

interface CopyGroupMenuItemsProps {
  groupCopies: boolean
  onToggleGroupCopies: () => void
}

/** The Group copies switch, with a separator under it. The demo shows none. */
export function CopyGroupMenuItems({ groupCopies, onToggleGroupCopies }: CopyGroupMenuItemsProps) {
  const { t } = useTranslation("torrents")
  if (isDemo) return null
  return (
    <>
      <DropdownMenuCheckboxItem checked={groupCopies} onCheckedChange={onToggleGroupCopies} className="text-sm">
        {t("copyGroups.groupCopies")}
      </DropdownMenuCheckboxItem>
      <DropdownMenuSeparator />
    </>
  )
}
