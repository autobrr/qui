/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"
import { OrphanScanSettingsDialog } from "@/components/instances/preferences/OrphanScanSettingsDialog"
import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { ReannounceSettingsDialog } from "@/components/instances/preferences/ReannounceSettingsDialog"
import { WorkflowsOverview } from "@/components/instances/preferences/WorkflowsOverview"
import { useInstances } from "@/hooks/useInstances"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function Automations() {
  const { t } = useTranslation("common")
  const { instances } = useInstances()
  const [configureInstanceId, setConfigureInstanceId] = useState<number | null>(null)
  const [configureOrphanScanId, setConfigureOrphanScanId] = useState<number | null>(null)
  const [expandedAccordion, setExpandedAccordion] = useState<string | null>(null)

  // Handler for coordinated accordion state across all cards
  const handleAccordionChange = (cardPrefix: string) => (values: string[]) => {
    if (values.length === 0) {
      setExpandedAccordion(null)
    } else {
      // Take the last item - Radix appends new items to the array
      setExpandedAccordion(`${cardPrefix}:${values[values.length - 1]}`)
    }
  }

  // Extract expanded instances for a specific card
  const getExpandedForCard = (cardPrefix: string): string[] => {
    if (!expandedAccordion) return []
    const [prefix, instanceId] = expandedAccordion.split(':')
    return prefix === cardPrefix ? [instanceId] : []
  }

  const configureReannounceInstance = instances?.find((inst) => inst.id === configureInstanceId)
  const configureOrphanScanInstance = instances?.find((inst) => inst.id === configureOrphanScanId)

  return (
    <div className="container mx-auto px-6 space-y-6 py-6 overflow-x-hidden">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex-1 space-y-2">
          <h1 className="text-2xl font-semibold">{t("automationsPage.title")}</h1>
          <p className="text-sm text-muted-foreground">
            {t("automationsPage.description")}
          </p>
        </div>
      </div>

      {/* Workflows full width, then Reannounce + Orphan Scan side by side */}
      <div className="space-y-6">
        <WorkflowsOverview
          expandedInstances={getExpandedForCard('workflows')}
          onExpandedInstancesChange={handleAccordionChange('workflows')}
        />
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6 items-start">
          <ReannounceOverview
            expandedInstances={getExpandedForCard('reannounce')}
            onExpandedInstancesChange={handleAccordionChange('reannounce')}
            onConfigureInstance={setConfigureInstanceId}
          />
          <OrphanScanOverview
            expandedInstances={getExpandedForCard('orphan')}
            onExpandedInstancesChange={handleAccordionChange('orphan')}
            onConfigureInstance={setConfigureOrphanScanId}
          />
        </div>
      </div>

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {t("automationsPage.noInstances")}
        </p>
      )}

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
      />
    </div>
  )
}
