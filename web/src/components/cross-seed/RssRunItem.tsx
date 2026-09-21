/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import type { CrossSeedRun } from "@/types"
import { AlertTriangle, CheckCircle2, ChevronDown, Clock, Loader2, XCircle } from "lucide-react"
import { useTranslation } from "react-i18next"

interface RSSRunItemProps {
  run: CrossSeedRun
  formatDateValue: (date: string | undefined) => string
}

/** Single RSS run item - used for scheduled, manual, and other run lists */
export function RSSRunItem({ run, formatDateValue }: RSSRunItemProps) {
  const { t } = useTranslation("crossseed")
  const hasResults = run.results && run.results.length > 0
  const successResults = run.results?.filter(r => r.success) ?? []
  const failedResults = run.results?.filter(r => !r.success && r.message) ?? []

  return (
    <Collapsible>
      <CollapsibleTrigger asChild disabled={!hasResults}>
        <div className={`flex items-center justify-between gap-2 p-2 rounded bg-muted/30 text-sm ${hasResults ? "hover:bg-muted/50 cursor-pointer" : ""}`}>
          <div className="flex items-center gap-2 min-w-0">
            {run.status === "success" && <CheckCircle2 className="h-3 w-3 text-primary shrink-0" />}
            {run.status === "running" && <Loader2 className="h-3 w-3 animate-spin text-yellow-500 shrink-0" />}
            {run.status === "failed" && <XCircle className="h-3 w-3 text-destructive shrink-0" />}
            {run.status === "partial" && <AlertTriangle className="h-3 w-3 text-yellow-500 shrink-0" />}
            {run.status === "pending" && <Clock className="h-3 w-3 text-muted-foreground shrink-0" />}
            <span className="text-xs text-muted-foreground">{t("automation.items", { count: run.totalFeedItems })}</span>
            {run.errorMessage && (
              <span className="min-w-0 truncate text-xs text-muted-foreground" title={run.errorMessage}>
                {t("automation.runError", { message: run.errorMessage })}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Badge variant="secondary" className="text-xs">{t("scan.crossSeedsAddedBadge", { count: run.crossSeedsAdded })}</Badge>
            {run.candidatesFailed > 0 && (
              <Badge variant="destructive" className="text-xs">{t("automation.failedCount", { count: run.candidatesFailed })}</Badge>
            )}
            <span className="text-xs text-muted-foreground">{formatDateValue(run.startedAt)}</span>
            {hasResults && <ChevronDown className="h-3 w-3 text-muted-foreground" />}
          </div>
        </div>
      </CollapsibleTrigger>
      {hasResults && (
        <CollapsibleContent>
          <div className="pl-5 pr-2 py-2 space-y-1 border-l-2 border-muted ml-1.5 mt-1 max-h-48 overflow-y-auto">
            {successResults.map((result, i) => (
              <div key={`${result.instanceId}-${i}`} className="flex items-center gap-2 text-xs">
                <Badge variant="default" className="text-[10px] shrink-0 w-20 justify-center truncate" title={result.instanceName}>{result.instanceName}</Badge>
                {result.indexerName && (
                  <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName}</Badge>
                )}
                <span className="truncate text-muted-foreground">{result.matchedTorrentName}</span>
              </div>
            ))}
            {successResults.length === 0 && failedResults.length === 0 && run.results && run.results.length > 0 && (
              <span className="text-xs text-muted-foreground">{t("scan.noResultsWithDetails")}</span>
            )}
            {failedResults.length > 0 && (
              <div className="mt-2 pt-2 border-t border-border/50 space-y-1">
                <span className="text-[10px] text-muted-foreground font-medium">{t("scan.failed")}</span>
                {failedResults.map((result, i) => (
                  <div key={`failed-${result.instanceId}-${i}`} className="flex flex-col gap-0.5 text-xs">
                    <div className="flex items-center gap-2">
                      <Badge variant="destructive" className="text-[10px] shrink-0 w-20 justify-center truncate" title={result.instanceName}>{result.instanceName}</Badge>
                      {result.indexerName && (
                        <Badge variant="secondary" className="text-[10px] shrink-0 w-24 justify-center truncate" title={result.indexerName}>{result.indexerName}</Badge>
                      )}
                    </div>
                    <span className="text-muted-foreground pl-1">{result.message}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </CollapsibleContent>
      )}
    </Collapsible>
  )
}
