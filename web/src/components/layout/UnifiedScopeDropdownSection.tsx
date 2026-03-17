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
  variant?: "dropdown" | "sidebar"
}

export function UnifiedScopeDropdownSection({
  activeInstances,
  effectiveUnifiedInstanceIds,
  isAllInstancesRoute,
  onResetUnifiedScope,
  onToggleUnifiedScopeInstance,
  scopeKeyPrefix,
  variant = "dropdown",
}: UnifiedScopeDropdownSectionProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  const hasCustomUnifiedScope = effectiveUnifiedInstanceIds.length !== activeInstances.length
  const scopeSummary = hasCustomUnifiedScope ? `${effectiveUnifiedInstanceIds.length}/${activeInstances.length}` : "ALL"
  const isSidebar = variant === "sidebar"

  const rowContainerClassName = isSidebar ? cn(
    "flex items-stretch rounded-md text-sm transition-all duration-200 ease-out",
    isAllInstancesRoute ? "bg-sidebar-primary text-sidebar-primary-foreground font-medium" : "text-sidebar-foreground"
  ) : cn(
    "flex items-stretch rounded-sm text-sm",
    isAllInstancesRoute ? "bg-accent text-accent-foreground font-medium" : "text-foreground"
  )

  const rowLinkClassName = isSidebar ? cn(
    "flex min-w-0 flex-1 items-center gap-3 px-3 py-2 outline-hidden transition-all duration-200 ease-out",
    isAllInstancesRoute ? "rounded-l-md" : "rounded-l-md hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground"
  ) : cn(
    "flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 outline-hidden transition-colors",
    isAllInstancesRoute ? "rounded-l-sm" : "rounded-l-sm hover:bg-accent/80 focus-visible:bg-accent/80"
  )

  const triggerClassName = isSidebar ? cn(
    "flex items-center gap-1 rounded-r-md px-2.5 outline-hidden transition-all duration-200 ease-out",
    isAllInstancesRoute ? "text-sidebar-primary-foreground/75 hover:bg-sidebar-primary/90 focus-visible:bg-sidebar-primary/90" : "text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground"
  ) : cn(
    "flex items-center gap-1 rounded-r-sm px-2 outline-hidden transition-colors",
    isAllInstancesRoute ? "text-accent-foreground/70 hover:bg-accent/90 focus-visible:bg-accent/90" : "text-muted-foreground hover:bg-accent/80 hover:text-foreground focus-visible:bg-accent/80 focus-visible:text-foreground"
  )

  const dropdownHeaderItemClassName = "cursor-pointer p-0"

  const contentClassName = cn(
    "overflow-hidden data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down",
    !isSidebar && "max-h-[min(18rem,calc(100vh-14rem))]"
  )

  const contentInnerClassName = isSidebar ? "ml-6 space-y-1 border-l border-sidebar-border/70 pl-3" : "ml-4 max-h-[min(18rem,calc(100vh-14rem))] space-y-1 overflow-y-auto overscroll-contain border-l border-border/60 pl-2 pr-1"

  const sidebarScopeRowClassName = "flex w-full items-center justify-between gap-2 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out outline-hidden"

  return (
    <Collapsible open={isExpanded} onOpenChange={setIsExpanded} className="space-y-1">
      <div className={rowContainerClassName}>
        {isSidebar ? (
          <>
            <Link
              to="/instances"
              className={rowLinkClassName}
            >
              <HardDrive className="h-4 w-4 flex-shrink-0" />
              <span className="truncate">Unified</span>
            </Link>
            <CollapsibleTrigger asChild>
              <button
                type="button"
                className={triggerClassName}
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
          </>
        ) : (
          <>
            <DropdownMenuItem asChild className={dropdownHeaderItemClassName}>
              <Link
                to="/instances"
                className={rowLinkClassName}
              >
                <HardDrive className="h-4 w-4 flex-shrink-0" />
                <span className="truncate">Unified</span>
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem
              asChild
              className={dropdownHeaderItemClassName}
              onSelect={(event) => {
                event.preventDefault()
              }}
            >
              <CollapsibleTrigger asChild>
                <button
                  type="button"
                  className={triggerClassName}
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
            </DropdownMenuItem>
          </>
        )}
      </div>

      <CollapsibleContent className={contentClassName}>
        <div className={contentInnerClassName}>
          {isSidebar ? (
            <>
              <button
                type="button"
                onClick={onResetUnifiedScope}
                className={cn(
                  sidebarScopeRowClassName,
                  "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground"
                )}
              >
                <span className="truncate">All active ({activeInstances.length})</span>
              </button>
              {activeInstances.map((instance) => {
                const checked = effectiveUnifiedInstanceIds.includes(instance.id)

                return (
                  <button
                    key={`${scopeKeyPrefix}-${instance.id}`}
                    type="button"
                    onClick={() => onToggleUnifiedScopeInstance(instance.id)}
                    className={cn(
                      sidebarScopeRowClassName,
                      checked ? "bg-sidebar-accent text-sidebar-accent-foreground" : "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground"
                    )}
                  >
                    <span className="truncate">{instance.name}</span>
                    <span
                      className={cn(
                        "h-2 w-2 rounded-full flex-shrink-0",
                        instance.connected ? "bg-green-500" : "bg-red-500"
                      )}
                      aria-hidden="true"
                    />
                  </button>
                )
              })}
            </>
          ) : (
            <>
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
            </>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
