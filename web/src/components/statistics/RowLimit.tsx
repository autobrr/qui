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
  // Adds rows. Undefined when the line has no Show more.
  onMore?: () => void
  // Shows each row. Undefined when the line has no Show all.
  onAll?: () => void
  // Returns to the first rows. Undefined while only the first rows show.
  onFewer?: () => void
}

/** The line below a table that shows some of its rows: the count, and Show more, Show all, or Show fewer. Show fewer keeps the line in view. */
export function RowLimit({ shown, total, onMore, onAll, onFewer }: RowLimitProps) {
  const { t, i18n } = useTranslation("statistics")
  const line = useRef<HTMLDivElement>(null)
  const formatCount = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language)

  return (
    <div ref={line} className="flex items-center justify-between gap-2 border-t px-4 pt-3 text-xs sm:px-6 text-muted-foreground">
      <span>{t("table.shown", { shown: formatCount.format(shown), total: formatCount.format(total) })}</span>
      <div className="flex gap-2">
        {onFewer && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              flushSync(onFewer)
              // Safari keeps the scroll offset when the list above gets shorter, so the button moves out of view.
              line.current?.scrollIntoView({ block: "nearest" })
            }}
          >
            {t("table.showFewer")}
          </Button>
        )}
        {onMore && <Button variant="outline" size="sm" onClick={onMore}>{t("table.showMore")}</Button>}
        {onAll && <Button variant="outline" size="sm" onClick={onAll}>{t("table.showAll")}</Button>}
      </div>
    </div>
  )
}
