/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
import { useTranslation } from "react-i18next"

interface InstancePreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function InstancePreferencesDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: InstancePreferencesDialogProps) {
  const { t } = useTranslation()
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
            {t("instancePreferences.title")}
          </DialogTitle>
          <DialogDescription className="flex items-center gap-1">
            {t("instancePreferences.description", { name: instanceName })}
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="speed" className="w-full">
          <TabsList className="grid w-full grid-cols-7">
            <TabsTrigger value="speed" className="flex items-center gap-2">
              <Gauge className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.speed")}</span>
            </TabsTrigger>
            <TabsTrigger value="queue" className="flex items-center gap-2">
              <Clock className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.queue")}</span>
            </TabsTrigger>
            <TabsTrigger value="files" className="flex items-center gap-2">
              <Folder className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.files")}</span>
            </TabsTrigger>
            <TabsTrigger value="seeding" className="flex items-center gap-2">
              <Upload className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.seeding")}</span>
            </TabsTrigger>
            <TabsTrigger value="connection" className="flex items-center gap-2">
              <Wifi className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.connection")}</span>
            </TabsTrigger>
            <TabsTrigger value="discovery" className="flex items-center gap-2">
              <Radar className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.discovery")}</span>
            </TabsTrigger>
            <TabsTrigger value="advanced" className="flex items-center gap-2">
              <Settings className="h-4 w-4" />
              <span className="hidden sm:inline">{t("instancePreferences.tabs.advanced")}</span>
            </TabsTrigger>
          </TabsList>

          <TabsContent value="speed" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.speedLimits.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.speedLimits.description")}
              </p>
            </div>
            <SpeedLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="queue" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.queueManagement.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.queueManagement.description")}
              </p>
            </div>
            <QueueManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="files" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.fileManagement.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.fileManagement.description")}
              </p>
            </div>
            <FileManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="seeding" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.seedingLimits.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.seedingLimits.description")}
              </p>
            </div>
            <SeedingLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="connection" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.connectionSettings.description")}
              </p>
            </div>
            <ConnectionSettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="discovery" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.networkDiscovery.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.networkDiscovery.description")}
              </p>
            </div>
            <NetworkDiscoveryForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>

          <TabsContent value="advanced" className="space-y-4">
            <div className="space-y-2">
              <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.title")}</h3>
              <p className="text-sm text-muted-foreground">
                {t("instancePreferences.content.advancedSettings.description")}
              </p>
            </div>
            <AdvancedNetworkForm instanceId={instanceId} onSuccess={handleSuccess} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}