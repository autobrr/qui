/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AUTOMATIONS_NAV_GROUPS, type AutomationsTab } from "@/components/automations/tabs"
import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"
import { OrphanScanSettingsDialog } from "@/components/instances/preferences/OrphanScanSettingsDialog"
import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { ReannounceSettingsDialog } from "@/components/instances/preferences/ReannounceSettingsDialog"
import { WorkflowsOverview } from "@/components/instances/preferences/WorkflowsOverview"
import { PageHeader } from "@/components/layout/PageHeader"
import { SectionedPage, type SectionNavGroup } from "@/components/layout/SectionedPage"
import { useInstances } from "@/hooks/useInstances"
import { useState } from "react"
import { useTranslation } from "react-i18next"

interface AutomationsProps {
  activeTab: AutomationsTab
  onTabChange: (tab: AutomationsTab) => void
}

export function Automations({ activeTab, onTabChange }: AutomationsProps) {
  const { t } = useTranslation("automations")
  const { instances } = useInstances()
  const [configureInstanceId, setConfigureInstanceId] = useState<number | null>(null)
  const [configureOrphanScanId, setConfigureOrphanScanId] = useState<number | null>(null)
  const navGroups: SectionNavGroup<AutomationsTab>[] = AUTOMATIONS_NAV_GROUPS.map(group => ({
    sections: group.tabs.map(entry => ({ value: entry.tab, label: t(entry.labelKey) })),
  }))

  const configureReannounceInstance = instances?.find((inst) => inst.id === configureInstanceId)
  const configureOrphanScanInstance = instances?.find((inst) => inst.id === configureOrphanScanId)

  return (
    <div className="space-y-6 p-4 lg:p-6 pb-16">
      <PageHeader title={t("page.title")} description={t("page.description")} />

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {t("page.noInstances")}
        </p>
      )}

      <SectionedPage navLabel={t("page.title")} groups={navGroups} activeSection={activeTab} onSectionChange={onTabChange}>
        {activeTab === "workflows" && <WorkflowsOverview />}
        {activeTab === "reannounce" && <ReannounceOverview onConfigureInstance={setConfigureInstanceId} />}
        {activeTab === "orphan-scan" && <OrphanScanOverview onConfigureInstance={setConfigureOrphanScanId} />}
      </SectionedPage>

      <ReannounceSettingsDialog
        open={configureInstanceId !== null}
        onOpenChange={(open) => !open && setConfigureInstanceId(null)}
        instanceId={configureInstanceId!}
        instanceName={configureReannounceInstance?.name}
      />

      <OrphanScanSettingsDialog
        open={configureOrphanScanId !== null}
        onOpenChange={(open) => !open && setConfigureOrphanScanId(null)}
        instanceId={configureOrphanScanId!}
        instanceName={configureOrphanScanInstance?.name}
        isRemote={configureOrphanScanInstance?.filesystemMode === "remote"}
        hasIdentity={configureOrphanScanInstance?.capabilities.identity}
      />
    </div>
  )
}
