/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Cog } from "lucide-react"
import { useState } from "react"
import { InstancePreferencesDialog } from "./preferences/InstancePreferencesDialog"

interface InstanceSettingsButtonProps {
  instanceId: number
  instanceName: string
  onClick?: (e: React.MouseEvent) => void
  showButton?: boolean
}

export function InstanceSettingsButton({
  instanceId,
  instanceName,
  onClick,
  showButton = true,
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
            <span
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
      />
    </>
  )
}
