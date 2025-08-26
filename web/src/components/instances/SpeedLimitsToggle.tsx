/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useSpeedLimitsStatus, useToggleSpeedLimits } from "@/hooks/useSpeedLimits"
import { Rabbit, Turtle, Loader2 } from "lucide-react"
import { formatSpeed } from "@/lib/utils"

interface SpeedLimitsToggleProps {
  instanceId: number
  connected: boolean
}

export function SpeedLimitsToggle({ instanceId, connected }: SpeedLimitsToggleProps) {
  const { data: speedLimits, isLoading } = useSpeedLimitsStatus(instanceId, {
    enabled: connected,
  })
  const toggleMutation = useToggleSpeedLimits(instanceId)

  const handleToggle = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    
    if (!connected || toggleMutation.isPending) return
    
    toggleMutation.mutate()
  }

  if (!connected || isLoading || !speedLimits) {
    return null
  }

  const isEnabled = speedLimits.alternativeSpeedLimitsEnabled
  const downloadLimit = isEnabled ? speedLimits.altDownloadLimit : speedLimits.downloadLimit
  const uploadLimit = isEnabled ? speedLimits.altUploadLimit : speedLimits.uploadLimit
  
  const tooltipContent = (
    <div className="space-y-1 text-xs">
      <div className="font-medium">
        Speed Limits: {isEnabled ? "Enabled" : "Unlimited"}
      </div>
      {isEnabled && (
        <div className="space-y-0.5 opacity-90">
          <div>↓ {downloadLimit > 0 ? formatSpeed(downloadLimit) : "Unlimited"}</div>
          <div>↑ {uploadLimit > 0 ? formatSpeed(uploadLimit) : "Unlimited"}</div>
        </div>
      )}
      <div className="opacity-90 pt-1 border-t border-white/20">
        Click to {isEnabled ? "disable" : "enable"} alternative speed limits
      </div>
    </div>
  )

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className={`h-6 w-6 p-0 transition-colors ${
              isEnabled? "text-orange-500 hover:text-orange-600 hover:bg-orange-50": "text-green-500 hover:text-green-600 hover:bg-green-50"
            }`}
            onClick={handleToggle}
            disabled={toggleMutation.isPending}
          >
            {toggleMutation.isPending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : isEnabled ? (
              <Turtle className="h-4 w-4" />
            ) : (
              <Rabbit className="h-4 w-4" />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {tooltipContent}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}