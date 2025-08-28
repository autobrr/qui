/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ConnectionSettingsForm } from "./ConnectionSettingsForm"
import { NetworkDiscoveryForm } from "./NetworkDiscoveryForm"
{/*import { ProxySettingsForm } from "./ProxySettingsForm" */}
import { AdvancedNetworkForm } from "./AdvancedNetworkForm"
import { Wifi, Radar, Settings } from "lucide-react"

interface ConnectionSettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function ConnectionSettingsDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: ConnectionSettingsDialogProps) {
  const handleSuccess = () => {
    // Keep dialog open after successful updates
    // Users might want to configure multiple sections
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-6xl max-h-[90vh] overflow-y-auto top-[5%] left-[50%] translate-x-[-50%] translate-y-0">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Wifi className="h-5 w-5" />
            Connection Settings
          </DialogTitle>
          <DialogDescription>
            Configure network and connection settings for <strong>{instanceName}</strong>
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="connection" className="w-full">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="connection" className="flex items-center gap-2">
              <Wifi className="h-4 w-4" />
              <span className="hidden sm:inline">Connection</span>
            </TabsTrigger>
            <TabsTrigger value="discovery" className="flex items-center gap-2">
              <Radar className="h-4 w-4" />
              <span className="hidden sm:inline">Discovery</span>
            </TabsTrigger>
            {/* 
            <TabsTrigger value="proxy" className="flex items-center gap-2">
              <Shield className="h-4 w-4" />
              <span className="hidden sm:inline">Proxy</span>
            </TabsTrigger>
            */}
            <TabsTrigger value="advanced" className="flex items-center gap-2">
              <Settings className="h-4 w-4" />
              <span className="hidden sm:inline">Advanced</span>
            </TabsTrigger>
          </TabsList>

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

          {/* 
          <TabsContent value="proxy" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">Proxy Settings</h3>
              <p className="text-sm text-muted-foreground">
                Configure proxy server for routing BitTorrent connections
              </p>
            </div>
            <ProxySettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>
          */}

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