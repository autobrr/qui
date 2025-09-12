/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
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
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { useInstances } from "@/hooks/useInstances"
import { useIncognitoMode } from "@/lib/incognito"
import { cn, formatErrorMessage } from "@/lib/utils"
import type { InstanceResponse } from "@/types"
import {
  CheckCircle,
  Edit,
  Eye,
  EyeOff,
  MoreVertical,
  RefreshCw,
  Trash2,
  XCircle
} from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

interface InstanceCardProps {
  instance: InstanceResponse
  onEdit: () => void
}

export function InstanceCard({ instance, onEdit }: InstanceCardProps) {
  const { deleteInstance, testConnection, isDeleting, isTesting } = useInstances()
  const [testResult, setTestResult] = useState<{ success: boolean; message: string | undefined } | null>(null)
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const displayUrl = instance.host


  const handleTest = async () => {
    setTestResult(null)
    try {
      const result = await testConnection(instance.id)
      // Convert connected to success for consistency with component state
      const testResult = { success: result.connected, message: result.message }
      setTestResult(testResult)

      if (result.connected) {
        toast.success("Test Connection Successful", {
          description: result.message || "Successfully connected to qBittorrent instance",
        })
      } else {
        toast.error("Test Connection Failed", {
          description: result.message ? formatErrorMessage(result.message) : "Could not connect to qBittorrent instance",
        })
      }
    } catch (error) {
      const message = "Connection failed"
      setTestResult({ success: false, message })
      toast.error("Test Connection Failed", {
        description: error instanceof Error ? formatErrorMessage(error.message) : message,
      })
    }
  }

  const handleDelete = () => {
    deleteInstance({ id: instance.id, name: instance.name }, {
      onSuccess: () => {
        toast.success("Instance Deleted", {
          description: `Successfully deleted "${instance.name}"`,
        })
        setShowDeleteDialog(false)
      },
      onError: (error) => {
        toast.error("Delete Failed", {
          description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to delete instance",
        })
        setShowDeleteDialog(false)
      },
    })
  }

  return (
    <Card>
      <div>
        <CardHeader className="flex flex-row items-center justify-between pr-2 space-y-0">
          <div className="flex-1 max-w-45">
            <CardTitle className="text-base font-medium truncate" title={instance.name}>
              {instance.name}
            </CardTitle>
          </div>
          <div className="flex items-center gap-1">
            <Badge
              variant={instance.connected ? "default" : "destructive"}
            >
              {instance.connected ? "Connected" : "Disconnected"}
            </Badge>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="h-8 w-8 p-0">
                  <MoreVertical className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={onEdit}>
                  <Edit className="mr-2 h-4 w-4" />
                  Edit
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleTest} disabled={isTesting}>
                  <RefreshCw className="mr-2 h-4 w-4" />
                  Test Connection
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => setShowDeleteDialog(true)}
                  disabled={isDeleting}
                  className="text-destructive"
                >
                  <Trash2 className="mr-2 h-4 w-4" />
                  Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </CardHeader>
        <CardDescription className="flex items-center gap-1 text-sm pl-6 pr-8">
          <span
            className={incognitoMode ? "blur-sm select-none truncate" : "truncate"}
            title={displayUrl}
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
            <span className="text-muted-foreground">Username:</span>
            {/* qBittorrent's default username is 'admin' */}
            <span>{instance.username || "admin"}</span>
          </div>
          {instance.basicUsername && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">Basic Auth:</span>
              <span>{instance.basicUsername}</span>
            </div>
          )}
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
            <span>Testing connection...</span>
          </div>
        )}
      </CardContent>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Instance</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{instance.name}"? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}