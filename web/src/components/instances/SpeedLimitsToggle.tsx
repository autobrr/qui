/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger, ContextMenuLabel } from "@/components/ui/context-menu"
import { useSpeedLimitsStatus, useToggleSpeedLimits } from "@/hooks/useSpeedLimits"
import { Rabbit, Turtle, Loader2, Settings } from "lucide-react"
import { formatSpeed } from "@/lib/utils"
import { SpeedLimitsDialog } from "./SpeedLimitsDialog"

interface SpeedLimitsToggleProps {
  instanceId: number
  instanceName: string
  connected: boolean
}

export function SpeedLimitsToggle({ instanceId, instanceName, connected }: SpeedLimitsToggleProps) {
  const [dialogOpen, setDialogOpen] = useState(false)
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

  const handleOpenDialog = (e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    
    if (!connected) return
    
    setDialogOpen(true)
  }

  if (!connected || isLoading || !speedLimits) {
    return null
  }

  const isEnabled = speedLimits.alternativeSpeedLimitsEnabled
  const downloadLimit = isEnabled ? speedLimits.altDownloadLimit : speedLimits.downloadLimit
  const uploadLimit = isEnabled ? speedLimits.altUploadLimit : speedLimits.uploadLimit
  
  const toggleTooltipContent = (
    <div className="space-y-1 text-xs">
      <div className="font-medium">
        Speed Limits: {isEnabled ? "Alternative" : "Global"}
      </div>
      {isEnabled && (
        <div className="space-y-0.5 opacity-90">
          <div>↓ {downloadLimit > 0 ? formatSpeed(downloadLimit) : "Unlimited"}</div>
          <div>↑ {uploadLimit > 0 ? formatSpeed(uploadLimit) : "Unlimited"}</div>
        </div>
      )}
      <div className="opacity-90 pt-1 border-t border-white/20 space-y-0.5">
        <div>Left-click: {isEnabled ? "disable" : "enable"} alternative speed limits</div>
        <div>Right-click: configure speed limits</div>
      </div>
    </div>
  )


  return (
    <>
      <TooltipProvider>
        <ContextMenu>
          <Tooltip>
            <ContextMenuTrigger asChild>
              <TooltipTrigger asChild onFocus={(event) => event.preventDefault()}>
                <Button
                  variant="ghost"
                  size="icon"
                  className={`h-6 w-6 p-0 transition-colors ${
                    isEnabled ? "text-orange-500 hover:text-orange-600 hover:bg-orange-50" : "text-green-500 hover:text-green-600 hover:bg-green-50"
                  }`}
                  onClick={handleToggle}
                  disabled={toggleMutation.isPending}
                >
                  {toggleMutation.isPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : isEnabled ? (
                    <Turtle className="h-4 w-4" />
                  ) : (
                    <Rabbit className="h-4 w-4" />
                  )}
                </Button>
              </TooltipTrigger>
            </ContextMenuTrigger>
            <TooltipContent side="bottom">
              {toggleTooltipContent}
            </TooltipContent>
          </Tooltip>
          <ContextMenuContent className="w-56">
            <ContextMenuLabel>Speed Limits</ContextMenuLabel>
            <ContextMenuSeparator />
          
            <ContextMenuItem
              onClick={handleOpenDialog}
              className="flex items-center gap-2"
            >
              <Settings className="h-4 w-4" />
              Configure Speed Limits...
            </ContextMenuItem>
          
            <ContextMenuSeparator />
          
            <div className="px-2 py-1">
              <div className="text-xs text-muted-foreground mb-1">
                Current Status: {isEnabled ? "Alternative" : "Global"} Mode
              </div>
              {isEnabled && (
                <div className="text-xs text-muted-foreground space-y-0.5">
                  <div>↓ {downloadLimit > 0 ? formatSpeed(downloadLimit) : "Unlimited"}</div>
                  <div>↑ {uploadLimit > 0 ? formatSpeed(uploadLimit) : "Unlimited"}</div>
                </div>
              )}
            </div>
          </ContextMenuContent>
        </ContextMenu>
      </TooltipProvider>
      
      <SpeedLimitsDialog
        instanceId={instanceId}
        instanceName={instanceName}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
      />
    </>
  )
}