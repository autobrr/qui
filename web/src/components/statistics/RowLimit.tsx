/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { useRef } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"

interface RowLimitProps {
  shown: number
  total: number
  all: boolean
  onAllChange: (all: boolean) => void
}

/** The line below a phone table that shows some of its rows: the count, and Show all or Show fewer. Show fewer keeps the line in view. */
export function RowLimit({ shown, total, all, onAllChange }: RowLimitProps) {
  const { t, i18n } = useTranslation("statistics")
  const line = useRef<HTMLDivElement>(null)
  const formatCount = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language)

  return (
    <div ref={line} className="flex items-center justify-between gap-2 border-t px-4 pt-3 text-xs text-muted-foreground">
      <span>{t("table.shown", { shown: formatCount.format(shown), total: formatCount.format(total) })}</span>
      {all ? (
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            flushSync(() => onAllChange(false))
            // Safari keeps the scroll offset when the list above gets shorter, so the button moves out of view.
            line.current?.scrollIntoView({ block: "nearest" })
          }}
        >
          {t("table.showFewer")}
        </Button>
      ) : <Button variant="outline" size="sm" onClick={() => onAllChange(true)}>{t("table.showAll")}</Button>}
    </div>
  )
}
