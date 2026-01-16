/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useInstances } from "@/hooks/useInstances"
import { cn, formatErrorMessage } from "@/lib/utils"
import type { Instance } from "@/types"
import { Clock, Cog, Folder, Gauge, MoreVertical, Power, Radar, Server, Settings, Trash2, Upload, Wifi } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { AdvancedNetworkForm } from "./AdvancedNetworkForm"
import { ConnectionSettingsForm } from "./ConnectionSettingsForm"
import { FileManagementForm } from "./FileManagementForm"
import { InstanceSettingsPanel } from "./InstanceSettingsPanel"
import { NetworkDiscoveryForm } from "./NetworkDiscoveryForm"
import { QueueManagementForm } from "./QueueManagementForm"
import { SeedingLimitsForm } from "./SeedingLimitsForm"
import { SpeedLimitsForm } from "./SpeedLimitsForm"

interface InstancePreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
  instance?: Instance
  defaultTab?: string
}

export function InstancePreferencesDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  instance,
  defaultTab,
}: InstancePreferencesDialogProps) {
  const {
    instances,
    deleteInstance,
    setInstanceStatus,
    isDeleting,
    isUpdatingStatus,
    updatingStatusId,
  } = useInstances()
  const currentInstance = instances?.find(i => i.id === instanceId) ?? instance
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)

  const handleSuccess = () => {
    // Keep dialog open after successful updates
    // Users might want to configure multiple sections
  }

  const handleDeleted = () => {
    // Close dialog when instance is deleted
    onOpenChange(false)
  }

  const handleToggleStatus = () => {
    if (!currentInstance) return
    const nextState = !currentInstance.isActive
    setInstanceStatus({ id: currentInstance.id, isActive: nextState }, {
      onSuccess: () => {
        toast.success(nextState ? "Instance Enabled" : "Instance Disabled", {
          description: nextState ? "qui will resume connecting to this qBittorrent instance." : "qui will stop attempting to reach this qBittorrent instance.",
        })
      },
      onError: (error) => {
        toast.error("Status Update Failed", {
          description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to update instance status",
        })
      },
    })
  }

  const handleDelete = () => {
    if (!currentInstance) return
    deleteInstance({ id: currentInstance.id, name: currentInstance.name }, {
      onSuccess: () => {
        toast.success("Instance Deleted", {
          description: `Successfully deleted "${currentInstance.name}"`,
        })
        setShowDeleteDialog(false)
        handleDeleted()
      },
      onError: (error) => {
        toast.error("Delete Failed", {
          description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to delete instance",
        })
        setShowDeleteDialog(false)
      },
    })
  }

  const isStatusUpdating = currentInstance && isUpdatingStatus && updatingStatusId === currentInstance.id

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-6xl max-h-[90vh] overflow-y-auto top-[5%] left-[50%] translate-x-[-50%] translate-y-0">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Cog className="h-5 w-5" />
              <span>Instance Settings</span>
              {currentInstance && (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 w-6 p-0 ml-1"
                      aria-label="Instance actions"
                    >
                      <MoreVertical className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start">
                    <DropdownMenuItem
                      onClick={handleToggleStatus}
                      disabled={isStatusUpdating}
                    >
                      <Power className={cn("mr-2 h-4 w-4", !currentInstance.isActive && "text-destructive")} />
                      {isStatusUpdating ? "Updating..." : currentInstance.isActive ? "Disable Instance" : "Enable Instance"}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onClick={() => setShowDeleteDialog(true)}
                      disabled={isDeleting}
                      className="text-destructive"
                    >
                      <Trash2 className="mr-2 h-4 w-4" />
                      Delete Instance
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DialogTitle>
            <DialogDescription>
              Configure all settings and preferences for <strong className="truncate max-w-xs inline-block align-bottom" title={instanceName}>{instanceName}</strong>
            </DialogDescription>
          </DialogHeader>

          <Tabs defaultValue={defaultTab ?? "instance"} className="w-full">
            <TabsList className="grid w-full grid-cols-8">
              <TabsTrigger value="instance" className="flex items-center gap-2">
                <Server className="h-4 w-4" />
                <span className="hidden sm:inline">Instance</span>
              </TabsTrigger>
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

            <TabsContent value="instance" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Instance Configuration</h3>
                <p className="text-sm text-muted-foreground">
                  Configure connection settings, authentication, and access options
                </p>
              </div>
              {currentInstance ? (
                <InstanceSettingsPanel instance={currentInstance} onSuccess={handleSuccess} />
              ) : (
                <p className="text-sm text-muted-foreground py-8 text-center">
                  Instance data not available. Please close and reopen this dialog.
                </p>
              )}
            </TabsContent>

            <TabsContent value="speed" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Speed Limits</h3>
                <p className="text-sm text-muted-foreground">
                  Configure download and upload speed limits
                </p>
              </div>
              <SpeedLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="queue" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Queue Management</h3>
                <p className="text-sm text-muted-foreground">
                  Configure torrent queue settings and active torrent limits
                </p>
              </div>
              <QueueManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="files" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">File Management</h3>
                <p className="text-sm text-muted-foreground">
                  Configure file paths and torrent management settings
                </p>
              </div>
              <FileManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="seeding" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Seeding Limits</h3>
                <p className="text-sm text-muted-foreground">
                  Configure share ratio and seeding time limits
                </p>
              </div>
              <SeedingLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="connection" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Connection Settings</h3>
                <p className="text-sm text-muted-foreground">
                  Configure listening port, protocol settings, and connection limits
                </p>
              </div>
              <ConnectionSettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="discovery" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">Network Discovery</h3>
                <p className="text-sm text-muted-foreground">
                  Configure peer discovery protocols and tracker settings
                </p>
              </div>
              <NetworkDiscoveryForm instanceId={instanceId} onSuccess={handleSuccess} />
            </TabsContent>

            <TabsContent value="advanced" className="mt-6">
              <div className="space-y-1 mb-6">
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

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Instance</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{instanceName}"? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={isDeleting}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
