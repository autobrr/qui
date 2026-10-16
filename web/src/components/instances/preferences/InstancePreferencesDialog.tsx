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
import { formatErrorMessage } from "@/lib/format-error-message"
import { cn } from "@/lib/utils"
import type { Instance, PreferenceField } from "@/types"
import { Cog, Download, Gauge, Globe, MoreVertical, Power, Radar, RefreshCw, Rss, Server, Settings, SlidersHorizontal, Trash2, Wifi, type LucideIcon } from "lucide-react"
import { useState } from "react"

import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { InstanceSettingsPanel } from "./InstanceSettingsPanel"
import { preferenceTabLabels } from "./labels"
import { PreferencesForm } from "./PreferencesForm"
import { preferenceTabsOf, usePreferenceView } from "./preferenceView"

const TAB_ICONS: Record<PreferenceField["tab"], LucideIcon> = {
  behavior: SlidersHorizontal,
  downloads: Download,
  connection: Wifi,
  speed: Gauge,
  bittorrent: Radar,
  rss: Rss,
  webui: Globe,
  advanced: Settings,
}

interface InstancePreferencesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
  instance?: Instance
  defaultTab?: string
}

// Mounts with the dialog content, so that the view is fetched each time the dialog opens.
function PreferenceTabs({ instanceId, instance, defaultTab }: { instanceId: number; instance?: Instance; defaultTab?: string }) {
  const { t } = useTranslation("instances")
  const view = usePreferenceView(instanceId)
  // Seeded only from a view fetched after the dialog opened, so that it does not start from an old cache.
  const fields = view.isFetchedAfterMount ? view.data : undefined
  const tabs = fields ? preferenceTabsOf(fields) : []

  return (
    <Tabs defaultValue={defaultTab ?? "instance"} className="flex w-full min-h-0 flex-1 flex-col">
      <div className="relative shrink-0">
        <TabsList className="flex w-full justify-start overflow-x-auto h-11 sm:h-9">
          <TabsTrigger value="instance" className="flex items-center gap-1.5 shrink-0">
            <Server className="h-4 w-4" />
            <span className="text-xs sm:text-sm">{t("preferences.dialog.tabs.instance")}</span>
          </TabsTrigger>
          <div className="h-6 w-px bg-muted-foreground/50 mx-1 sm:mx-2 self-center shrink-0" />
          {tabs.map(tab => {
            const Icon = TAB_ICONS[tab]
            return (
              <TabsTrigger key={tab} value={tab} className="flex items-center gap-1.5 shrink-0">
                <Icon className="h-4 w-4" />
                <span className="text-xs sm:text-sm">{t(preferenceTabLabels[tab].labelKey)}</span>
              </TabsTrigger>
            )
          })}
          {!fields && (view.isError ? (
            <span className="flex shrink-0 items-center gap-2 px-2 text-xs text-muted-foreground sm:text-sm" role="alert">
              {t("preferences.dialog.errorFallback")}
              <Button variant="ghost" size="sm" onClick={() => view.refetch()}>
                <RefreshCw className="mr-2 h-4 w-4" />
                {t("preferences.dialog.retry")}
              </Button>
            </span>
          ) : (
            <span className="shrink-0 px-2 text-xs text-muted-foreground sm:text-sm" role="status">
              {t("preferences.dialog.loadingFallback")}
            </span>
          ))}
        </TabsList>
      </div>

      <TabsContent value="instance" className="mt-6 flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="space-y-1 mb-6 shrink-0">
          <h3 className="text-lg font-medium">{t("preferences.dialog.sections.instanceConfig.title")}</h3>
          <p className="text-sm text-muted-foreground">{t("preferences.dialog.sections.instanceConfig.description")}</p>
        </div>
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {instance ? (
            <InstanceSettingsPanel instance={instance} />
          ) : (
            <p className="text-sm text-muted-foreground py-8 text-center">
              {t("preferences.dialog.instanceNotAvailable")}
            </p>
          )}
        </div>
      </TabsContent>

      {fields && <PreferencesForm key={instanceId} instanceId={instanceId} fields={fields} />}
    </Tabs>
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

          <PreferenceTabs instanceId={instanceId} instance={currentInstance} defaultTab={defaultTab} />
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
