/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Clock, Cog, Folder, Gauge, Radar, Settings, Upload, Wifi } from "lucide-react"
import { AdvancedNetworkForm } from "./AdvancedNetworkForm"
import { ConnectionSettingsForm } from "./ConnectionSettingsForm"
import { FileManagementForm } from "./FileManagementForm"
import { NetworkDiscoveryForm } from "./NetworkDiscoveryForm"
import { QueueManagementForm } from "./QueueManagementForm"
import { SeedingLimitsForm } from "./SeedingLimitsForm"
import { SpeedLimitsForm } from "./SpeedLimitsForm"

interface InstancePreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
  defaultTab?: string
}

export function InstancePreferencesDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  defaultTab,
}: InstancePreferencesDialogProps) {
  const handleSuccess = () => {
    // Keep dialog open after successful updates
    // Users might want to configure multiple sections
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-6xl max-h-[90vh] overflow-y-auto top-[5%] left-[50%] translate-x-[-50%] translate-y-0">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Cog className="h-5 w-5" />
            Instance Preferences
          </DialogTitle>
          <DialogDescription className="flex items-center gap-1">
            Configure all settings and preferences for <strong className="truncate max-w-xs inline-block align-bottom" title={instanceName}>{instanceName}</strong>
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue={defaultTab ?? "speed"} className="w-full">
          <TabsList className="grid w-full grid-cols-7">
            <TabsTrigger value="speed" className="flex items-center gap-2">
              <Gauge className="h-4 w-4" />
              <span className="hidden sm:inline">Speed</span>
            </TabsTrigger>
            <TabsTrigger value="queue" className="flex items-center gap-2">
              <Clock className="h-4 w-4" />
              <span className="hidden sm:inline">Queue</span>
            </TabsTrigger>
            <TabsTrigger value="files" className="flex items-center gap-2">
              <Folder className="h-4 w-4" />
              <span className="hidden sm:inline">Files</span>
            </TabsTrigger>
            <TabsTrigger value="seeding" className="flex items-center gap-2">
              <Upload className="h-4 w-4" />
              <span className="hidden sm:inline">Seeding</span>
            </TabsTrigger>
            <TabsTrigger value="connection" className="flex items-center gap-2">
              <Wifi className="h-4 w-4" />
              <span className="hidden sm:inline">Connection</span>
            </TabsTrigger>
            <TabsTrigger value="discovery" className="flex items-center gap-2">
              <Radar className="h-4 w-4" />
              <span className="hidden sm:inline">Discovery</span>
            </TabsTrigger>
            <TabsTrigger value="advanced" className="flex items-center gap-2">
              <Settings className="h-4 w-4" />
              <span className="hidden sm:inline">Advanced</span>
            </TabsTrigger>
          </TabsList>

          <TabsContent value="speed" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Speed Limits</h3>
              <p className="text-sm text-muted-foreground">
                Configure download and upload speed limits for this qBittorrent instance
              </p>
            </div>
            <SpeedLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="queue" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Queue Management</h3>
              <p className="text-sm text-muted-foreground">
                Configure torrent queue settings and active torrent limits
              </p>
            </div>
            <QueueManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="files" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">File Management</h3>
              <p className="text-sm text-muted-foreground">
                Configure file paths and torrent management settings
              </p>
            </div>
            <FileManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="seeding" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Seeding Limits</h3>
              <p className="text-sm text-muted-foreground">
                Configure share ratio and seeding time limits
              </p>
            </div>
            <SeedingLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="connection" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Connection Settings</h3>
              <p className="text-sm text-muted-foreground">
                Configure listening port, protocol settings, and connection limits
              </p>
            </div>
            <ConnectionSettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="discovery" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Network Discovery</h3>
              <p className="text-sm text-muted-foreground">
                Configure peer discovery protocols and tracker settings
              </p>
            </div>
            <NetworkDiscoveryForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="advanced" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Advanced Settings</h3>
              <p className="text-sm text-muted-foreground">
                Performance tuning, disk I/O, peer management, and security settings
              </p>
            </div>
            <AdvancedNetworkForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
