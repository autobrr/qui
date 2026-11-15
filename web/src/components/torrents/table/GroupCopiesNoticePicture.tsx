/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import { ArrowRight, ChevronDown, File } from "lucide-react"
import { useTranslation } from "react-i18next"

function Row({ pill }: { pill?: string }) {
  return (
    <div className={cn("flex h-[18px] items-center gap-1.5 rounded-sm bg-background px-1.5 ring-1", pill ? "ring-foreground/30" : "ring-foreground/10")}>
      <File className="h-2.5 w-2.5 shrink-0 text-muted-foreground" />
      <span className="h-1 w-12 rounded-full bg-muted-foreground/50" />
      {pill && (
        <span className="-mr-0.5 ml-auto flex h-4 items-center gap-px rounded-md border bg-secondary pl-1 pr-0.5 text-[11px] font-medium leading-4 tabular-nums text-secondary-foreground">
          {pill}
          <ChevronDown className="h-2.5 w-2.5" />
        </span>
      )}
    </div>
  )
}

/** The picture of the Group copies notice: three copies of one torrent become one row with the "+2" pill. */
export function GroupCopiesNoticePicture() {
  const { t } = useTranslation("torrents")
  return (
    <div aria-hidden="true" className="flex items-center gap-2 rounded-md bg-foreground/[0.07] p-2">
      <div className="flex flex-1 flex-col gap-1">
        <Row />
        <Row />
        <Row />
      </div>
      <ArrowRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <div className="flex-1">
        <Row pill={t("copyGroups.badge", { hidden: 2 })} />
      </div>
    </div>
  )
}
