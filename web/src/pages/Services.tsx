/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { TrackerReannounceForm } from "@/components/instances/preferences/TrackerReannounceForm"
import { OrphanScanOverview } from "@/components/instances/preferences/OrphanScanOverview"
import { AutomationsActivityOverview } from "@/components/instances/preferences/AutomationActivityOverview"
import { AutomationsOverview } from "@/components/instances/preferences/AutomationsOverview"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { useInstances } from "@/hooks/useInstances"
import { useState } from "react"

const REANNOUNCE_FORM_ID = "reannounce-settings-form"

export function Services() {
  const { instances, isUpdating } = useInstances()
  const [configureInstanceId, setConfigureInstanceId] = useState<number | null>(null)

  const configureInstance = instances?.find((inst) => inst.id === configureInstanceId)

  const handleConfigureReannounce = (instanceId: number) => {
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

      {/* Orphan File Scanner - shows all instances with local access */}
      <OrphanScanOverview />

      {/* Automations: 2-col grid with independent heights */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
        <AutomationsOverview />
        <AutomationsActivityOverview />
      </div>

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No instances configured yet. Add one in Settings to use services.
        </p>
      )}

      {/* Reannounce Configuration Sheet */}
      <Sheet open={configureInstanceId !== null} onOpenChange={(open) => !open && handleCloseSheet()}>
        <SheetContent side="right" className="flex h-full max-h-[100dvh] w-full flex-col overflow-hidden p-0 sm:max-w-2xl">
          <SheetHeader className="shrink-0 px-6 pt-6">
            <SheetTitle>Configure Reannounce</SheetTitle>
            <SheetDescription>
              {configureInstance?.name ?? "Instance"}
            </SheetDescription>
          </SheetHeader>

          <div className="flex-1 min-h-0 overflow-hidden">
            <ScrollArea className="h-full px-6 py-4">
              <TrackerReannounceForm
                instanceId={configureInstanceId!}
                variant="embedded"
                formId={REANNOUNCE_FORM_ID}
                onSuccess={handleCloseSheet}
              />
            </ScrollArea>
          </div>

          <SheetFooter className="shrink-0 border-t bg-muted/30 px-6 py-4">
            <Button type="submit" form={REANNOUNCE_FORM_ID} disabled={isUpdating}>
              {isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </div>
  )
}
