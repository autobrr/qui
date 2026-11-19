/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Section } from "@/components/kit/Section"
import { DisplayProfilesSettings } from "@/components/display-profiles/DisplayProfilesSettings"
import { IndexersPage } from "@/components/indexers/IndexersPage"
import { ArrInstancesManager } from "@/components/settings/ArrInstancesManager"
import { ApiKeysManager } from "@/components/settings/ApiKeysManager"
import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { ApplicationInfoPanel } from "@/components/settings/ApplicationSettings"
import { GazelleSettingsCard } from "@/components/settings/GazelleSettingsCard"
import { InstancesManager } from "@/components/settings/InstancesSettings"
import { DEFAULT_SETTINGS_TAB, SETTINGS_NAV_GROUPS, type SettingsTab } from "@/components/settings/tabs"
import { SectionedPage, type SectionNavGroup } from "@/components/layout/SectionedPage"
import { ExternalProgramsManager } from "@/components/settings/ExternalProgramsManager"
import { LogSettingsPanel } from "@/components/settings/LogSettingsPanel"
import { NotificationsManager } from "@/components/settings/NotificationsManager"
import { TorznabSearchCachePanel } from "@/components/settings/SearchCacheSettings"
import { SecuritySettings } from "@/components/settings/SecuritySettings"
import { TimeLanguageSettings } from "@/components/settings/TimeLanguageSettings"
import { TrackerCustomizationsManager } from "@/components/settings/TrackerCustomizationsManager"
import { StatisticsSettingsCard } from "@/components/settings/StatisticsSettingsCard"
import { ShareSettingsForm } from "@/components/shares/ShareSettingsForm"
import { LicenseManager } from "@/components/themes/LicenseManager.tsx"
import { ThemeSelector } from "@/components/themes/ThemeSelector"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"

import { withBasePath } from "@/lib/base-url"
import type { SettingsSearch } from "@/routes/_authenticated/settings"
import { useQuery } from "@tanstack/react-query"
import { ExternalLink } from "lucide-react"
import { useTranslation } from "react-i18next"

interface SettingsProps {
  search: SettingsSearch
  /** Replaces the current history entry, unless replace is false. */
  onSearchChange: (search: SettingsSearch, options?: { replace: boolean }) => void
}

export function Settings({ search, onSearchChange }: SettingsProps) {
  const { t } = useTranslation("settings")
  const navGroups: SectionNavGroup<SettingsTab>[] = SETTINGS_NAV_GROUPS.map(group => ({
    label: "labelKey" in group ? t(group.labelKey) : undefined,
    sections: group.tabs.map(entry => ({ value: entry.tab, label: t(entry.labelKey) })),
  }))
  const appInfoQuery = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
  })

  return (
    <SectionedPage
      title={t("title")}
      description={t("description")}
      groups={navGroups}
      section={search.tab}
      defaultSection={DEFAULT_SETTINGS_TAB}
      onSectionChange={(tab, options) => onSearchChange({ tab }, options)}
    >
      {(activeTab) => (
        <>
          {activeTab === "application" && (
            <ApplicationInfoPanel openUpdate={search.modal === "install-update"} onUpdateOpened={() => onSearchChange({ ...search, modal: undefined })} />
          )}

          {activeTab === "instances" && (
            <InstancesManager search={search} onSearchChange={onSearchChange} />
          )}

          {activeTab === "indexers" && (
            <>
              <IndexersPage />
              <GazelleSettingsCard />
            </>
          )}

          {activeTab === "trackers" && (
            <TrackerCustomizationsManager />
          )}

          {activeTab === "search-cache" && (
            <TorznabSearchCachePanel />
          )}

          {activeTab === "integrations" && (
            <ArrInstancesManager />
          )}

          {activeTab === "client-api" && (
            <ClientApiKeysManager />
          )}

          {activeTab === "api" && (
            <Section
              title={t("apiKeys.cardTitle")}
              description={t("apiKeys.cardDescription")}
              actions={
                <Button variant="outline" size="sm" asChild>
                  <a href={withBasePath("api/docs")} target="_blank" rel="noopener noreferrer">
                    {t("apiKeys.apiDocs")}
                    <ExternalLink />
                  </a>
                </Button>
              }
            >
              <ApiKeysManager authMode={appInfoQuery.data?.authMode} authModeLoading={appInfoQuery.isLoading} />
            </Section>
          )}

          {activeTab === "external-programs" && (
            <ExternalProgramsManager />
          )}

          {activeTab === "shares" && (
            <ShareSettingsForm />
          )}

          {activeTab === "notifications" && (
            <NotificationsManager />
          )}

          {activeTab === "datetime" && (
            <TimeLanguageSettings />
          )}

          {activeTab === "display-profiles" && (
            <DisplayProfilesSettings />
          )}

          {activeTab === "statistics" && (
            <StatisticsSettingsCard />
          )}

          {activeTab === "themes" && (
            <>
              <ThemeSelector openMenu={search.modal === "theme-menu"} onMenuOpened={() => onSearchChange({ ...search, modal: undefined })} />
              <div id="license-manager">
                <LicenseManager
                  checkoutStatus={search.checkout}
                  checkoutPaymentStatus={search.status}
                  onCheckoutConsumed={() => onSearchChange({ tab: "themes" })}
                />
              </div>
            </>
          )}

          {activeTab === "security" && (
            <SecuritySettings />
          )}

          {activeTab === "logs" && (
            <LogSettingsPanel />
          )}
        </>
      )}
    </SectionedPage>
  )
}
