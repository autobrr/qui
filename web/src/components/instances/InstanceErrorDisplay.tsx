/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AlertCircle, ChevronDown, KeyRound } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { formatErrorMessage } from "@/lib/format-error-message"
import type { InstanceResponse } from "@/types"

/** The errors of one instance, under its row: a saved password that qui cannot read, and the last connection errors. */
export function InstanceErrorDisplay({ instance, onEdit }: { instance: InstanceResponse; onEdit: () => void }) {
  const { t } = useTranslation("instances")
  const { formatTimestamp } = useDateTimeFormatters()
  const recentErrors = instance.connected ? [] : instance.recentErrors ?? []

  return (
    <div className="space-y-2.5">
      {instance.hasDecryptionError && (
        <div className="flex flex-col gap-2.5 sm:flex-row sm:items-center">
          <div className="flex min-w-0 flex-1 gap-2 text-sm">
            <KeyRound aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-destructive" />
            <div className="min-w-0">
              <p className="font-medium text-destructive">{t("errorDisplay.passwordRequired")}</p>
              <p className="text-xs text-muted-foreground">{t("errorDisplay.decryptionDescription")}</p>
            </div>
          </div>
          <Button variant="outline" size="sm" className="shrink-0 max-sm:h-10" onClick={onEdit}>
            {t("errorDisplay.reenterPassword")}
          </Button>
        </div>
      )}

      {recentErrors.length > 0 && (
        <Collapsible>
          <CollapsibleTrigger className="group -mx-1.5 flex min-h-8 w-[calc(100%+0.75rem)] items-center gap-2 rounded-md px-1.5 text-left text-sm text-destructive outline-none transition-colors hover:bg-destructive/10 focus-visible:ring-[3px] focus-visible:ring-ring/50 pointer-coarse:min-h-10">
            <AlertCircle aria-hidden="true" className="size-4 shrink-0" />
            <span className="flex-1 font-medium">{t("errorDisplay.recentErrors", { count: recentErrors.length })}</span>
            <ChevronDown aria-hidden="true" className="size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="mt-1.5 divide-y divide-destructive/15 rounded-md border border-destructive/20 bg-destructive/5">
              {recentErrors.map((error) => (
                <li key={error.id} className="space-y-1 px-3 py-2 text-xs">
                  <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                    <span className="font-medium text-destructive">{t([`errorDisplay.errorTypes.${error.errorType}`, "common:status.unknown"])}</span>
                    <span className="tabular-nums text-muted-foreground">{formatTimestamp(new Date(error.occurredAt).getTime() / 1000)}</span>
                  </div>
                  <p className="font-mono text-[11px] leading-relaxed text-foreground/80 [overflow-wrap:anywhere]">{formatErrorMessage(error.errorMessage)}</p>
                </li>
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  )
}
