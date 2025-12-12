/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { TrackerReannounceForm } from "@/components/instances/preferences/TrackerReannounceForm"
import { TrackerRulesOverview } from "@/components/instances/preferences/TrackerRulesOverview"
import { TrackerRulesPanel } from "@/components/instances/preferences/TrackerRulesPanel"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { useInstances } from "@/hooks/useInstances"
import { useState } from "react"

type ConfigureType = "reannounce" | "tracker-rules"

const REANNOUNCE_FORM_ID = "reannounce-settings-form"

export function Services() {
  const { instances, isUpdating } = useInstances()
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
        <SheetContent side="right" className="flex h-full max-h-[100dvh] w-full flex-col overflow-hidden p-0 sm:max-w-2xl">
          <SheetHeader className="shrink-0 px-6 pt-6">
            <SheetTitle>
              {configureType === "reannounce" ? "Configure Reannounce" : "Configure Tracker Rules"}
            </SheetTitle>
            <SheetDescription>
              {configureInstance?.name ?? "Instance"}
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 min-h-0 overflow-hidden">
            <ScrollArea className="h-full px-6 py-4">
              {configureType === "reannounce" ? (
                <TrackerReannounceForm
                  instanceId={configureInstanceId!}
                  variant="embedded"
                  formId={REANNOUNCE_FORM_ID}
                  onSuccess={handleCloseSheet}
                />
              ) : (
                <TrackerRulesPanel instanceId={configureInstanceId!} variant="embedded" />
              )}
            </ScrollArea>
          </div>

          {configureType === "reannounce" && (
            <SheetFooter className="shrink-0 border-t bg-muted/30 px-6 py-4">
              <Button type="submit" form={REANNOUNCE_FORM_ID} disabled={isUpdating}>
                {isUpdating ? "Saving..." : "Save Changes"}
              </Button>
            </SheetFooter>
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
