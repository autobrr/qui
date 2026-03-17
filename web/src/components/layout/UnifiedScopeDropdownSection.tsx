/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { DropdownMenuCheckboxItem, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import type { InstanceResponse } from "@/types"
import { Link } from "@tanstack/react-router"
import { ChevronRight, HardDrive } from "lucide-react"
import { useState } from "react"

interface UnifiedScopeDropdownSectionProps {
  activeInstances: InstanceResponse[]
  effectiveUnifiedInstanceIds: number[]
  isAllInstancesRoute: boolean
  onResetUnifiedScope: () => void
  onToggleUnifiedScopeInstance: (instanceId: number) => void
  scopeKeyPrefix: string
}

export function UnifiedScopeDropdownSection({
  activeInstances,
  effectiveUnifiedInstanceIds,
  isAllInstancesRoute,
  onResetUnifiedScope,
  onToggleUnifiedScopeInstance,
  scopeKeyPrefix,
}: UnifiedScopeDropdownSectionProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const hasCustomUnifiedScope = effectiveUnifiedInstanceIds.length !== activeInstances.length
  const scopeSummary = hasCustomUnifiedScope ? `${effectiveUnifiedInstanceIds.length}/${activeInstances.length}` : "ALL"

  return (
    <Collapsible open={isExpanded} onOpenChange={setIsExpanded} className="space-y-1">
      <div
        className={cn(
          "flex items-stretch rounded-sm text-sm",
          isAllInstancesRoute ? "bg-accent text-accent-foreground font-medium" : "text-foreground"
        )}
      >
        <Link
          to="/instances"
          className={cn(
            "flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 outline-hidden transition-colors",
            isAllInstancesRoute ? "rounded-l-sm" : "rounded-l-sm hover:bg-accent/80 focus-visible:bg-accent/80"
          )}
        >
          <HardDrive className="h-4 w-4 flex-shrink-0" />
          <span className="truncate">Unified</span>
        </Link>
        <CollapsibleTrigger asChild>
          <button
            type="button"
            className={cn(
              "flex items-center gap-1 rounded-r-sm px-2 outline-hidden transition-colors",
              isAllInstancesRoute ? "text-accent-foreground/70 hover:bg-accent/90 focus-visible:bg-accent/90" : "text-muted-foreground hover:bg-accent/80 hover:text-foreground focus-visible:bg-accent/80 focus-visible:text-foreground"
            )}
            aria-label={isExpanded ? "Collapse unified scope" : "Expand unified scope"}
          >
            <span className="text-[10px] font-semibold uppercase tracking-[0.18em]">
              {scopeSummary}
            </span>
            <ChevronRight
              className={cn(
                "h-4 w-4 transition-transform duration-200",
                isExpanded && "rotate-90"
              )}
            />
          </button>
        </CollapsibleTrigger>
      </div>

      <CollapsibleContent className="overflow-hidden data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down">
        <div className="ml-4 space-y-1 border-l border-border/60 pl-2">
          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              onResetUnifiedScope()
            }}
            className="cursor-pointer text-sm"
          >
            All active ({activeInstances.length})
          </DropdownMenuItem>
          {activeInstances.map((instance) => {
            const checked = effectiveUnifiedInstanceIds.includes(instance.id)

            return (
              <DropdownMenuCheckboxItem
                key={`${scopeKeyPrefix}-${instance.id}`}
                checked={checked}
                onSelect={(event) => {
                  event.preventDefault()
                  onToggleUnifiedScopeInstance(instance.id)
                }}
                className="cursor-pointer"
              >
                <span className="flex w-full items-center justify-between gap-2">
                  <span className="truncate">{instance.name}</span>
                  <span
                    className={cn(
                      "h-2 w-2 rounded-full flex-shrink-0",
                      instance.connected ? "bg-green-500" : "bg-red-500"
                    )}
                    aria-hidden="true"
                  />
                </span>
              </DropdownMenuCheckboxItem>
            )
          })}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
