/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useState, useCallback } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent
} from "@/components/ui/context-menu"
import {
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent
} from "@/components/ui/dropdown-menu"
import { Share2, Upload, Download, Gauge } from "lucide-react"

interface ShareLimitSubmenuProps {
  type: "context" | "dropdown"
  hashCount: number
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => void
  isPending?: boolean
}

export const ShareLimitSubmenu = memo(function ShareLimitSubmenu({
  type,
  hashCount,
  onConfirm,
  isPending = false,
}: ShareLimitSubmenuProps) {
  const [ratioEnabled, setRatioEnabled] = useState(false)
  const [ratioLimit, setRatioLimit] = useState(1.5)
  const [seedingTimeEnabled, setSeedingTimeEnabled] = useState(false)
  const [seedingTimeLimit, setSeedingTimeLimit] = useState(1440) // 24 hours in minutes
  const [inactiveSeedingTimeEnabled, setInactiveSeedingTimeEnabled] = useState(false)
  const [inactiveSeedingTimeLimit, setInactiveSeedingTimeLimit] = useState(10080) // 7 days in minutes

  const handleConfirm = useCallback((): void => {
    onConfirm(
      ratioEnabled ? ratioLimit : -1,
      seedingTimeEnabled ? seedingTimeLimit : -1,
      inactiveSeedingTimeEnabled ? inactiveSeedingTimeLimit : -1
    )
    // Reset form
    setRatioEnabled(false)
    setRatioLimit(1.5)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(1440)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(10080)
  }, [ratioEnabled, ratioLimit, seedingTimeEnabled, seedingTimeLimit, inactiveSeedingTimeEnabled, inactiveSeedingTimeLimit, onConfirm])

  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <Share2 className="mr-2 h-4 w-4" />
        Set Share Limits
      </SubTrigger>
      <SubContent className="w-72">
        <div className="p-3 space-y-4">
          <div className="text-sm font-medium text-foreground mb-3">
            Share Limits for {hashCount} torrent(s)
          </div>
          
          {/* Ratio Limit */}
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="ratioEnabled"
                checked={ratioEnabled}
                onCheckedChange={setRatioEnabled}
              />
              <Label htmlFor="ratioEnabled" className="text-xs">Set ratio limit</Label>
            </div>
            {ratioEnabled && (
              <div className="ml-6 space-y-1">
                <Input
                  id="ratioLimit"
                  type="number"
                  min="0"
                  step="0.1"
                  value={ratioLimit}
                  onChange={(e) => setRatioLimit(parseFloat(e.target.value) || 0)}
                  placeholder="1.5"
                  className="h-7 text-xs"
                />
                <p className="text-[10px] text-muted-foreground">
                  Stop seeding when ratio reaches this value
                </p>
              </div>
            )}
          </div>

          {/* Seeding Time Limit */}
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="seedingTimeEnabled"
                checked={seedingTimeEnabled}
                onCheckedChange={setSeedingTimeEnabled}
              />
              <Label htmlFor="seedingTimeEnabled" className="text-xs">Set seeding time limit</Label>
            </div>
            {seedingTimeEnabled && (
              <div className="ml-6 space-y-1">
                <Input
                  id="seedingTimeLimit"
                  type="number"
                  min="0"
                  value={seedingTimeLimit}
                  onChange={(e) => setSeedingTimeLimit(parseInt(e.target.value) || 0)}
                  placeholder="1440"
                  className="h-7 text-xs"
                />
                <p className="text-[10px] text-muted-foreground">
                  Minutes (1440 = 24 hours)
                </p>
              </div>
            )}
          </div>

          {/* Inactive Seeding Time Limit */}
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="inactiveSeedingTimeEnabled"
                checked={inactiveSeedingTimeEnabled}
                onCheckedChange={setInactiveSeedingTimeEnabled}
              />
              <Label htmlFor="inactiveSeedingTimeEnabled" className="text-xs">Set inactive seeding limit</Label>
            </div>
            {inactiveSeedingTimeEnabled && (
              <div className="ml-6 space-y-1">
                <Input
                  id="inactiveSeedingTimeLimit"
                  type="number"
                  min="0"
                  value={inactiveSeedingTimeLimit}
                  onChange={(e) => setInactiveSeedingTimeLimit(parseInt(e.target.value) || 0)}
                  placeholder="10080"
                  className="h-7 text-xs"
                />
                <p className="text-[10px] text-muted-foreground">
                  Minutes (10080 = 7 days)
                </p>
              </div>
            )}
          </div>

          <Button 
            onClick={handleConfirm} 
            disabled={isPending}
            size="sm"
            className="w-full h-7"
          >
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </div>
      </SubContent>
    </Sub>
  )
})

interface SpeedLimitsSubmenuProps {
  type: "context" | "dropdown"
  hashCount: number
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending?: boolean
}

export const SpeedLimitsSubmenu = memo(function SpeedLimitsSubmenu({
  type,
  hashCount,
  onConfirm,
  isPending = false,
}: SpeedLimitsSubmenuProps) {
  const [uploadLimit, setUploadLimit] = useState(0)
  const [downloadLimit, setDownloadLimit] = useState(0)

  const handleConfirm = useCallback((): void => {
    onConfirm(uploadLimit, downloadLimit)
    // Reset form
    setUploadLimit(0)
    setDownloadLimit(0)
  }, [uploadLimit, downloadLimit, onConfirm])

  const SubTrigger = type === "context" ? ContextMenuSubTrigger : DropdownMenuSubTrigger
  const Sub = type === "context" ? ContextMenuSub : DropdownMenuSub
  const SubContent = type === "context" ? ContextMenuSubContent : DropdownMenuSubContent

  return (
    <Sub>
      <SubTrigger disabled={isPending}>
        <Gauge className="mr-2 h-4 w-4" />
        Set Speed Limits
      </SubTrigger>
      <SubContent className="w-64">
        <div className="p-3 space-y-4">
          <div className="text-sm font-medium text-foreground mb-3">
            Speed Limits for {hashCount} torrent(s)
          </div>
          
          {/* Download Limit */}
          <div className="space-y-2">
            <Label htmlFor="downloadLimit" className="flex items-center gap-2 text-xs">
              <Download className="h-3 w-3" />
              Download limit (KB/s)
            </Label>
            <Input
              id="downloadLimit"
              type="number"
              min="0"
              value={downloadLimit || ""}
              onChange={(e) => setDownloadLimit(parseInt(e.target.value) || 0)}
              placeholder="0 = unlimited"
              className="h-7 text-xs"
            />
          </div>

          {/* Upload Limit */}
          <div className="space-y-2">
            <Label htmlFor="uploadLimit" className="flex items-center gap-2 text-xs">
              <Upload className="h-3 w-3" />
              Upload limit (KB/s)
            </Label>
            <Input
              id="uploadLimit"
              type="number"
              min="0"
              value={uploadLimit || ""}
              onChange={(e) => setUploadLimit(parseInt(e.target.value) || 0)}
              placeholder="0 = unlimited"
              className="h-7 text-xs"
            />
          </div>

          <Button 
            onClick={handleConfirm} 
            disabled={isPending}
            size="sm"
            className="w-full h-7"
          >
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </div>
      </SubContent>
    </Sub>
  )
})