/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { useIncognitoMode } from "@/lib/incognito"
import { cn, formatErrorMessage } from "@/lib/utils"
import type { InstanceResponse } from "@/types"
import {
  ArrowDown,
  ArrowUp,
  CheckCircle,
  Edit,
  Eye,
  EyeOff,
  HardDrive,
  MoreVertical,
  Power,
  RefreshCw,
  Trash2,
  XCircle
} from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface InstanceCardProps {
  instance: InstanceResponse
  onEdit: () => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  disableMoveUp?: boolean
  disableMoveDown?: boolean
}

export function InstanceCard({
  instance,
  onEdit,
  onMoveUp,
  onMoveDown,
  disableMoveUp = false,
  disableMoveDown = false,
}: InstanceCardProps) {
  const { t } = useTranslation("instances")
  const {
    deleteInstance,
    testConnection,
    setInstanceStatus,
    isDeleting,
    isTesting,
    isUpdatingStatus,
    updatingStatusId,
  } = useInstances()
  const [testResult, setTestResult] = useState<{ success: boolean; message: string | undefined } | null>(null)
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const displayUrl = instance.host

  const statusBadge = !instance.isActive? { label: t("card.status.disabled"), variant: "secondary" as const }: instance.connected? { label: t("card.status.connected"), variant: "default" as const }: { label: t("card.status.disconnected"), variant: "destructive" as const }

  const handleTest = async () => {
    if (!instance.isActive) {
      toast.error(t("card.toast.instanceDisabledTitle"), {
        description: t("card.toast.testDisabledDescription"),
      })
      return
    }

    setTestResult(null)
    try {
      const result = await testConnection(instance.id)
      // Convert connected to success for consistency with component state
      const testResult = { success: result.connected, message: result.message }
      setTestResult(testResult)

      if (result.connected) {
        toast.success(t("card.toast.testSuccessTitle"), {
          description: result.message || t("card.toast.testSuccessDescription"),
        })
      } else {
        toast.error(t("card.toast.testFailedTitle"), {
          description: result.message ? formatErrorMessage(result.message) : t("card.toast.testFailedDescription"),
        })
      }
    } catch (error) {
      const message = t("card.toast.connectionFailed")
      setTestResult({ success: false, message })
      toast.error(t("card.toast.testFailedTitle"), {
        description: error instanceof Error ? formatErrorMessage(error.message) : message,
      })
    }
  }

  const handleToggleStatus = () => {
    const nextState = !instance.isActive
    setInstanceStatus({ id: instance.id, isActive: nextState }, {
      onSuccess: () => {
        setTestResult(null)
        toast.success(nextState ? t("card.toast.instanceEnabledTitle") : t("card.toast.instanceDisabledTitle"), {
          description: nextState? t("card.toast.instanceEnabledDescription"): t("card.toast.instanceDisabledDescription"),
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
    deleteInstance({ id: instance.id, name: instance.name }, {
      onSuccess: () => {
        toast.success(t("card.toast.instanceDeletedTitle"), {
          description: t("card.toast.instanceDeletedDescription", { name: instance.name }),
        })
        setShowDeleteDialog(false)
      },
      onError: (error) => {
        toast.error(t("card.toast.deleteFailedTitle"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : t("card.toast.deleteFailedDescription"),
        })
        setShowDeleteDialog(false)
      },
    })
  }

  return (
    <Card className="bg-muted/40">
      <div>
        <CardHeader className="flex flex-row items-center justify-between pr-2 space-y-0">
          <div className="flex-1 min-w-0 overflow-hidden">
            <CardTitle className="text-base font-medium truncate" title={instance.name}>
              {instance.name}
            </CardTitle>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            <Badge variant={statusBadge.variant}>
              {statusBadge.label}
            </Badge>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant={instance.isActive ? "ghost" : "outline"}
                  size="icon"
                  className={cn("h-8 w-8 p-0")}
                  disabled={isUpdatingStatus && updatingStatusId === instance.id}
                  aria-pressed={instance.isActive}
                  aria-label={instance.isActive ? t("card.tooltips.disableInstance") : t("card.tooltips.enableInstance")}
                  onClick={(event) => {
                    event.preventDefault()
                    event.stopPropagation()
                    handleToggleStatus()
                  }}
                >
                  <Power className={cn("h-4 w-4", isUpdatingStatus && updatingStatusId === instance.id && "animate-pulse", !instance.isActive && "text-destructive")} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {instance.isActive ? t("card.tooltips.disableInstance") : t("card.tooltips.enableInstance")}
              </TooltipContent>
            </Tooltip>
            {(onMoveUp || onMoveDown) && (
              <div className="flex items-center gap-1">
                {onMoveUp && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 p-0"
                        disabled={disableMoveUp}
                        aria-label={t("card.tooltips.moveUp")}
                        onClick={(event) => {
                          event.preventDefault()
                          event.stopPropagation()
                          if (!disableMoveUp) {
                            onMoveUp()
                          }
                        }}
                      >
                        <ArrowUp className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("card.tooltips.moveUp")}</TooltipContent>
                  </Tooltip>
                )}
                {onMoveDown && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 p-0"
                        disabled={disableMoveDown}
                        aria-label={t("card.tooltips.moveDown")}
                        onClick={(event) => {
                          event.preventDefault()
                          event.stopPropagation()
                          if (!disableMoveDown) {
                            onMoveDown()
                          }
                        }}
                      >
                        <ArrowDown className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("card.tooltips.moveDown")}</TooltipContent>
                  </Tooltip>
                )}
              </div>
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="h-8 w-8 p-0">
                  <MoreVertical className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={onEdit}>
                  <Edit className="mr-2 h-4 w-4" />
                  {t("card.actions.edit")}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleTest} disabled={isTesting || !instance.isActive}>
                  <RefreshCw className="mr-2 h-4 w-4" />
                  {t("card.actions.testConnection")}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => setShowDeleteDialog(true)}
                  disabled={isDeleting}
                  className="text-destructive"
                >
                  <Trash2 className="mr-2 h-4 w-4" />
                  {t("card.actions.delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </CardHeader>
        <CardDescription className="flex items-center gap-1 text-sm pl-6 pr-8">
          <span
            className={incognitoMode ? "blur-sm select-none truncate" : "truncate"}
            {...(!incognitoMode && { title: displayUrl })}
          >
            {displayUrl}
          </span>
          <Button
            variant="ghost"
            size="icon"
            className="h-4 w-4 hover:bg-muted/50"
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setIncognitoMode(!incognitoMode)
            }}
          >
            {incognitoMode ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
          </Button>
        </CardDescription>
      </div>
      <CardContent>
        <div className="space-y-1 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">{t("card.labels.username")}</span>
            {/* qBittorrent's default username is 'admin' */}
            <span className={incognitoMode ? "blur-sm select-none" : ""}>
              {instance.username || "admin"}
            </span>
          </div>
          {instance.basicUsername && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">{t("card.labels.basicAuth")}</span>
              <span className={incognitoMode ? "blur-sm select-none" : ""}>
                {instance.basicUsername}
              </span>
            </div>
          )}
          <div className="flex justify-between">
            <span className="text-muted-foreground">{t("card.labels.tlsVerification")}</span>
            <span className={instance.tlsSkipVerify ? "text-amber-500" : ""}>
              {instance.tlsSkipVerify ? t("card.labels.tlsSkipped") : t("card.labels.tlsStrict")}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-muted-foreground">{t("card.labels.localFileAccess")}</span>
            <span className={cn(
              "flex items-center gap-1",
              instance.hasLocalFilesystemAccess ? "text-primary" : "text-muted-foreground"
            )}>
              <HardDrive className="h-3 w-3" />
              {instance.hasLocalFilesystemAccess ? t("card.status.enabled") : t("card.status.disabled")}
            </span>
          </div>
        </div>

        <InstanceErrorDisplay instance={instance} onEdit={onEdit} showEditButton={true} compact />

        {testResult && (
          <div className={cn(
            "mt-4 flex items-center gap-2 text-sm",
            testResult.success ? "text-primary" : "text-destructive"
          )}>
            {testResult.success ? (
              <CheckCircle className="h-4 w-4" />
            ) : (
              <XCircle className="h-4 w-4" />
            )}
            <span>{testResult.success ? testResult.message : formatErrorMessage(testResult.message)}</span>
          </div>
        )}

        {isTesting && (
          <div className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
            <RefreshCw className="h-4 w-4 animate-spin" />
            <span>{t("card.labels.testingConnection")}</span>
          </div>
        )}
      </CardContent>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("card.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("card.deleteDialog.description", { name: instance.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("card.deleteDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("card.deleteDialog.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}
