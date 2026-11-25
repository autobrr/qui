/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { DropdownMenuCheckboxItem, DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { cn } from "@/lib/utils"
import type { InstanceResponse } from "@/types"
import { Link } from "@tanstack/react-router"
import { ChevronRight } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

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
  const { t } = useTranslation("common")
  const [isExpanded, setIsExpanded] = useState(false)
  const hasCustomUnifiedScope = effectiveUnifiedInstanceIds.length !== activeInstances.length
  const scopeSummary = hasCustomUnifiedScope ? `${effectiveUnifiedInstanceIds.length}/${activeInstances.length}` : t("header.allScope")
  const isSidebar = variant === "sidebar"

  const rowContainerClassName = isSidebar ? cn(
    "flex items-stretch rounded-md text-sm short:text-13/5",
    isAllInstancesRoute ? "bg-sidebar-primary text-sidebar-primary-foreground font-medium" : "text-sidebar-foreground"
  ) : cn(
    "flex items-stretch rounded-sm text-sm",
    isAllInstancesRoute ? "bg-accent text-accent-foreground font-medium" : "text-foreground"
  )

  const rowLinkClassName = isSidebar ? cn(
    "flex min-w-0 flex-1 items-center gap-3 px-3 py-1.5 outline-hidden short:py-1",
    isAllInstancesRoute ? "rounded-l-md" : "rounded-l-md hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground hover:transition-colors hover:duration-200 hover:ease-out"
  ) : cn(
    "flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 outline-hidden transition-colors",
    isAllInstancesRoute ? "rounded-l-sm" : "rounded-l-sm hover:bg-accent/80 focus-visible:bg-accent/80"
  )

  const triggerClassName = isSidebar ? cn(
    "flex items-center gap-1 rounded-r-md px-2.5 outline-hidden",
    isAllInstancesRoute ? "text-sidebar-primary-foreground/75 hover:bg-sidebar-primary/90 focus-visible:bg-sidebar-primary/90" : "text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:bg-sidebar-accent focus-visible:text-sidebar-accent-foreground hover:transition-colors hover:duration-200 hover:ease-out"
  ) : cn(
    "flex items-center gap-1 rounded-r-sm px-2 outline-hidden transition-colors",
    isAllInstancesRoute ? "text-accent-foreground/70 hover:bg-accent/90 focus-visible:bg-accent/90" : "text-muted-foreground hover:bg-accent/80 hover:text-foreground focus-visible:bg-accent/80 focus-visible:text-foreground"
  )

  const dropdownHeaderItemClassName = "cursor-pointer p-0"
  const sidebarTextEnterClassName = "animate-in fade-in-0 duration-200 motion-reduce:animate-none"

  const contentClassName = cn(
    "overflow-hidden data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down",
    !isSidebar && "max-h-[min(18rem,calc(100vh-14rem))]"
  )

  const contentInnerClassName = isSidebar ? "ml-6 space-y-0.5 border-l border-sidebar-border/70 pl-3 short:space-y-px" : "ml-4 max-h-[min(18rem,calc(100vh-14rem))] space-y-1 overflow-y-auto overscroll-contain border-l border-border/60 pl-2 pr-1"

  const sidebarScopeRowClassName = "flex w-full items-center justify-between gap-2 rounded-md px-3 py-1.5 text-sm font-medium transition-all duration-200 ease-out outline-hidden short:py-1 short:text-13/5"

  return (
    <Collapsible open={isExpanded} onOpenChange={setIsExpanded} className="space-y-1">
      <div className={rowContainerClassName}>
        {isSidebar ? (
          <>
            <Link
              to="/instances"
              className={rowLinkClassName}
            >
              <InstanceMarkIcon instanceId={ALL_INSTANCES_ID} colored={!isAllInstancesRoute} />
              <span className={cn("truncate", sidebarTextEnterClassName)}>{t("unifiedScope.unified")}</span>
            </Link>
            <CollapsibleTrigger asChild>
              <button
                type="button"
                className={cn(triggerClassName, sidebarTextEnterClassName)}
                aria-label={isExpanded ? t("unifiedScope.collapseScope") : t("unifiedScope.expandScope")}
              >
                <span className="text-10 font-semibold uppercase tracking-[0.18em]">
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
                <InstanceMarkIcon instanceId={ALL_INSTANCES_ID} colored={!isAllInstancesRoute} />
                <span className="truncate">{t("unifiedScope.unified")}</span>
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
                  aria-label={isExpanded ? t("unifiedScope.collapseScope") : t("unifiedScope.expandScope")}
                >
                  <span className="text-10 font-semibold uppercase tracking-[0.18em]">
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
                <span className="truncate">{t("unifiedScope.allActive", { count: activeInstances.length })}</span>
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
                    <span className="flex min-w-0 items-center gap-2">
                      <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} />
                      <span className="truncate">{instance.name}</span>
                    </span>
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
                {t("unifiedScope.allActive", { count: activeInstances.length })}
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
                    <span className="flex min-w-0 items-center gap-2">
                      <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} />
                      <span className="truncate">{instance.name}</span>
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
