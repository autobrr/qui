/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { Download, Trash2, Loader2, CheckCircle2, XCircle, Clock } from "lucide-react"
import type { TorrentCreationStatus } from "@/types"

interface TorrentCreationTasksProps {
  instanceId: number
}

const STATUS_COLORS: Record<TorrentCreationStatus, string> = {
  Queued: "bg-yellow-500",
  Running: "bg-blue-500",
  Finished: "bg-green-500",
  Failed: "bg-red-500",
}

const STATUS_ICONS: Record<TorrentCreationStatus, React.ReactNode> = {
  Queued: <Clock className="h-4 w-4" />,
  Running: <Loader2 className="h-4 w-4 animate-spin" />,
  Finished: <CheckCircle2 className="h-4 w-4" />,
  Failed: <XCircle className="h-4 w-4" />,
}

export function TorrentCreationTasks({ instanceId }: TorrentCreationTasksProps) {
  const queryClient = useQueryClient()

  const { data: tasks, isLoading } = useQuery({
    queryKey: ["torrent-creation-tasks", instanceId],
    queryFn: () => api.getTorrentCreationTasks(instanceId),
    refetchInterval: (query) => {
      // Poll every 2 seconds if there are running or queued tasks
      const tasks = query.state.data
      if (tasks?.some((t) => t.status === "Running" || t.status === "Queued")) {
        return 2000
      }
      return false
    },
  })

  const downloadMutation = useMutation({
    mutationFn: (taskID: string) => api.downloadTorrentFile(instanceId, taskID),
  })

  const deleteMutation = useMutation({
    mutationFn: (taskID: string) => api.deleteTorrentCreationTask(instanceId, taskID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["torrent-creation-tasks", instanceId] })
    },
  })

  if (isLoading) {
    return (
      <div className="p-4 text-center text-muted-foreground">
        Loading tasks...
      </div>
    )
  }

  if (!tasks || tasks.length === 0) {
    return (
      <div className="p-4 text-center text-muted-foreground">
        No torrent creation tasks found
      </div>
    )
  }

  return (
    <div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Source</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Progress</TableHead>
            <TableHead>Added</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {tasks.map((task) => (
            <TableRow key={task.taskID}>
              <TableCell>
                <div className="space-y-1">
                  <div className="font-medium truncate max-w-md" title={task.sourcePath}>
                    {task.sourcePath.split("/").pop() || task.sourcePath}
                  </div>
                  {task.private && (
                    <Badge variant="outline" className="text-xs">
                      Private
                    </Badge>
                  )}
                  {task.errorMessage && (
                    <div className="text-xs text-destructive">{task.errorMessage}</div>
                  )}
                </div>
              </TableCell>
              <TableCell>
                <Badge variant="outline" className={STATUS_COLORS[task.status]}>
                  <span className="flex items-center gap-1">
                    {STATUS_ICONS[task.status]}
                    {task.status}
                  </span>
                </Badge>
              </TableCell>
              <TableCell>
                {task.status === "Running" && task.progress !== undefined ? (
                  <div className="space-y-1">
                    <Progress value={task.progress} className="w-32" />
                    <div className="text-xs text-muted-foreground">
                      {Math.round(task.progress)}%
                    </div>
                  </div>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell>
                <div className="text-sm">
                  {new Date(task.timeAdded).toLocaleString()}
                </div>
              </TableCell>
              <TableCell className="text-right">
                <div className="flex justify-end gap-2">
                  {task.status === "Finished" && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => downloadMutation.mutate(task.taskID)}
                      disabled={downloadMutation.isPending}
                    >
                      <Download className="h-4 w-4" />
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => deleteMutation.mutate(task.taskID)}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}