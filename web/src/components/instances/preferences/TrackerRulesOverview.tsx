/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { cn, parseTrackerDomains } from "@/lib/utils"
import type { TrackerRule } from "@/types"
import { useQueries } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, Clock, Info, Loader2, Scale, Settings2 } from "lucide-react"
import { useMemo, useState } from "react"

interface TrackerRulesOverviewProps {
  onConfigureInstance?: (instanceId: number) => void
}

export function TrackerRulesOverview({ onConfigureInstance }: TrackerRulesOverviewProps) {
  const { instances } = useInstances()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Fetch rules for all active instances
  const rulesQueries = useQueries({
    queries: activeInstances.map((instance) => ({
      queryKey: ["tracker-rules", instance.id],
      queryFn: () => api.listTrackerRules(instance.id),
      staleTime: 30000,
    })),
  })

  const getRulesSummary = (rules: TrackerRule[] | undefined): string => {
    if (!rules || rules.length === 0) return "No rules configured"
    const enabledCount = rules.filter(r => r.enabled).length
    if (enabledCount === rules.length) {
      return `${rules.length} rule${rules.length === 1 ? "" : "s"}`
    }
    return `${enabledCount}/${rules.length} rules enabled`
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg font-semibold">Tracker Rules</CardTitle>
          <CardDescription>
            No instances configured. Add one in Settings to use this service.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-lg font-semibold">Tracker Rules</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px]">
              <p>
                Apply per-tracker speed limits, ratio limits, and seeding time limits automatically.
                Rules are matched by tracker domain and optionally filtered by category or tag.
              </p>
            </TooltipContent>
          </Tooltip>
        </div>
        <CardDescription>
          Apply speed and ratio caps per tracker domain.
        </CardDescription>
      </CardHeader>

      <CardContent className="p-0">
        <Accordion
          type="multiple"
          value={expandedInstances}
          onValueChange={setExpandedInstances}
          className="border-t"
        >
          {activeInstances.map((instance, index) => {
            const rulesQuery = rulesQueries[index]
            const rules = rulesQuery?.data ?? []
            const sortedRules = [...rules].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
            const enabledRulesCount = rules.filter(r => r.enabled).length

            return (
              <AccordionItem key={instance.id} value={String(instance.id)}>
                <AccordionTrigger className="px-6 py-4 hover:no-underline group">
                  <div className="flex items-center justify-between w-full pr-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <span className="font-medium truncate">{instance.name}</span>
                      {rules.length > 0 && (
                        <Badge variant="outline" className={cn(
                          "text-xs",
                          enabledRulesCount > 0 && "bg-emerald-500/10 text-emerald-500 border-emerald-500/20"
                        )}>
                          {enabledRulesCount}/{rules.length} active
                        </Badge>
                      )}
                    </div>
                  </div>
                </AccordionTrigger>

                <AccordionContent className="px-6 pb-4">
                  <div className="space-y-4">
                    {/* Rules summary */}
                    <div className="flex items-center justify-between p-3 rounded-lg bg-muted/30 border border-border/40">
                      <div className="space-y-0.5">
                        <p className="text-sm text-muted-foreground">
                          {getRulesSummary(rules)}
                        </p>
                      </div>
                      {onConfigureInstance && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => onConfigureInstance(instance.id)}
                          className="h-8"
                        >
                          <Settings2 className="h-4 w-4 mr-2" />
                          Configure
                        </Button>
                      )}
                    </div>

                    {/* Rules list preview */}
                    {rulesQuery?.isError ? (
                      <div className="h-[100px] flex flex-col items-center justify-center border border-destructive/30 rounded-lg bg-destructive/10 text-center p-4">
                        <p className="text-sm text-destructive">Failed to load rules</p>
                        <p className="text-xs text-destructive/70 mt-1">Check connection to the instance.</p>
                      </div>
                    ) : rulesQuery?.isLoading ? (
                      <div className="flex items-center gap-2 text-muted-foreground text-sm py-4">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Loading rules...
                      </div>
                    ) : sortedRules.length === 0 ? (
                      <div className="flex flex-col items-center justify-center py-6 text-center space-y-2 border border-dashed rounded-lg">
                        <p className="text-sm text-muted-foreground">
                          No tracker rules configured yet.
                        </p>
                        {onConfigureInstance && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onConfigureInstance(instance.id)}
                          >
                            Add your first rule
                          </Button>
                        )}
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {sortedRules.slice(0, 5).map((rule) => (
                          <RulePreview key={rule.id} rule={rule} />
                        ))}
                        {sortedRules.length > 5 && (
                          <p className="text-xs text-muted-foreground text-center py-2">
                            +{sortedRules.length - 5} more rule{sortedRules.length - 5 === 1 ? "" : "s"}
                          </p>
                        )}
                      </div>
                    )}
                  </div>
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      </CardContent>
    </Card>
  )
}

function RulePreview({ rule }: { rule: TrackerRule }) {
  const trackers = parseTrackerDomains(rule)

  return (
    <div className={cn(
      "rounded-lg border bg-muted/20 p-3 flex items-center justify-between gap-3",
      !rule.enabled && "opacity-50"
    )}>
      <div className="flex items-center gap-2 min-w-0">
        <span className={cn(
          "text-sm font-medium truncate",
          !rule.enabled && "text-muted-foreground"
        )}>
          {rule.name}
        </span>
        {!rule.enabled && (
          <Badge variant="outline" className="text-[10px] text-muted-foreground shrink-0">
            Off
          </Badge>
        )}
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {trackers.length > 0 && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="outline" className="text-[10px] px-1.5 h-5 cursor-help">
                {trackers.length} tracker{trackers.length === 1 ? "" : "s"}
              </Badge>
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p className="break-all">{trackers.join(", ")}</p>
            </TooltipContent>
          </Tooltip>
        )}
        {rule.uploadLimitKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5">
            <ArrowUp className="h-3 w-3" />
            {rule.uploadLimitKiB}
          </Badge>
        )}
        {rule.downloadLimitKiB !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5">
            <ArrowDown className="h-3 w-3" />
            {rule.downloadLimitKiB}
          </Badge>
        )}
        {rule.ratioLimit !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5">
            <Scale className="h-3 w-3" />
            {rule.ratioLimit}
          </Badge>
        )}
        {rule.seedingTimeLimitMinutes !== undefined && (
          <Badge variant="outline" className="text-[10px] px-1.5 h-5 gap-0.5">
            <Clock className="h-3 w-3" />
            {rule.seedingTimeLimitMinutes}m
          </Badge>
        )}
      </div>
    </div>
  )
}

