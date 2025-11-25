/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TrackerReannounceForm } from "@/components/instances/preferences/TrackerReannounceForm"
import { TrackerRulesPanel } from "@/components/instances/preferences/TrackerRulesPanel"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useInstances } from "@/hooks/useInstances"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { HardDrive } from "lucide-react"
import { useMemo } from "react"

type ServicesSearch = {
  instanceId?: string
}

export function Services() {
  const { instances } = useInstances()
  const navigate = useNavigate()
  const search = useSearch({ from: "/_authenticated/services" }) as ServicesSearch

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

  return (
    <div className="container mx-auto px-6 space-y-6 py-6">
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex-1 space-y-2">
          <h1 className="text-2xl font-semibold">Services</h1>
          <p className="text-sm text-muted-foreground">
            Instance-level automation and helper services managed by qui.
          </p>
        </div>

        <div className="flex items-center gap-2">
          {instances && instances.length > 0 && (
            <Select
              value={selectedInstanceId ? String(selectedInstanceId) : undefined}
              onValueChange={handleInstanceChange}
            >
              <SelectTrigger className="!w-[240px] !max-w-[240px]">
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
          )}
        </div>
      </div>

      {instances && instances.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No instances configured yet. Add one in Settings to use services.
        </p>
      )}

      {!selectedInstance && instances && instances.length > 0 && (
        <p className="text-sm text-muted-foreground">
          Select an active instance to configure services.
        </p>
      )}

      {selectedInstance && (
        <div key={selectedInstance.id} className="space-y-6">
          <TrackerRulesPanel instanceId={selectedInstance.id} />

          <TrackerReannounceForm instanceId={selectedInstance.id} />
        </div>
      )}
    </div>
  )
}
