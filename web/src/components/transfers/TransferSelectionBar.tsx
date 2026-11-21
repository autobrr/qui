/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SelectionBar } from "@/components/kit/SelectionBar"
import { Button } from "@/components/ui/button"
import type { TransferBulkAction } from "@/lib/transfers-api"
import { cn } from "@/lib/utils"
import { Loader2, RotateCcw, Trash2, X, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

const icons: Record<TransferBulkAction, LucideIcon> = { retry: RotateCcw, cancel: X, remove: Trash2 }

interface TransferSelectionBarProps {
  count: number
  /** The selection holds every row behind the chip, also the rows of the other pages. */
  wholeChip: boolean
  /** The number of rows behind the chip, when the whole page is picked and more rows exist. The bar then offers them all. */
  more: number | null
  actions: readonly TransferBulkAction[]
  /** The step that waits for its answer: the bar takes no other step until then. */
  running: TransferBulkAction | null
  onPickAll: () => void
  onClear: () => void
  onAction: (action: TransferBulkAction) => void
}

export function TransferSelectionBar({ count, wholeChip, more, actions, running, onPickAll, onClear, onAction }: TransferSelectionBarProps) {
  const { t } = useTranslation("transfers")

  return (
    <SelectionBar
      label={t("bulk.label")}
      summary={wholeChip ? t("bulk.wholeChip", { count }) : t("bulk.picked", { count })}
      extra={more !== null && (
        <Button variant="link" size="sm" className="h-auto px-0 py-1 max-sm:min-h-11" onClick={onPickAll}>
          {t("bulk.pickAll", { count: more })}
        </Button>
      )}
      onClear={onClear}
    >
      {actions.map(action => {
        const Icon = icons[action]
        return (
          <Button
            key={action}
            variant="outline"
            size="sm"
            className={cn(action === "remove" && "text-destructive hover:text-destructive")}
            disabled={running !== null}
            onClick={() => onAction(action)}
          >
            {running === action ? <Loader2 aria-hidden="true" className="animate-spin" /> : <Icon aria-hidden="true" />}
            {t(`actions.${action}`)}
          </Button>
        )
      })}
    </SelectionBar>
  )
}
