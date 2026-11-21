/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { UnifiedScopeDropdownSection } from "@/components/layout/UnifiedScopeDropdownSection"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Logo } from "@/components/ui/Logo"
import { NapsterLogo } from "@/components/ui/NapsterLogo"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { useTheme } from "@/hooks/useTheme"
import { cn } from "@/lib/utils"
import type { InstanceResponse } from "@/types"
import { Link } from "@tanstack/react-router"
import { ChevronsUpDown } from "lucide-react"
import { useTranslation } from "react-i18next"

interface HeaderInstanceTitleProps {
  className?: string
  instanceName: string | null
  activeInstances: InstanceResponse[]
  selectedInstanceId: number | null
  isAllInstancesRoute: boolean
  effectiveUnifiedInstanceIds: number[]
  onResetUnifiedScope: () => void
  onToggleUnifiedScopeInstance: (instanceId: number) => void
}

export function HeaderInstanceTitle({
  className,
  instanceName,
  activeInstances,
  selectedInstanceId,
  isAllInstancesRoute,
  effectiveUnifiedInstanceIds,
  onResetUnifiedScope,
  onToggleUnifiedScopeInstance,
}: HeaderInstanceTitleProps) {
  const { t } = useTranslation("common")
  const { theme } = useTheme()
  const logo = theme === "swizzin" ? (
    <SwizzinLogo className="h-5 w-5" />
  ) : theme === "napster" ? (
    <NapsterLogo className="h-5 w-5" />
  ) : (
    <Logo className="h-5 w-5" />
  )

  if (!instanceName || activeInstances.length < 2) {
    return (
      <h1 className={cn("flex items-center gap-2 pl-2 sm:pl-0 text-xl font-semibold transition-all duration-300", className)}>
        {logo}
        {instanceName ? (
          <span className="truncate max-w-32">{instanceName}</span>
        ) : "qui"}
      </h1>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className={cn(
            "group flex items-center gap-2 text-xl font-semibold transition-all duration-300 hover:opacity-90 rounded-sm px-1 -mx-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
            className
          )}
          aria-label={t("header.currentInstance", { name: instanceName })}
          aria-haspopup="menu"
        >
          {logo}
          <span className="flex items-center max-w-32">
            <span className="truncate">{instanceName}</span>
            <ChevronsUpDown className="h-3 w-3 text-muted-foreground ml-1 mt-0.5 opacity-60 flex-shrink-0" />
          </span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-64 mt-2" side="bottom" align="start">
        <DropdownMenuLabel className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
          {t("nav.instances")}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <UnifiedScopeDropdownSection
          activeInstances={activeInstances}
          effectiveUnifiedInstanceIds={effectiveUnifiedInstanceIds}
          isAllInstancesRoute={isAllInstancesRoute}
          onResetUnifiedScope={onResetUnifiedScope}
          onToggleUnifiedScopeInstance={onToggleUnifiedScopeInstance}
          scopeKeyPrefix="header-switch-scope"
        />
        <div className="max-h-64 overflow-y-auto space-y-1">
          {activeInstances.map((instance) => (
            <DropdownMenuItem key={instance.id} asChild>
              <Link
                to="/instances/$instanceId"
                params={{ instanceId: instance.id.toString() }}
                className={cn(
                  "flex items-center gap-2 cursor-pointer rounded-sm px-2 py-1.5 text-sm focus-visible:outline-none",
                  instance.id === selectedInstanceId ? "bg-accent text-accent-foreground font-medium" : "hover:bg-accent/80 data-[highlighted]:bg-accent/80 text-foreground"
                )}
              >
                <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} colored={instance.id !== selectedInstanceId} />
                <span className="flex-1 truncate">{instance.name}</span>
                {!instance.connected && <span className="sr-only">{t("header.disconnected")}</span>}
              </Link>
            </DropdownMenuItem>
          ))}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
