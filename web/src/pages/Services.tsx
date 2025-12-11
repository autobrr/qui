/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ReannounceOverview } from "@/components/instances/preferences/ReannounceOverview"
import { TrackerReannounceForm } from "@/components/instances/preferences/TrackerReannounceForm"
import { TrackerRulesPanel } from "@/components/instances/preferences/TrackerRulesPanel"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useInstances } from "@/hooks/useInstances"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { HardDrive } from "lucide-react"
import { useMemo, useState } from "react"

type ServicesSearch = {
  instanceId?: string
}

export function Services() {
  const { instances } = useInstances()
  const navigate = useNavigate()
  const search = useSearch({ from: "/_authenticated/services" }) as ServicesSearch
  const [configureInstanceId, setConfigureInstanceId] = useState<number | null>(null)

  const activeInstances = useMemo(
    () => (instances ?? []).filter((instance) => instance.isActive),
    [instances]
  )

  const selectedInstanceId = useMemo(() => {
    const fromSearch = search.instanceId ? Number(search.instanceId) : undefined
    const allInstances = instances ?? []
    if (fromSearch && allInstances.some((inst) => inst.id === fromSearch)) {
      return fromSearch
    }
    if (allInstances.length > 0) {
      return allInstances[0]?.id
    }
    return undefined
  }, [instances, search.instanceId])

  const handleInstanceChange = (value: string) => {
    navigate({
      to: "/services",
      search: (prev: ServicesSearch) => ({
        ...prev,
        instanceId: value,
      }) satisfies ServicesSearch,
      replace: true,
    })
  }

  const selectedInstance = activeInstances.find((inst) => inst.id === selectedInstanceId)
  const configureInstance = instances?.find((inst) => inst.id === configureInstanceId)

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
      <ReannounceOverview onConfigureInstance={setConfigureInstanceId} />

      {/* Tracker Rules - per-instance selection */}
      {instances && instances.length > 0 && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Tracker Rules</h2>
            <Select
              value={selectedInstanceId ? String(selectedInstanceId) : undefined}
              onValueChange={handleInstanceChange}
            >
              <SelectTrigger className="w-[240px]">
                <div className="flex items-center gap-2 min-w-0 overflow-hidden">
                  <HardDrive className="h-4 w-4 flex-shrink-0" />
                  <span className="truncate">
                    <SelectValue placeholder="Select instance" />
                  </span>
                </div>
              </SelectTrigger>
              <SelectContent>
                {(instances ?? []).map((instance) => (
                  <SelectItem key={instance.id} value={String(instance.id)}>
                    <div className="flex items-center max-w-40 gap-2">
                      <span className="truncate">{instance.name}</span>
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {selectedInstance && (
            <TrackerRulesPanel instanceId={selectedInstance.id} />
          )}

          {!selectedInstance && (
            <p className="text-sm text-muted-foreground">
              Select an active instance to configure tracker rules.
            </p>
          )}
        </div>
      )}

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No instances configured yet. Add one in Settings to use services.
        </p>
      )}

      {/* Reannounce Configuration Sheet */}
      <Sheet open={configureInstanceId !== null} onOpenChange={(open) => !open && setConfigureInstanceId(null)}>
        <SheetContent side="right" className="w-full sm:max-w-2xl">
          <SheetHeader>
            <SheetTitle>Configure Reannounce</SheetTitle>
            <SheetDescription>
              {configureInstance?.name ?? "Instance"}
            </SheetDescription>
          </SheetHeader>
          <ScrollArea className="flex-1">
            {configureInstanceId && (
              <div className="px-6 py-4">
                <TrackerReannounceForm
                  instanceId={configureInstanceId}
                  variant="embedded"
                  onSuccess={() => setConfigureInstanceId(null)}
                />
              </div>
            )}
          </ScrollArea>
        </SheetContent>
      </Sheet>
    </div>
  )
}
