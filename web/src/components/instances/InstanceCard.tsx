/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import type { InstanceResponse } from "@/types"
import { toast } from "sonner"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import {
  MoreVertical,
  Edit,
  Trash2,
  RefreshCw,
  CheckCircle,
  XCircle,
  Eye,
  EyeOff
} from "lucide-react"
import { useInstances } from "@/hooks/useInstances"
import { cn, formatErrorMessage } from "@/lib/utils"
import { useIncognitoMode } from "@/lib/incognito"

interface InstanceCardProps {
  instance: InstanceResponse
  onEdit: () => void
}

export function InstanceCard({ instance, onEdit }: InstanceCardProps) {
  const { deleteInstance, testConnection, isDeleting, isTesting } = useInstances()
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
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
    if (confirm(`Are you sure you want to delete "${instance.name}"?`)) {
      deleteInstance({ id: instance.id, name: instance.name }, {
        onSuccess: () => {
          toast.success("Instance Deleted", {
            description: `Successfully deleted "${instance.name}"`,
          })
        },
        onError: (error) => {
          toast.error("Delete Failed", {
            description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to delete instance",
          })
        },
      })
    }
  }

  return (
    <Card>
      <div>
        <CardHeader className="flex flex-row items-center justify-between pr-2 space-y-0">
          <div>
            <CardTitle className="text-base font-medium">
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
                  onClick={handleDelete}
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
          {instance.lastConnectedAt && (
            <div className="flex justify-between">
              <span className="text-muted-foreground">Last connected:</span>
              <span>{new Date(instance.lastConnectedAt).toLocaleString()}</span>
            </div>
          )}
        </div>

        <InstanceErrorDisplay instance={instance} onEdit={onEdit} showEditButton={true} />

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
    </Card>
  )
}