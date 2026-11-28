/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import type { UnregisteredDeleteWarning } from "@/types"

const SHOWN_TRACKERS = 3

interface RecentlyUnregisteredWarningProps {
  warning: UnregisteredDeleteWarning | null
  total: number
}

export function RecentlyUnregisteredWarning({ warning, total }: RecentlyUnregisteredWarningProps) {
  const { t } = useTranslation("torrents")
  const [incognitoMode] = useIncognitoMode()
  if (!warning) return null

  const summary = warning.count >= total
    ? t("unregisteredWarning.all", { count: total })
    : t("unregisteredWarning.some", { count: total, matched: warning.count })

  return (
    <Alert variant="warning">
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{t("unregisteredWarning.title")}</AlertTitle>
      <AlertDescription className="space-y-1">
        <p>{summary}</p>
        <p>{t("unregisteredWarning.earliest", { since: formatRelativeTime(warning.since) })}</p>
        <ul className="list-disc pl-4">
          {warning.trackers.slice(0, SHOWN_TRACKERS).map(tracker => (
            <li key={tracker.host}>
              {t("unregisteredWarning.tracker", { host: incognitoMode ? getLinuxTrackerDomain(tracker.host) : tracker.host, count: tracker.count })}
            </li>
          ))}
          {warning.trackers.length > SHOWN_TRACKERS && (
            <li>{t("unregisteredWarning.moreTrackers", { count: warning.trackers.length - SHOWN_TRACKERS })}</li>
          )}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
