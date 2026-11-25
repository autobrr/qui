/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { ChevronLeft, ChevronRight, Loader2 } from "lucide-react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

export const PAGE_ROWS = 10

interface PagerProps {
  page: number
  total: number
  waiting?: boolean
  onPageChange: (page: number) => void
  children?: ReactNode
}

export function Pager({ page, total, waiting = false, onPageChange, children }: PagerProps) {
  const { t, i18n } = useTranslation("statistics")
  const formatCount = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language)
  const first = page * PAGE_ROWS
  const last = Math.min(first + PAGE_ROWS, total)

  return (
    <div className="flex items-center justify-between gap-2 border-t px-6 pt-3 text-xs text-muted-foreground">
      <span className="tabular-nums">{t("table.range", { from: formatCount.format(first + 1), to: formatCount.format(last), total: formatCount.format(total) })}</span>
      {children}
      {total > PAGE_ROWS && (
        <div className="flex gap-2">
          <Button variant="outline" size="icon" className="size-8" aria-label={t("table.previousPage")} disabled={page === 0} onClick={() => onPageChange(page - 1)}>
            <ChevronLeft />
          </Button>
          <Button variant="outline" size="icon" className="size-8" aria-label={t("table.nextPage")} disabled={waiting || last >= total} onClick={() => onPageChange(page + 1)}>
            {waiting ? <Loader2 className="animate-spin" /> : <ChevronRight />}
          </Button>
        </div>
      )}
    </div>
  )
}
