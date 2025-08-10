/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState } from 'react'
import type { Instance } from '@/types'
import { toast } from 'sonner'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { 
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { 
  MoreVertical, 
  Edit, 
  Trash2, 
  RefreshCw, 
  CheckCircle,
  XCircle
} from 'lucide-react'
import { useInstances } from '@/hooks/useInstances'
import { cn } from '@/lib/utils'

interface InstanceCardProps {
  instance: Instance
  onEdit: () => void
}

export function InstanceCard({ instance, onEdit }: InstanceCardProps) {
  const { deleteInstance, testConnection, isDeleting, isTesting } = useInstances()
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)

  const handleTest = async () => {
    setTestResult(null)
    try {
      const result = await testConnection(instance.id)
      // Convert connected to success for consistency with component state
      const testResult = { success: result.connected, message: result.message }
      setTestResult(testResult)
      
      if (result.connected) {
        toast.success('Test Connection Successful', {
          description: result.message || 'Successfully connected to qBittorrent instance'
        })
      } else {
        toast.error('Test Connection Failed', {
          description: result.message || 'Could not connect to qBittorrent instance'
        })
      }
    } catch (error) {
      const message = 'Connection failed'
      setTestResult({ success: false, message })
      toast.error('Test Connection Failed', {
        description: error instanceof Error ? error.message : message
      })
    }
  }

  const handleDelete = async () => {
    if (confirm(`Are you sure you want to delete "${instance.name}"?`)) {
      try {
        await deleteInstance(instance.id)
        toast.success('Instance Deleted', {
          description: `Successfully deleted "${instance.name}"`
        })
      } catch (error) {
        toast.error('Delete Failed', {
          description: error instanceof Error ? error.message : 'Failed to delete instance'
        })
      }
    }
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <div>
          <CardTitle className="text-base font-medium">
            {instance.name}
          </CardTitle>
          <CardDescription className="text-sm">
            {instance.host}:{instance.port}
          </CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <Badge 
            variant="outline"
          >
            Configured
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
      <CardContent>
        <div className="space-y-1 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">Username:</span>
            <span>{instance.username}</span>
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
            <span>{testResult.message}</span>
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