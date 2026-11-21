/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { ListX } from "lucide-react"
import { Children, type ReactNode, useEffect, useEffectEvent } from "react"
import { useTranslation } from "react-i18next"

interface SelectionBarProps {
  label: string
  summary: string
  extra?: ReactNode
  children?: ReactNode
  onClear: () => void
}

function keyBelongsElsewhere(event: KeyboardEvent) {
  const target = event.target instanceof Element ? event.target : null
  return event.defaultPrevented || event.isComposing || (target !== null && (
    target.tagName === "INPUT" ||
    target.tagName === "TEXTAREA" ||
    target.tagName === "SELECT" ||
    (target instanceof HTMLElement && target.isContentEditable) ||
    target.closest("[role=\"dialog\"], [role=\"alertdialog\"], [role=\"combobox\"]") !== null
  ))
}

export function SelectionBar({ label, summary, extra, children, onClear }: SelectionBarProps) {
  const { t } = useTranslation("common")
  const clearOnEscape = useEffectEvent((event: KeyboardEvent) => {
    if (event.key === "Escape" && !keyBelongsElsewhere(event)) onClear()
  })

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => clearOnEscape(event)
    document.addEventListener("keydown", onKeyDown)
    return () => document.removeEventListener("keydown", onKeyDown)
  }, [])

  return (
    <div
      role="region"
      aria-label={label}
      className="sticky bottom-0 z-10 flex flex-wrap items-center gap-x-3 gap-y-2 border-t bg-card px-4 py-2 shadow-[0_-8px_16px_-12px_rgb(0_0_0/0.5)] sm:pr-5 sm:pl-6"
    >
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3">
        <p aria-live="polite" className="truncate text-sm font-medium tabular-nums">{summary}</p>
        {extra}
      </div>
      {Children.toArray(children).length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 max-sm:order-1 max-sm:w-full max-sm:*:h-11 max-sm:*:flex-1 max-sm:*:basis-0">
          {children}
        </div>
      )}
      <Button type="button" variant="ghost" size="sm" className="text-muted-foreground max-sm:size-11 max-sm:px-0" onClick={onClear}>
        <ListX aria-hidden="true" />
        <span className="max-sm:sr-only">{t("kit.clearSelection")}</span>
      </Button>
    </div>
  )
}
