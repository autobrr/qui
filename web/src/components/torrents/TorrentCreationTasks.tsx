/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { api } from "@/lib/api"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { getTorrentTaskPollInterval } from "@/lib/torrent-task-polling"
import type { TorrentCreationStatus, TorrentCreationTask } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { CheckCircle2, Clock, Download, Loader2, Trash2, XCircle } from "lucide-react"
import { toast } from "sonner"

interface TorrentCreationTasksProps {
  instanceId: number
}

const STATUS_COLORS: Record<TorrentCreationStatus, string> = {
  Queued: "bg-yellow-500 p-1",
  Running: "bg-blue-500 p-1",
  Finished: "bg-green-500 p-1",
  Failed: "bg-red-500 p-1",
}

const STATUS_ICONS: Record<TorrentCreationStatus, React.ReactNode> = {
  Queued: <Clock className="h-4 w-4" />,
  Running: <Loader2 className="h-4 w-4 animate-spin" />,
  Finished: <CheckCircle2 className="h-4 w-4" />,
  Failed: <XCircle className="h-4 w-4" />,
}

export function TorrentCreationTasks({ instanceId }: TorrentCreationTasksProps) {
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // Keep table responsive while tasks run and ease off polling once queue clears
  const { data: tasks, isLoading } = useQuery({
    queryKey: ["torrent-creation-tasks", instanceId],
    queryFn: () => api.getTorrentCreationTasks(instanceId),
    refetchInterval: (query) =>
      getTorrentTaskPollInterval(query.state.data as TorrentCreationTask[] | undefined, {
        activeInterval: 2000,
      }),
    refetchIntervalInBackground: true,
  })

  const downloadMutation = useMutation({
    mutationFn: (taskID: string) => api.downloadTorrentFile(instanceId, taskID),
    onSuccess: () => {
      toast.success("Torrent file download started")
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to download torrent file")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (taskID: string) => api.deleteTorrentCreationTask(instanceId, taskID),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["torrent-creation-tasks", instanceId] })
      queryClient.invalidateQueries({ queryKey: ["active-task-count", instanceId] })
      toast.success("Torrent creation task deleted")
    },
    onError: (error: Error) => {
      toast.error(error.message || "Failed to delete torrent creation task")
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
      {/* Mobile/Tablet Card Layout */}
      <div className="lg:hidden space-y-3 p-4">
        {tasks.map((task) => (
          <div
            key={task.taskID}
            className="border rounded-lg p-4 space-y-3 bg-card"
          >
            {/* Header with source and status */}
            <div className="flex items-start justify-between gap-3">
              <div className="flex-1 min-w-0 space-y-1">
                <div className="font-medium text-sm break-words" title={task.sourcePath}>
                  {task.sourcePath.split("/").pop() || task.sourcePath}
                </div>
                <div className="flex flex-wrap gap-2items-center">
                  <Badge variant="outline" className={STATUS_COLORS[task.status]}>
                    <span className="flex items-center gap-1">
                      {STATUS_ICONS[task.status]}
                      {task.status}
                    </span>
                  </Badge>
                  {task.private && (
                    <Badge variant="outline" className="text-xs">
                      Private
                    </Badge>
                  )}
                </div>
              </div>
            </div>

            {/* Progress bar for running tasks */}
            {task.status === "Running" && task.progress !== undefined && (
              <div className="space-y-1.5">
                <Progress value={task.progress} className="h-2" />
                <div className="text-xs text-muted-foreground">
                  {Math.round(task.progress)}%
                </div>
              </div>
            )}

            {/* Error message */}
            {task.errorMessage && (
              <div className="text-xs text-destructive break-words">
                {task.errorMessage}
              </div>
            )}

            {/* Footer with date and actions */}
            <div className="flex items-center justify-between gap-3 pt-2 border-t">
              <div className="text-xs text-muted-foreground">
                {formatDate(new Date(task.timeAdded))}
              </div>
              <div className="flex gap-2">
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
            </div>
          </div>
        ))}
      </div>

      {/* Desktop Table Layout */}
      <div className="hidden lg:block">
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
                    <div className="font-medium truncate max-w-sm lg:max-w-md xl:max-w-lg 2xl:max-w-lg" title={task.sourcePath}>
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
                    {formatDate(new Date(task.timeAdded))}
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
    </div>
  )
}
