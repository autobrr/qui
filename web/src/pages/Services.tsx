/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { TrackerReannounceForm } from "@/components/instances/preferences/TrackerReannounceForm"
import { TrackerRulesOverview } from "@/components/instances/preferences/TrackerRulesOverview"
import { TrackerRulesPanel } from "@/components/instances/preferences/TrackerRulesPanel"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useInstances } from "@/hooks/useInstances"
import { useState } from "react"

type ConfigureType = "reannounce" | "tracker-rules"

export function Services() {
  const { instances } = useInstances()
  const [configureInstanceId, setConfigureInstanceId] = useState<number | null>(null)
  const [configureType, setConfigureType] = useState<ConfigureType>("reannounce")

  const configureInstance = instances?.find((inst) => inst.id === configureInstanceId)

  const handleConfigureReannounce = (instanceId: number) => {
    setConfigureType("reannounce")
    setConfigureInstanceId(instanceId)
  }

  const handleConfigureTrackerRules = (instanceId: number) => {
    setConfigureType("tracker-rules")
    setConfigureInstanceId(instanceId)
  }

  const handleCloseSheet = () => {
    setConfigureInstanceId(null)
  }

  return (
    <div className="container mx-auto px-6 space-y-6 py-6">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex-1 space-y-2">
          <h1 className="text-2xl font-semibold">Services</h1>
          <p className="text-sm text-muted-foreground">
            Instance-level automation and helper services managed by qui.
          </p>
        </div>
      </div>

      {/* Reannounce Overview - shows all instances in accordion */}
      <ReannounceOverview onConfigureInstance={handleConfigureReannounce} />

      {/* Tracker Rules Overview - shows all instances in accordion */}
      <TrackerRulesOverview onConfigureInstance={handleConfigureTrackerRules} />

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No instances configured yet. Add one in Settings to use services.
        </p>
      )}

      {/* Configuration Sheet */}
      <Sheet open={configureInstanceId !== null} onOpenChange={(open) => !open && handleCloseSheet()}>
        <SheetContent side="right" className="w-full sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>
              {configureType === "reannounce" ? "Configure Reannounce" : "Configure Tracker Rules"}
            </SheetTitle>
            <SheetDescription>
              {configureInstance?.name ?? "Instance"}
            </SheetDescription>
          </SheetHeader>
          <ScrollArea className="flex-1">
            {configureInstanceId && configureType === "reannounce" && (
              <div className="px-6 py-4">
                <TrackerReannounceForm
                  instanceId={configureInstanceId}
                  variant="embedded"
                  onSuccess={handleCloseSheet}
                />
              </div>
            )}
            {configureInstanceId && configureType === "tracker-rules" && (
              <div className="px-6 py-4">
                <TrackerRulesPanel instanceId={configureInstanceId} variant="embedded" />
              </div>
            )}
          </ScrollArea>
        </SheetContent>
      </Sheet>
    </div>
  )
}
