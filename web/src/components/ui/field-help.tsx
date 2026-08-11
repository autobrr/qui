/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Info } from "lucide-react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

// Field help lives in a tooltip on the label rather than a paragraph under the
// control: forms carry enough controls that the prose made them scroll.
export function FieldHelp({ children }: { children: ReactNode }) {
  const { t } = useTranslation("common")

  return (
    <Tooltip>
      <TooltipTrigger aria-label={t("fieldHelp.trigger")} className="text-muted-foreground hover:text-foreground">
        <Info className="size-3.5" />
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">{children}</TooltipContent>
    </Tooltip>
  )
}
