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

import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// Lazy load tab content components - only Instance tab is eagerly loaded
import { InstanceSettingsPanel } from "./InstanceSettingsPanel"

/** Loading fallback for lazy-loaded tab content */
function TabLoadingFallback() {
  const { t } = useTranslation("instances")
  return (
    <div className="flex items-center justify-center py-12" role="status" aria-live="polite">
      <div className="text-sm text-muted-foreground">{t("preferences.dialog.loadingFallback")}</div>
    </div>
  )
}

/** Error fallback for lazy-loaded tab content */
function TabErrorFallback({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation("instances")
  return (
    <div className="flex flex-col items-center justify-center py-12 gap-4" role="alert">
      <p className="text-sm text-muted-foreground">{t("preferences.dialog.errorFallback")}</p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        <RefreshCw className="mr-2 h-4 w-4" />
        {t("preferences.dialog.retry")}
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

interface PreferencesTabSectionProps {
  value: string
  title: string
  description: string
  children: ReactNode
}

function PreferencesTabSection({ value, title, description, children }: PreferencesTabSectionProps) {
  return (
    <TabsContent value={value} className="mt-6 flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="space-y-1 mb-6 shrink-0">
        <h3 className="text-lg font-medium">{title}</h3>
        <p className="text-sm text-muted-foreground">
          {description}
        </p>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {children}
      </div>
    </TabsContent>
  )
}

export function InstancePreferencesDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  instance,
  defaultTab,
}: InstancePreferencesDialogProps) {
  const { t } = useTranslation("instances")
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
    [lazyRetryKey]
  )
  const QueueManagementForm = useMemo(
    () => lazy(() => import("./QueueManagementForm").then(m => ({ default: m.QueueManagementForm }))),
    [lazyRetryKey]
  )
  const FileManagementForm = useMemo(
    () => lazy(() => import("./FileManagementForm").then(m => ({ default: m.FileManagementForm }))),
    [lazyRetryKey]
  )
  const SeedingLimitsForm = useMemo(
    () => lazy(() => import("./SeedingLimitsForm").then(m => ({ default: m.SeedingLimitsForm }))),
    [lazyRetryKey]
  )
  const ConnectionSettingsForm = useMemo(
    () => lazy(() => import("./ConnectionSettingsForm").then(m => ({ default: m.ConnectionSettingsForm }))),
    [lazyRetryKey]
  )
  const NetworkDiscoveryForm = useMemo(
    () => lazy(() => import("./NetworkDiscoveryForm").then(m => ({ default: m.NetworkDiscoveryForm }))),
    [lazyRetryKey]
  )
  const AdvancedNetworkForm = useMemo(
    () => lazy(() => import("./AdvancedNetworkForm").then(m => ({ default: m.AdvancedNetworkForm }))),
    [lazyRetryKey]
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
        toast.success(nextState ? t("card.toast.instanceEnabledTitle") : t("card.toast.instanceDisabledTitle"), {
          description: nextState ? t("card.toast.instanceEnabledDescription") : t("card.toast.instanceDisabledDescription"),
        })
      },
      onError: (error) => {
        toast.error(t("card.toast.statusUpdateFailedTitle"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : t("card.toast.statusUpdateFailedDescription"),
        })
      },
    })
  }

  const handleDelete = () => {
    if (!currentInstance) return
    deleteInstance({ id: currentInstance.id, name: currentInstance.name }, {
      onSuccess: () => {
        toast.success(t("card.toast.instanceDeletedTitle"), {
          description: t("card.toast.instanceDeletedDescription", { name: currentInstance.name }),
        })
        setShowDeleteDialog(false)
        handleDeleted()
      },
      onError: (error) => {
        toast.error(t("card.toast.deleteFailedTitle"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : t("card.toast.deleteFailedDescription"),
        })
        setShowDeleteDialog(false)
      },
    })
  }

  const isStatusUpdating = currentInstance && isUpdatingStatus && updatingStatusId === currentInstance.id

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-6xl max-h-[90vh] flex flex-col overflow-hidden top-[5%] left-[50%] translate-x-[-50%] translate-y-0">
          <DialogHeader className="shrink-0">
            <DialogTitle className="flex items-center gap-2">
              <Cog className="h-5 w-5" />
              <span>{t("preferences.dialog.title")}</span>
              {currentInstance && (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-9 w-9 p-0 ml-1"
                      aria-label={t("preferences.dialog.actions.instanceActions")}
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
                      {isStatusUpdating ? t("preferences.dialog.actions.updating") : currentInstance.isActive ? t("preferences.dialog.actions.disableInstance") : t("preferences.dialog.actions.enableInstance")}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onClick={() => setShowDeleteDialog(true)}
                      disabled={isDeleting}
                      className="text-destructive"
                    >
                      <Trash2 className="mr-2 h-4 w-4" />
                      {t("preferences.dialog.actions.deleteInstance")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </DialogTitle>
            <DialogDescription>
              {t("preferences.dialog.configureDescription")} <strong className="truncate max-w-xs inline-block align-bottom" title={displayInstanceName}>{displayInstanceName}</strong>
            </DialogDescription>
          </DialogHeader>

          <Tabs defaultValue={defaultTab ?? "instance"} className="flex w-full min-h-0 flex-1 flex-col">
            <div className="relative shrink-0">
              <TabsList className="flex w-full justify-start overflow-x-auto h-11 sm:h-9">
                <TabsTrigger value="instance" className="flex items-center gap-1.5 shrink-0">
                  <Server className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.instance")}</span>
                </TabsTrigger>
                <div className="h-6 w-px bg-muted-foreground/50 mx-1 sm:mx-2 self-center shrink-0" />
                <TabsTrigger value="speed" className="flex items-center gap-1.5 shrink-0">
                  <Gauge className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.speed")}</span>
                </TabsTrigger>
                <TabsTrigger value="queue" className="flex items-center gap-1.5 shrink-0">
                  <Clock className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.queue")}</span>
                </TabsTrigger>
                <TabsTrigger value="files" className="flex items-center gap-1.5 shrink-0">
                  <Folder className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.files")}</span>
                </TabsTrigger>
                <TabsTrigger value="seeding" className="flex items-center gap-1.5 shrink-0">
                  <Upload className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.seeding")}</span>
                </TabsTrigger>
                <TabsTrigger value="connection" className="flex items-center gap-1.5 shrink-0">
                  <Wifi className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.connection")}</span>
                </TabsTrigger>
                <TabsTrigger value="discovery" className="flex items-center gap-1.5 shrink-0">
                  <Radar className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.discovery")}</span>
                </TabsTrigger>
                <TabsTrigger value="advanced" className="flex items-center gap-1.5 shrink-0">
                  <Settings className="h-4 w-4" />
                  <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.advanced")}</span>
                </TabsTrigger>
              </TabsList>
            </div>

            <PreferencesTabSection
              value="instance"
              title={t("preferences.dialog.sections.instanceConfig.title")}
              description={t("preferences.dialog.sections.instanceConfig.description")}
            >
              {currentInstance ? (
                <InstanceSettingsPanel instance={currentInstance} onSuccess={handleSuccess} />
              ) : (
                <p className="text-sm text-muted-foreground py-8 text-center">
                  {t("preferences.dialog.instanceNotAvailable")}
                </p>
              )}
            </PreferencesTabSection>

            <PreferencesTabSection
              value="speed"
              title={t("preferences.dialog.sections.speedLimits.title")}
              description={t("preferences.dialog.sections.speedLimits.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <SpeedLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="queue"
              title={t("preferences.dialog.sections.queueManagement.title")}
              description={t("preferences.dialog.sections.queueManagement.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <QueueManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="files"
              title={t("preferences.dialog.sections.fileManagement.title")}
              description={t("preferences.dialog.sections.fileManagement.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <FileManagementForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="seeding"
              title={t("preferences.dialog.sections.seedingLimits.title")}
              description={t("preferences.dialog.sections.seedingLimits.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <SeedingLimitsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="connection"
              title={t("preferences.dialog.sections.connectionSettings.title")}
              description={t("preferences.dialog.sections.connectionSettings.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <ConnectionSettingsForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="discovery"
              title={t("preferences.dialog.sections.networkDiscovery.title")}
              description={t("preferences.dialog.sections.networkDiscovery.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <NetworkDiscoveryForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

            <PreferencesTabSection
              value="advanced"
              title={t("preferences.dialog.sections.advancedSettings.title")}
              description={t("preferences.dialog.sections.advancedSettings.description")}
            >
              <TabErrorBoundary onRetry={handleLazyRetry}>
                <Suspense fallback={<TabLoadingFallback />}>
                  <AdvancedNetworkForm instanceId={instanceId} onSuccess={handleSuccess} />
                </Suspense>
              </TabErrorBoundary>
            </PreferencesTabSection>

          </Tabs>
        </DialogContent>
      </Dialog>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.dialog.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("preferences.dialog.deleteDialog.description", { name: displayInstanceName })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.dialog.deleteDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={isDeleting}
            >
              {t("preferences.dialog.deleteDialog.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
