/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FeatureNotice } from "@/components/feature-notice/FeatureNotice"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { isDemo } from "@/lib/demo"
import { cn } from "@/lib/utils"
import { Group, Ungroup } from "lucide-react"
import { useTranslation } from "react-i18next"

interface GroupCopiesToolbarProps {
  groupCopies: boolean
  onToggleGroupCopies: () => void
}

/** The Group copies switch as a toolbar button. The demo shows none. */
export function GroupCopiesToolbar({ groupCopies, onToggleGroupCopies }: GroupCopiesToolbarProps) {
  const { t } = useTranslation("torrents")
  if (isDemo) return null
  return (
    <Tooltip disableHoverableContent={true}>
      <FeatureNotice id="group-copies" onAction={onToggleGroupCopies}>
        <TooltipTrigger asChild onFocus={(e) => e.preventDefault()}>
          <Button
            variant="outline"
            size="icon"
            aria-pressed={groupCopies}
            aria-label={t("copyGroups.groupCopies")}
            className={cn(groupCopies && "border-primary/50 bg-primary/10 text-primary hover:bg-primary/15 hover:text-primary")}
            onClick={onToggleGroupCopies}
          >
            {groupCopies ? <Group className="h-4 w-4" /> : <Ungroup className="h-4 w-4" />}
          </Button>
        </TooltipTrigger>
      </FeatureNotice>
      <TooltipContent>{groupCopies ? t("copyGroups.ungroupCopies") : t("copyGroups.groupCopies")}</TooltipContent>
    </Tooltip>
  )
}
