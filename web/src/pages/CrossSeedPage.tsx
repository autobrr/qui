/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { BlocklistTab } from "@/components/cross-seed/BlocklistTab"
import { CompletionTab } from "@/components/cross-seed/CompletionTab"
import {
  DEFAULT_RSS_INTERVAL_MINUTES,
  useActiveInstances,
  useCrossSeedSearchSettings,
  useCrossSeedSearchStatus,
  useCrossSeedSettings,
  useCrossSeedStatus,
  useEnabledIndexers,
  useFormatDateValue,
  useManualRunCooldown
} from "@/components/cross-seed/cross-seed-settings"
import { DirScanTab } from "@/components/cross-seed/DirScanTab"
import { LibraryTab } from "@/components/cross-seed/LibraryTab"
import { RssTab } from "@/components/cross-seed/RssTab"
import { RulesTab } from "@/components/cross-seed/RulesTab"
import { SeasonPacksTab } from "@/components/cross-seed/SeasonPacksTab"
import { CROSS_SEED_NAV_GROUPS, type CrossSeedTab } from "@/components/cross-seed/tabs"
import { WebhookTab } from "@/components/cross-seed/WebhookTab"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { api } from "@/lib/api"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AlertTriangle } from "lucide-react"
import { useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"

interface CrossSeedPageProps {
  activeTab: CrossSeedTab
  onTabChange: (tab: CrossSeedTab) => void
}

export function CrossSeedPage({ activeTab, onTabChange }: CrossSeedPageProps) {
  const { t } = useTranslation("crossseed")
  const formatDateValue = useFormatDateValue()

  // Keep the shared SSE stream open so qui activity events drive cache invalidation.
  useActivityStream()

  const { data: settings } = useCrossSeedSettings()
  const { data: status } = useCrossSeedStatus()
  const { data: searchSettings } = useCrossSeedSearchSettings()
  const { instances } = useActiveInstances()
  const hasEnabledIndexers = useEnabledIndexers().length > 0

  const { data: searchStatus } = useCrossSeedSearchStatus()

  const searchInstanceId = searchSettings?.instanceId ?? null
  const { data: searchRuns } = useQuery({
    queryKey: ["cross-seed", "search-runs", searchInstanceId],
    queryFn: () => searchInstanceId ? api.listCrossSeedSearchRuns(searchInstanceId, { limit: 10 }) : Promise.resolve([]),
    enabled: !!searchInstanceId,
  })

  const automationEnabled = settings?.enabled ?? false
  const automationRunning = status?.running ?? false
  const latestRun = status?.lastRun
  const cooldown = useManualRunCooldown(settings?.runIntervalMinutes ?? DEFAULT_RSS_INTERVAL_MINUTES, latestRun?.startedAt)

  const searchRunning = searchStatus?.running ?? false
  const activeSearchRun = searchStatus?.run

  const currentSearchInstanceName = useMemo(() => {
    const id = searchRunning && activeSearchRun ? activeSearchRun.instanceId : searchInstanceId
    const name = instances?.find(instance => instance.id === id)?.name
    if (name) return name
    if (searchRunning && activeSearchRun) return t("scan.instanceFallback", { id: activeSearchRun.instanceId })
    return t("scan.noInstanceSelected")
  }, [activeSearchRun, instances, searchInstanceId, searchRunning, t])

  const searchRunStats = useMemo(() => ({
    totalRuns: searchRuns?.length ?? 0,
    totalAdded: (searchRuns ?? []).reduce((sum, run) => sum + run.crossSeedsAdded, 0),
  }), [searchRuns])

  const automationStatusLabel = automationRunning ? t("scan.runningUpper") : automationEnabled ? t("automation.scheduledUpper") : t("overview.rssAutomation.disabledUpper")
  const automationStatusVariant: "default" | "secondary" | "destructive" =
    automationRunning ? "default" : automationEnabled ? "secondary" : "destructive"

  const handleOpenGazelleSettings = useCallback(() => {
    onTabChange("rules")
    window.setTimeout(() => {
      document.getElementById("gazelle-settings")?.scrollIntoView({ behavior: "smooth", block: "start" })
    }, 50)
  }, [onTabChange])

  return (
    <div className="space-y-6 p-4 lg:p-6 pb-16">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t("pageTitle")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("pageDescription")}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Badge variant={automationEnabled ? "default" : "secondary"}>
            {t(automationEnabled ? "automationOnBadge" : "automationOffBadge")}
          </Badge>
        </div>
      </div>

      {!hasEnabledIndexers && (
        <Alert className="border-border rounded-xl bg-card">
          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400" />
          <AlertTitle>{t("indexersMissing.title")}</AlertTitle>
          <AlertDescription className="space-y-1">
            <p>{t("indexersMissing.description")}</p>
            <p>
              <Link to="/settings" search={{ tab: "indexers" }} className="font-medium text-primary underline-offset-4 hover:underline">
                {t("indexersMissing.manageLink")}
              </Link>{" "}
              {t("indexersMissing.linkSuffix")}
            </p>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 md:grid-cols-2 mb-6">
        <Card className="h-full">
          <CardHeader className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <CardTitle className="text-base">{t("overview.rssAutomation.title")}</CardTitle>
              <Badge variant={automationStatusVariant}>
                {automationStatusLabel}
              </Badge>
            </div>
            <CardDescription>{t("overview.rssAutomation.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.rssAutomation.nextRun")}</span>
              <span className="font-medium">
                {automationEnabled ? status?.nextRunAt ? formatDateValue(status.nextRunAt) : "—" : t("overview.rssAutomation.disabled")}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.rssAutomation.manualTrigger")}</span>
              <span className="font-medium">{cooldown.active ? t("overview.rssAutomation.cooldown", { display: cooldown.display }) : t("overview.rssAutomation.ready")}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.rssAutomation.lastRun")}</span>
              <span className="font-medium">
                {latestRun ? `${t(`dirScan.statusLabelsUpper.${latestRun.status}`, latestRun.status)} • ${formatDateValue(latestRun.startedAt)}` : t("overview.rssAutomation.noRunsYet")}
              </span>
            </div>
          </CardContent>
        </Card>

        <Card className="h-full">
          <CardHeader className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <CardTitle className="text-base">{t("overview.seededSearch.title")}</CardTitle>
              <Badge variant={searchRunning ? "default" : "secondary"}>{searchRunning ? t("scan.runningUpper") : t("scan.idleUpper")}</Badge>
            </div>
            <CardDescription>{t("overview.seededSearch.description")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.seededSearch.instance")}</span>
              <span className="font-medium truncate text-right max-w-[180px]">{currentSearchInstanceName}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.seededSearch.recentRuns")}</span>
              <span className="font-medium">{t("scan.runSummary", { runs: searchRunStats.totalRuns, added: searchRunStats.totalAdded })}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t("overview.seededSearch.now")}</span>
              <span className="font-medium">
                {searchRunning ? activeSearchRun ? t("scan.scannedProgress", { processed: activeSearchRun.processed, total: activeSearchRun.totalTorrents ?? "?" }) : t("overview.seededSearch.running") : t("overview.seededSearch.idle")}
              </span>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="md:hidden">
        <Select value={activeTab} onValueChange={(value) => onTabChange(value as CrossSeedTab)}>
          <SelectTrigger className="w-full min-h-11" aria-label={t("pageTitle")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {CROSS_SEED_NAV_GROUPS.map(group => (
              <SelectGroup key={group.tabs[0]}>
                {"labelKey" in group && <SelectLabel>{t(group.labelKey)}</SelectLabel>}
                {group.tabs.map(tab => (
                  <SelectItem key={tab} value={tab}>{t(`tabs.${tab}`)}</SelectItem>
                ))}
              </SelectGroup>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex flex-col gap-6 md:flex-row">
        <nav className="hidden w-56 shrink-0 space-y-4 md:block" aria-label={t("pageTitle")}>
          {CROSS_SEED_NAV_GROUPS.map(group => (
            <div key={group.tabs[0]} className="space-y-1">
              {"labelKey" in group && (
                <p className="px-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t(group.labelKey)}</p>
              )}
              {group.tabs.map(tab => (
                <button
                  key={tab}
                  type="button"
                  onClick={() => onTabChange(tab)}
                  aria-current={activeTab === tab ? "page" : undefined}
                  className={cn(
                    "w-full rounded-md px-3 py-2 text-left text-sm font-medium transition-colors",
                    activeTab === tab ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                  )}
                >
                  {t(`tabs.${tab}`)}
                </button>
              ))}
            </div>
          ))}
        </nav>

        <div className="min-w-0 flex-1 space-y-6">
          {activeTab === "rss" && <RssTab />}
          {activeTab === "webhook" && <WebhookTab />}
          {activeTab === "completion" && <CompletionTab />}
          {activeTab === "library" && <LibraryTab onOpenGazelleSettings={handleOpenGazelleSettings} />}
          {activeTab === "directories" && <DirScanTab instances={instances ?? []} />}
          {activeTab === "season-packs" && <SeasonPacksTab />}
          {activeTab === "rules" && <RulesTab />}
          {activeTab === "blocklist" && <BlocklistTab instances={instances ?? []} />}
        </div>
      </div>
    </div>
  )
}
