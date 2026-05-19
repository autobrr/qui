/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { Instance } from "@/types"
import { Cog } from "lucide-react"
import { useState } from "react"
import { InstancePreferencesDialog } from "./preferences/InstancePreferencesDialog"

interface InstanceSettingsButtonProps {
  instanceId: number
  instanceName: string
  instance?: Instance
  onClick?: (e: React.MouseEvent) => void
  showButton?: boolean
  defaultTab?: string
  /** Use a proper Button component instead of a span */
  asButton?: boolean
}

export function InstanceSettingsButton({
  instanceId,
  instanceName,
  instance,
  onClick,
  showButton = true,
  defaultTab,
  asButton = false,
}: InstanceSettingsButtonProps) {
  const [preferencesOpen, setPreferencesOpen] = useState(false)

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    onClick?.(e)
    setPreferencesOpen(true)
  }

  return (
    <>
      {showButton && (
        <Tooltip>
          <TooltipTrigger asChild>
            {asButton ? (
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 p-0"
                onClick={handleClick}
                aria-label="Instance settings"
              >
                <Cog className="h-4 w-4" />
              </Button>
            ) : (
              <span
                aria-label="Instance settings"
                role="button"
                tabIndex={0}
                className="cursor-pointer"
                onClick={handleClick}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault()
                    handleClick(e as unknown as React.MouseEvent)
                  }
                }}
              >
                <Cog className="h-4 w-4" />
              </span>
            )}
          </TooltipTrigger>
          <TooltipContent>
            Instance Settings
          </TooltipContent>
        </Tooltip>
      )}

      <InstancePreferencesDialog
        open={preferencesOpen}
        onOpenChange={setPreferencesOpen}
        instanceId={instanceId}
        instanceName={instanceName}
        instance={instance}
        defaultTab={defaultTab}
      />
    </>
  )
}
