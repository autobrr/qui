/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { getTrackerStatusBadge } from "@/lib/tracker-utils"
import type { AutomationUnregisteredFacts } from "@/types"

interface UnregisteredDeleteFactsProps {
  facts: AutomationUnregisteredFacts
}

export function UnregisteredDeleteFacts({ facts }: UnregisteredDeleteFactsProps) {
  const { t } = useTranslation("instances")
  const { formatISOTimestamp } = useDateTimeFormatters()

  return (
    <div className="flex flex-col gap-1 text-xs bg-muted/40 p-2 rounded">
      {facts.since && (
        <span>{t("preferences.unregisteredDeleteFacts.since", { time: formatISOTimestamp(facts.since) })}</span>
      )}
      {facts.host && (
        <div className="flex items-center gap-1.5 flex-wrap">
          <span>{t("preferences.unregisteredDeleteFacts.tracker", { host: facts.host })}</span>
          {facts.status !== undefined && getTrackerStatusBadge(facts.status, true)}
        </div>
      )}
      {facts.message && (
        <span className="break-words">{t("preferences.unregisteredDeleteFacts.message", { message: facts.message })}</span>
      )}
    </div>
  )
}
