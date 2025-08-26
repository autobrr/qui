/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import { useSpeedLimitsStatus, useSetSpeedLimits } from "@/hooks/useSpeedLimits"
import { formatSpeed } from "@/lib/utils"
import { Loader2, Rabbit, Turtle } from "lucide-react"
import type { SetSpeedLimitsRequest } from "@/types"

interface SpeedLimitsDialogProps {
  instanceId: number
  instanceName: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Convert bytes/s to MB/s for display
const bytesToMBps = (bytes: number): number => {
  return bytes === 0 ? 0 : Math.round(bytes / (1024 * 1024) * 100) / 100
}

// Convert MB/s to bytes/s for API
const mbpsToBytes = (mbps: number): number => {
  return mbps === 0 ? 0 : Math.round(mbps * 1024 * 1024)
}

export function SpeedLimitsDialog({ instanceId, instanceName, open, onOpenChange }: SpeedLimitsDialogProps) {
  const { data: speedLimits, isLoading } = useSpeedLimitsStatus(instanceId, {
    enabled: open,
  })
  const setSpeedLimitsMutation = useSetSpeedLimits(instanceId)

  // Form state
  const [globalDownload, setGlobalDownload] = useState("")
  const [globalUpload, setGlobalUpload] = useState("")
  const [altDownload, setAltDownload] = useState("")
  const [altUpload, setAltUpload] = useState("")
  const [useAltLimits, setUseAltLimits] = useState(false)

  // Initialize form with current values
  useEffect(() => {
    if (speedLimits) {
      setGlobalDownload(bytesToMBps(speedLimits.downloadLimit).toString())
      setGlobalUpload(bytesToMBps(speedLimits.uploadLimit).toString())
      setAltDownload(bytesToMBps(speedLimits.altDownloadLimit).toString())
      setAltUpload(bytesToMBps(speedLimits.altUploadLimit).toString())
      setUseAltLimits(speedLimits.alternativeSpeedLimitsEnabled)
    }
  }, [speedLimits])

  const handleSave = async () => {
    const request: SetSpeedLimitsRequest = {}

    // Parse and convert values
    const globalDlValue = parseFloat(globalDownload)
    const globalUlValue = parseFloat(globalUpload)
    const altDlValue = parseFloat(altDownload)
    const altUlValue = parseFloat(altUpload)

    // Set global limits if changed
    if (!isNaN(globalDlValue) && globalDlValue !== bytesToMBps(speedLimits?.downloadLimit || 0)) {
      request.downloadLimit = mbpsToBytes(globalDlValue)
    }
    if (!isNaN(globalUlValue) && globalUlValue !== bytesToMBps(speedLimits?.uploadLimit || 0)) {
      request.uploadLimit = mbpsToBytes(globalUlValue)
    }

    // Set alternative limits if changed
    if (!isNaN(altDlValue) && altDlValue !== bytesToMBps(speedLimits?.altDownloadLimit || 0)) {
      request.altDownloadLimit = mbpsToBytes(altDlValue)
    }
    if (!isNaN(altUlValue) && altUlValue !== bytesToMBps(speedLimits?.altUploadLimit || 0)) {
      request.altUploadLimit = mbpsToBytes(altUlValue)
    }

    // Set alternative mode if changed
    if (useAltLimits !== speedLimits?.alternativeSpeedLimitsEnabled) {
      request.alternativeSpeedLimitsEnabled = useAltLimits
    }

    try {
      await setSpeedLimitsMutation.mutateAsync(request)
      onOpenChange(false)
    } catch (error) {
      console.error("Failed to set speed limits:", error)
    }
  }

  const presetSpeeds = [
    { label: "1 MB/s", value: 1 },
    { label: "5 MB/s", value: 5 },
    { label: "10 MB/s", value: 10 },
    { label: "25 MB/s", value: 25 },
    { label: "50 MB/s", value: 50 },
    { label: "Unlimited", value: 0 },
  ]

  const applyPreset = (field: "globalDownload" | "globalUpload" | "altDownload" | "altUpload", value: number) => {
    const stringValue = value.toString()
    switch (field) {
      case "globalDownload":
        setGlobalDownload(stringValue)
        break
      case "globalUpload":
        setGlobalUpload(stringValue)
        break
      case "altDownload":
        setAltDownload(stringValue)
        break
      case "altUpload":
        setAltUpload(stringValue)
        break
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            Speed Limits - {instanceName}
          </DialogTitle>
          <DialogDescription>
            Configure download and upload speed limits for this qBittorrent instance.
            Values are in MB/s (0 = unlimited).
          </DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-6 w-6 animate-spin" />
            <span className="ml-2">Loading current settings...</span>
          </div>
        ) : (
          <div className="space-y-6">
            {/* Alternative Speed Limits Toggle */}
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {useAltLimits ? (
                  <Turtle className="h-4 w-4 text-orange-500" />
                ) : (
                  <Rabbit className="h-4 w-4 text-green-500" />
                )}
                <Label htmlFor="alt-mode" className="text-sm font-medium">
                  Use Alternative Speed Limits
                </Label>
              </div>
              <Switch
                id="alt-mode"
                checked={useAltLimits}
                onCheckedChange={setUseAltLimits}
              />
            </div>

            <Separator />

            {/* Global Speed Limits */}
            <div className="space-y-4">
              <div className="flex items-center gap-2">
                <Rabbit className="h-4 w-4 text-green-500" />
                <h3 className="text-sm font-semibold">Global Speed Limits</h3>
                {!useAltLimits && <span className="text-xs text-muted-foreground">(Currently Active)</span>}
              </div>
              
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="global-download">Download (MB/s)</Label>
                  <Input
                    id="global-download"
                    type="number"
                    min="0"
                    step="0.1"
                    placeholder="0 = unlimited"
                    value={globalDownload}
                    onChange={(e) => setGlobalDownload(e.target.value)}
                  />
                  <div className="flex flex-wrap gap-1">
                    {presetSpeeds.map((preset) => (
                      <Button
                        key={`global-dl-${preset.value}`}
                        variant="outline"
                        size="sm"
                        className="text-xs h-6 px-2"
                        onClick={() => applyPreset("globalDownload", preset.value)}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="global-upload">Upload (MB/s)</Label>
                  <Input
                    id="global-upload"
                    type="number"
                    min="0"
                    step="0.1"
                    placeholder="0 = unlimited"
                    value={globalUpload}
                    onChange={(e) => setGlobalUpload(e.target.value)}
                  />
                  <div className="flex flex-wrap gap-1">
                    {presetSpeeds.map((preset) => (
                      <Button
                        key={`global-ul-${preset.value}`}
                        variant="outline"
                        size="sm"
                        className="text-xs h-6 px-2"
                        onClick={() => applyPreset("globalUpload", preset.value)}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <Separator />

            {/* Alternative Speed Limits */}
            <div className="space-y-4">
              <div className="flex items-center gap-2">
                <Turtle className="h-4 w-4 text-orange-500" />
                <h3 className="text-sm font-semibold">Alternative Speed Limits</h3>
                {useAltLimits && <span className="text-xs text-muted-foreground">(Currently Active)</span>}
              </div>
              
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="alt-download">Download (MB/s)</Label>
                  <Input
                    id="alt-download"
                    type="number"
                    min="0"
                    step="0.1"
                    placeholder="0 = unlimited"
                    value={altDownload}
                    onChange={(e) => setAltDownload(e.target.value)}
                  />
                  <div className="flex flex-wrap gap-1">
                    {presetSpeeds.map((preset) => (
                      <Button
                        key={`alt-dl-${preset.value}`}
                        variant="outline"
                        size="sm"
                        className="text-xs h-6 px-2"
                        onClick={() => applyPreset("altDownload", preset.value)}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="alt-upload">Upload (MB/s)</Label>
                  <Input
                    id="alt-upload"
                    type="number"
                    min="0"
                    step="0.1"
                    placeholder="0 = unlimited"
                    value={altUpload}
                    onChange={(e) => setAltUpload(e.target.value)}
                  />
                  <div className="flex flex-wrap gap-1">
                    {presetSpeeds.map((preset) => (
                      <Button
                        key={`alt-ul-${preset.value}`}
                        variant="outline"
                        size="sm"
                        className="text-xs h-6 px-2"
                        onClick={() => applyPreset("altUpload", preset.value)}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>
              </div>
            </div>

            {speedLimits && (
              <div className="text-xs text-muted-foreground p-3 bg-muted rounded">
                <strong>Current Status:</strong><br />
                Mode: {speedLimits.alternativeSpeedLimitsEnabled ? "Alternative" : "Global"}<br />
                Active Download: {formatSpeed(speedLimits.alternativeSpeedLimitsEnabled ? speedLimits.altDownloadLimit : speedLimits.downloadLimit)}<br />
                Active Upload: {formatSpeed(speedLimits.alternativeSpeedLimitsEnabled ? speedLimits.altUploadLimit : speedLimits.uploadLimit)}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button 
            onClick={handleSave} 
            disabled={isLoading || setSpeedLimitsMutation.isPending}
          >
            {setSpeedLimitsMutation.isPending ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
                Saving...
              </>
            ) : (
              "Save Changes"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}