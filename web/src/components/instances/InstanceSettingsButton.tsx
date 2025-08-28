/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { InstancePreferencesDialog } from "./preferences/InstancePreferencesDialog"
import { Cog } from "lucide-react"

interface InstanceSettingsButtonProps {
  instanceId: number
  instanceName: string
  onClick?: (e: React.MouseEvent) => void
}

export function InstanceSettingsButton({
  instanceId,
  instanceName,
  onClick,
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
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 p-0"
            onClick={handleClick}
          >
            <Cog className="h-4 w-4" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          Instance Settings
        </TooltipContent>
      </Tooltip>

      <InstancePreferencesDialog
        open={preferencesOpen}
        onOpenChange={setPreferencesOpen}
        instanceId={instanceId}
        instanceName={instanceName}
      />
    </>
  )
}