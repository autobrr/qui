/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AfterInjectionTab } from "@/components/cross-seed/AfterInjectionTab"
import { BlocklistTab } from "@/components/cross-seed/BlocklistTab"
import { CategoriesTab } from "@/components/cross-seed/CategoriesTab"
import { CompletionTab } from "@/components/cross-seed/CompletionTab"
import { DirScanTab } from "@/components/cross-seed/DirScanTab"
import { LibraryTab } from "@/components/cross-seed/LibraryTab"
import { MatchingTab } from "@/components/cross-seed/MatchingTab"
import { RssTab } from "@/components/cross-seed/RssTab"
import { SeasonPacksTab } from "@/components/cross-seed/SeasonPacksTab"
import { CROSS_SEED_NAV_GROUPS, type CrossSeedTab } from "@/components/cross-seed/tabs"
import { WebhookTab } from "@/components/cross-seed/WebhookTab"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useActiveInstances, useCrossSeedSettings, useEnabledIndexers } from "@/hooks/useCrossSeedSettings"
import { cn } from "@/lib/utils"
import { Link } from "@tanstack/react-router"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"

interface CrossSeedPageProps {
  activeTab: CrossSeedTab
  onTabChange: (tab: CrossSeedTab) => void
}

export function CrossSeedPage({ activeTab, onTabChange }: CrossSeedPageProps) {
  const { t } = useTranslation("crossseed")

  // Keep the shared SSE stream open so qui activity events drive cache invalidation.
  useActivityStream()

  const { data: settings } = useCrossSeedSettings()
  const { instances } = useActiveInstances()
  const hasEnabledIndexers = useEnabledIndexers().length > 0
  const automationEnabled = settings?.enabled ?? false

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
          {settings && (
            <>
              {activeTab === "rss" && <RssTab settings={settings} />}
              {activeTab === "webhook" && <WebhookTab settings={settings} />}
              {activeTab === "completion" && <CompletionTab settings={settings} />}
              {activeTab === "library" && <LibraryTab settings={settings} />}
              {activeTab === "season-packs" && <SeasonPacksTab settings={settings} />}
              {activeTab === "matching" && <MatchingTab settings={settings} />}
              {activeTab === "categories" && <CategoriesTab settings={settings} />}
              {activeTab === "after-injection" && <AfterInjectionTab settings={settings} />}
            </>
          )}
          {activeTab === "directories" && <DirScanTab instances={instances ?? []} />}
          {activeTab === "blocklist" && <BlocklistTab instances={instances ?? []} />}
        </div>
      </div>
    </div>
  )
}
