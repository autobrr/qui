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
import { Clock, Cog, Folder, Gauge, MoreVertical, Power, Radar, RefreshCw, Server, Settings, Trash2, Upload, Wifi } from "lucide-react"
import { Component, lazy, Suspense, useCallback, useMemo, useState, type ErrorInfo, type ReactNode } from "react"
import { Trans, useTranslation } from "react-i18next"

import { toast } from "sonner"

// Lazy load tab content components - only Instance tab is eagerly loaded
import { InstanceSettingsPanel } from "./InstanceSettingsPanel"

/** Loading fallback for lazy-loaded tab content */
function TabLoadingFallback() {
  const { t } = useTranslation("common")
  const tr = (key: string) => String(t(key as never))
  return (
    <div className="flex items-center justify-center py-12" role="status" aria-live="polite">
      <div className="text-sm text-muted-foreground">{tr("instancePreferencesDialog.loading")}</div>
    </div>
  )
}

/** Error fallback for lazy-loaded tab content */
function TabErrorFallback({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation("common")
  const tr = (key: string) => String(t(key as never))
  return (
    <div className="flex flex-col items-center justify-center py-12 gap-4" role="alert">
      <p className="text-sm text-muted-foreground">{tr("instancePreferencesDialog.loadFailed")}</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        <RefreshCw className="mr-2 h-4 w-4" />
        {tr("instancePreferencesDialog.actions.retry")}
      </Button>
    </div>
  )
}

/** Error boundary for lazy-loaded tab content */
interface TabErrorBoundaryProps {
  children: ReactNode
  onRetry?: () => void
}

interface TabErrorBoundaryState {
  hasError: boolean
}

class TabErrorBoundary extends Component<TabErrorBoundaryProps, TabErrorBoundaryState> {
  constructor(props: TabErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false }
  }

  static getDerivedStateFromError(): TabErrorBoundaryState {
    return { hasError: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error("Tab content failed to load:", error, errorInfo)
  }

  handleRetry = () => {
    this.setState({ hasError: false })
    this.props.onRetry?.()
  }

  render() {
    if (this.state.hasError) {
      return <TabErrorFallback onRetry={this.handleRetry} />
    }

    return this.props.children
  }
}

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
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const {
    instances,
    deleteInstance,
    setInstanceStatus,
    isDeleting,
    isUpdatingStatus,
    updatingStatusId,
  } = useInstances()
  const currentInstance = instances?.find(i => i.id === instanceId) ?? instance
  const displayInstanceName = currentInstance?.name ?? instanceName
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [lazyRetryKey, setLazyRetryKey] = useState(0)

  const handleLazyRetry = useCallback(() => {
    setLazyRetryKey((prev) => prev + 1)
  }, [])

  const SpeedLimitsForm = useMemo(
    () => lazy(() => import("./SpeedLimitsForm").then(m => ({ default: m.SpeedLimitsForm }))),
    [lazyRetryKey],
  )
  const QueueManagementForm = useMemo(
    () => lazy(() => import("./QueueManagementForm").then(m => ({ default: m.QueueManagementForm }))),
    [lazyRetryKey],
  )
  const FileManagementForm = useMemo(
    () => lazy(() => import("./FileManagementForm").then(m => ({ default: m.FileManagementForm }))),
    [lazyRetryKey],
  )
  const SeedingLimitsForm = useMemo(
    () => lazy(() => import("./SeedingLimitsForm").then(m => ({ default: m.SeedingLimitsForm }))),
    [lazyRetryKey],
  )
  const ConnectionSettingsForm = useMemo(
    () => lazy(() => import("./ConnectionSettingsForm").then(m => ({ default: m.ConnectionSettingsForm }))),
    [lazyRetryKey],
  )
  const NetworkDiscoveryForm = useMemo(
    () => lazy(() => import("./NetworkDiscoveryForm").then(m => ({ default: m.NetworkDiscoveryForm }))),
    [lazyRetryKey],
  )
  const AdvancedNetworkForm = useMemo(
    () => lazy(() => import("./AdvancedNetworkForm").then(m => ({ default: m.AdvancedNetworkForm }))),
    [lazyRetryKey],
  )

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
        toast.success(nextState ? tr("instancePreferencesDialog.toasts.instanceEnabled") : tr("instancePreferencesDialog.toasts.instanceDisabled"), {
          description: nextState
            ? tr("instancePreferencesDialog.toasts.instanceEnabledDescription")
            : tr("instancePreferencesDialog.toasts.instanceDisabledDescription"),
        })
      },
      onError: (error) => {
        toast.error(tr("instancePreferencesDialog.toasts.statusUpdateFailed"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : tr("instancePreferencesDialog.toasts.failedUpdateInstanceStatus"),
        })
      },
    })
  }

  const handleDelete = () => {
    if (!currentInstance) return
    deleteInstance({ id: currentInstance.id, name: currentInstance.name }, {
      onSuccess: () => {
        toast.success(tr("instancePreferencesDialog.toasts.instanceDeleted"), {
          description: tr("instancePreferencesDialog.toasts.instanceDeletedDescription", { name: currentInstance.name }),
        })
        setShowDeleteDialog(false)
        handleDeleted()
      },
      onError: (error) => {
        toast.error(tr("instancePreferencesDialog.toasts.deleteFailed"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : tr("instancePreferencesDialog.toasts.failedDeleteInstance"),
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
              <span>{tr("instancePreferencesDialog.title")}</span>
              {currentInstance && (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-9 w-9 p-0 ml-1"
                      aria-label={tr("instancePreferencesDialog.aria.instanceActions")}
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
                      {isStatusUpdating
                        ? tr("instancePreferencesDialog.actions.updating")
                        : currentInstance.isActive
                          ? tr("instancePreferencesDialog.actions.disableInstance")
                          : tr("instancePreferencesDialog.actions.enableInstance")}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onClick={() => setShowDeleteDialog(true)}
                      disabled={isDeleting}
                      className="text-destructive"
                    >
                      <Trash2 className="mr-2 h-4 w-4" />
                      {tr("instancePreferencesDialog.actions.deleteInstance")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DialogTitle>
            <DialogDescription>
              <Trans
                i18nKey="instancePreferencesDialog.description"
                ns="common"
                values={{ name: displayInstanceName }}
                components={{
                  name: <strong className="truncate max-w-xs inline-block align-bottom" title={displayInstanceName} />,
                }}
              />
            </DialogDescription>
          </DialogHeader>

          <Tabs defaultValue={defaultTab ?? "instance"} className="w-full">
            {/* Scrollable container with fade indicators */}
            <div className="relative">
              {/* Left fade indicator */}
              <div className="absolute left-0 top-0 bottom-0 w-4 bg-gradient-to-r from-background to-transparent z-10 pointer-events-none sm:hidden" />
              {/* Right fade indicator */}
              <div className="absolute right-0 top-0 bottom-0 w-4 bg-gradient-to-l from-background to-transparent z-10 pointer-events-none sm:hidden" />

              <TabsList className="flex w-full overflow-x-auto -mx-1 px-1 h-11 sm:h-9">
                <TabsTrigger value="instance" className="flex items-center gap-1.5 shrink-0">
                  <Server className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.instance")}</span>
                </TabsTrigger>
                <div className="h-6 w-px bg-muted-foreground/50 mx-1 sm:mx-2 self-center shrink-0" />
                <TabsTrigger value="speed" className="flex items-center gap-1.5 shrink-0">
                  <Gauge className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.speed")}</span>
                </TabsTrigger>
                <TabsTrigger value="queue" className="flex items-center gap-1.5 shrink-0">
                  <Clock className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.queue")}</span>
                </TabsTrigger>
                <TabsTrigger value="files" className="flex items-center gap-1.5 shrink-0">
                  <Folder className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.files")}</span>
                </TabsTrigger>
                <TabsTrigger value="seeding" className="flex items-center gap-1.5 shrink-0">
                  <Upload className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.seeding")}</span>
                </TabsTrigger>
                <TabsTrigger value="connection" className="flex items-center gap-1.5 shrink-0">
                  <Wifi className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.connection")}</span>
                </TabsTrigger>
                <TabsTrigger value="discovery" className="flex items-center gap-1.5 shrink-0">
                  <Radar className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.discovery")}</span>
                </TabsTrigger>
                <TabsTrigger value="advanced" className="flex items-center gap-1.5 shrink-0">
                  <Settings className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{tr("instancePreferencesDialog.tabs.advanced")}</span>
                </TabsTrigger>
              </TabsList>
            </div>

            <TabsContent value="instance" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.instance.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.instance.description")}
                </p>
              </div>
              {currentInstance ? (
                <InstanceSettingsPanel instance={currentInstance} onSuccess={handleSuccess} />
              ) : (
                <p className="text-sm text-muted-foreground py-8 text-center">
                  {tr("instancePreferencesDialog.instanceDataUnavailable")}
                </p>
              )}
            </TabsContent>

            <TabsContent value="speed" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.speed.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.speed.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <SpeedLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="queue" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.queue.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.queue.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <QueueManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="files" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.files.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.files.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <FileManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="seeding" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.seeding.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.seeding.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <SeedingLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="connection" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.connection.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.connection.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <ConnectionSettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="discovery" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.discovery.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.discovery.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <NetworkDiscoveryForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

            <TabsContent value="advanced" className="mt-6">
              <div className="space-y-1 mb-6">
                <h3 className="text-lg font-medium">{tr("instancePreferencesDialog.sections.advanced.title")}</h3>
                <p className="text-sm text-muted-foreground">
                  {tr("instancePreferencesDialog.sections.advanced.description")}
                </p>
              </div>
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <AdvancedNetworkForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </TabsContent>

          </Tabs>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{tr("instancePreferencesDialog.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("instancePreferencesDialog.deleteDialog.description", { name: displayInstanceName })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("instancePreferencesDialog.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={isDeleting}
            >
              {tr("instancePreferencesDialog.actions.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
