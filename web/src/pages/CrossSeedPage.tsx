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
import { CROSS_SEED_NAV_GROUPS, DEFAULT_CROSS_SEED_TAB, type CrossSeedTab } from "@/components/cross-seed/tabs"
import { WebhookTab } from "@/components/cross-seed/WebhookTab"
import { SectionedPage, type SectionNavGroup } from "@/components/layout/SectionedPage"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { useActivityStream } from "@/contexts/SyncStreamContext"
import { useActiveInstances, useCrossSeedSettings, useEnabledIndexers } from "@/hooks/useCrossSeedSettings"
import { Link } from "@tanstack/react-router"
import { AlertTriangle } from "lucide-react"
import { useTranslation } from "react-i18next"

interface CrossSeedPageProps {
  activeTab: CrossSeedTab | undefined
  onTabChange: (tab: CrossSeedTab | undefined, options: { replace: boolean }) => void
}

export function CrossSeedPage({ activeTab, onTabChange }: CrossSeedPageProps) {
  const { t } = useTranslation("crossseed")

  useActivityStream()

  const { data: settings } = useCrossSeedSettings()
  const { instances } = useActiveInstances()
  const hasEnabledIndexers = useEnabledIndexers().length > 0
  const automationEnabled = settings?.enabled ?? false
  const navGroups: SectionNavGroup<CrossSeedTab>[] = CROSS_SEED_NAV_GROUPS.map(group => ({
    label: "labelKey" in group ? t(group.labelKey) : undefined,
    sections: group.tabs.map(tab => ({ value: tab, label: t(`tabs.${tab}`) })),
  }))

  return (
    <SectionedPage
      title={t("pageTitle")}
      description={t("pageDescription")}
      badge={(
        <Badge variant={automationEnabled ? "default" : "secondary"}>
          {t(automationEnabled ? "automationOnBadge" : "automationOffBadge")}
        </Badge>
      )}
      notice={!hasEnabledIndexers && (
        <Alert className="border-border rounded-xl bg-card [&>svg]:text-warning">
          <AlertTriangle className="h-4 w-4" />
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
      groups={navGroups}
      section={activeTab}
      defaultSection={DEFAULT_CROSS_SEED_TAB}
      onSectionChange={onTabChange}
    >
      {(tab) => (
        <>
          {settings && (
            <>
              {tab === "rss" && <RssTab settings={settings} />}
              {tab === "webhook" && <WebhookTab settings={settings} />}
              {tab === "completion" && <CompletionTab settings={settings} />}
              {tab === "library" && <LibraryTab settings={settings} />}
              {tab === "season-packs" && <SeasonPacksTab settings={settings} />}
              {tab === "matching" && <MatchingTab settings={settings} />}
              {tab === "categories" && <CategoriesTab settings={settings} />}
              {tab === "after-injection" && <AfterInjectionTab settings={settings} />}
            </>
          )}
          {tab === "directories" && <DirScanTab instances={instances ?? []} />}
          {tab === "blocklist" && <BlocklistTab instances={instances ?? []} />}
        </>
      )}
    </SectionedPage>
  )
}
