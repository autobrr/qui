/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import type { TransferBulkAction } from "@/lib/transfers-api"
import { cn } from "@/lib/utils"
import { ListX, Loader2, RotateCcw, Trash2, X, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

const icons: Record<TransferBulkAction, LucideIcon> = { retry: RotateCcw, cancel: X, remove: Trash2 }

// A phone fits two buttons on a line. A third one takes the next line, so no button leaves the screen.
const phoneButton = "max-sm:h-11 max-sm:min-w-[calc(50%-0.1875rem)] max-sm:flex-1"

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

/** The steps for the picked rows. The bar floats over the list, so the rows do not move when it shows. */
export function TransferSelectionBar({ count, wholeChip, more, actions, running, onPickAll, onClear, onAction }: TransferSelectionBarProps) {
  const { t } = useTranslation("transfers")

  return (
    <div className="sticky bottom-4 z-10 max-lg:bottom-[calc(4.75rem+env(safe-area-inset-bottom))]">
      <div
        role="region"
        aria-label={t("bulk.label")}
        className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-popover py-2 pr-2 pl-4 text-popover-foreground shadow-lg max-sm:gap-y-2 max-sm:pl-3"
      >
        <div className="flex min-w-0 flex-wrap items-center gap-x-3">
          <p aria-live="polite" className="text-sm font-medium tabular-nums">
            {wholeChip ? t("bulk.wholeChip", { count }) : t("bulk.picked", { count })}
          </p>
          {more !== null && (
            <Button variant="link" size="sm" className="h-auto px-0 py-1 max-sm:min-h-11" onClick={onPickAll}>
              {t("bulk.pickAll", { count: more })}
            </Button>
          )}
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-1.5 max-sm:w-full">
          {actions.map(action => {
            const Icon = icons[action]
            return (
              <Button
                key={action}
                variant="outline"
                size="sm"
                className={cn(phoneButton, action === "remove" && "text-destructive hover:text-destructive")}
                disabled={running !== null}
                onClick={() => onAction(action)}
              >
                {running === action ? <Loader2 aria-hidden="true" className="animate-spin" /> : <Icon aria-hidden="true" />}
                {t(`actions.${action}`)}
              </Button>
            )
          })}
          {/* The X of the bar would read as the X of Cancel, so the selection clears with its own mark. */}
          <Button variant="ghost" size="sm" className={cn("text-muted-foreground", phoneButton)} onClick={onClear}>
            <ListX aria-hidden="true" />
            {t("bulk.clear")}
          </Button>
        </div>
      </div>
    </div>
  )
}
