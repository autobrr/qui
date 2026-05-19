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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { APIError, api } from "@/lib/api"
import type { ExternalProgram, ExternalProgramCreate, ExternalProgramUpdate, PathMapping } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Edit, Plus, Trash2, X } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

// Type for automation references in delete conflict response
interface AutomationReference {
  id: number
  instanceId: number
  name: string
}

function getErrorMessage(error: unknown): string {
  if (error instanceof APIError) return error.message
  if (error instanceof Error) return error.message
  return "Unknown error"
}

export function ExternalProgramsManager() {
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editProgram, setEditProgram] = useState<ExternalProgram | null>(null)
  const [deleteProgram, setDeleteProgram] = useState<ExternalProgram | null>(null)
  const [deleteConflict, setDeleteConflict] = useState<AutomationReference[] | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // Fetch external programs
  const { data: programs, isLoading, error } = useQuery({
    queryKey: ["externalPrograms"],
    queryFn: () => api.listExternalPrograms(),
    staleTime: 30 * 1000, // 30 seconds
  })

  const createMutation = useMutation({
    mutationFn: async (data: ExternalProgramCreate) => {
      return api.createExternalProgram(data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      setShowCreateDialog(false)
      toast.success("External program created successfully")
    },
    onError: (error: unknown) => {
      toast.error(`Failed to create external program: ${getErrorMessage(error)}`)
    },
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: number; data: ExternalProgramUpdate }) => {
      return api.updateExternalProgram(id, data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      setEditProgram(null)
      toast.success("External program updated successfully")
    },
    onError: (error: unknown) => {
      toast.error(`Failed to update external program: ${getErrorMessage(error)}`)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async ({ id, force }: { id: number; force?: boolean }) => {
      return api.deleteExternalProgram(id, force)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      // Also invalidate automations since we may have modified them
      queryClient.invalidateQueries({ queryKey: ["automations"] })
      setDeleteProgram(null)
      setDeleteConflict(null)
      toast.success("External program deleted successfully")
    },
    onError: (error: unknown) => {
      if (error instanceof APIError && error.status === 409) {
        const data = error.data as { automations?: AutomationReference[] } | undefined
        if (data?.automations) {
          setDeleteConflict(data.automations)
          return
        }
      }
      toast.error(`Failed to delete external program: ${getErrorMessage(error)}`)
    },
  })

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm" className="w-full sm:w-auto">
              <Plus className="mr-2 h-4 w-4" />
              Create External Program
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>Create External Program</DialogTitle>
              <DialogDescription>
                Configure an external program or script that can be executed from the torrent context menu.
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0">
              <ProgramForm
                onSubmit={(data) => createMutation.mutate(data)}
                onCancel={() => setShowCreateDialog(false)}
                isPending={createMutation.isPending}
              />
            </div>
          </DialogContent>
        </Dialog>
      </div>

      {isLoading && <div className="text-center py-8">Loading external programs...</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">Failed to load external programs</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!programs || programs.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              No external programs configured. Create one to get started.
            </div>
          </CardContent>
        </Card>
      )}

      {programs && programs.length > 0 && (
        <div className="grid gap-4">
          {programs.map((program) => (
            <Card className="bg-muted/40" key={program.id}>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between">
                  <div className="space-y-1 flex-1">
                    <div className="flex items-center gap-2">
                      <CardTitle className="text-lg">{program.name}</CardTitle>
                      <Badge variant={program.enabled ? "default" : "secondary"}>
                        {program.enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </div>
                    <CardDescription className="text-xs">
                      Created {formatDate(new Date(program.created_at))}
                      {program.updated_at !== program.created_at &&
                        ` • Updated ${formatDate(new Date(program.updated_at))}`}
                    </CardDescription>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditProgram(program)}
                      aria-label={`Edit ${program.name}`}
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteProgram(program)}
                      aria-label={`Delete ${program.name}`}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div>
                  <div className="text-sm font-medium mb-1">Program Path:</div>
                  <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                    {program.path}
                  </code>
                </div>
                {program.args_template && (
                  <div>
                    <div className="text-sm font-medium mb-1">Arguments Template:</div>
                    <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                      {program.args_template}
                    </code>
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {/* Edit Dialog */}
      {editProgram && (
        <Dialog open={true} onOpenChange={() => setEditProgram(null)}>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>Edit External Program</DialogTitle>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0">
              <ProgramForm
                program={editProgram}
                onSubmit={(data) => updateMutation.mutate({ id: editProgram.id, data })}
                onCancel={() => setEditProgram(null)}
                isPending={updateMutation.isPending}
              />
            </div>
          </DialogContent>
        </Dialog>
      )}

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={deleteProgram !== null} onOpenChange={(open) => {
        if (!open) {
          setDeleteProgram(null)
          setDeleteConflict(null)
        }
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete External Program</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3">
                {deleteConflict ? (
                  <>
                    <p className="text-amber-600 dark:text-amber-500">
                      This program is used by the following automation rules:
                    </p>
                    <ul className="list-disc list-inside text-sm space-y-1 max-h-32 overflow-y-auto">
                      {deleteConflict.map((ref) => (
                        <li key={ref.id}>{ref.name}</li>
                      ))}
                    </ul>
                    <p>
                      If you proceed, the external program action will be removed from these automation rules.
                    </p>
                  </>
                ) : (
                  <p>Are you sure you want to delete "{deleteProgram?.name}"? This action cannot be undone.</p>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteProgram && deleteMutation.mutate({ id: deleteProgram.id, force: deleteConflict !== null })}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={deleteMutation.isPending}
            >
              {deleteConflict ? "Delete Anyway" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface ProgramFormProps {
  program?: ExternalProgram
  onSubmit: (data: ExternalProgramCreate | ExternalProgramUpdate) => void
  onCancel: () => void
  isPending: boolean
}

function ProgramForm({ program, onSubmit, onCancel, isPending }: ProgramFormProps) {
  const [name, setName] = useState(program?.name || "")
  const [path, setPath] = useState(program?.path || "")
  const [argsTemplate, setArgsTemplate] = useState(program?.args_template || "")
  const [enabled, setEnabled] = useState(program?.enabled !== false)
  const [useTerminal, setUseTerminal] = useState(program?.use_terminal !== false)
  const [pathMappings, setPathMappings] = useState<PathMapping[]>(program?.path_mappings || [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error("Name is required")
      return
    }

    if (!path.trim()) {
      toast.error("Program path is required")
      return
    }

    // Filter out empty path mappings
    const validPathMappings = pathMappings.filter(
      (mapping) => mapping.from.trim() !== "" && mapping.to.trim() !== ""
    )

    onSubmit({
      name: name.trim(),
      path: path.trim(),
      args_template: argsTemplate.trim(),
      enabled,
      use_terminal: useTerminal,
      path_mappings: validPathMappings,
    })
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="name">Name *</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="My External Program"
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="path">Program Path *</Label>
        <Input
          id="path"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/usr/bin/my-script.sh or C:\Scripts\my-script.bat"
          required
        />
        <p className="text-xs text-muted-foreground">
          Full path to the executable
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="args">Arguments Template</Label>
        <Textarea
          id="args"
          value={argsTemplate}
          onChange={(e) => setArgsTemplate(e.target.value)}
          placeholder='"{hash}" "{name}" --save-path "{save_path}"'
          rows={3}
        />
        <div className="text-xs text-muted-foreground space-y-1">
          <div>Full path to script with arguments</div>
          <div>Available placeholders:</div>
          <ul className="list-disc list-inside pl-2 space-y-0.5">
            <li><code className="bg-muted px-1 rounded">{"{hash}"}</code> - Torrent hash</li>
            <li><code className="bg-muted px-1 rounded">{"{name}"}</code> - Torrent name</li>
            <li><code className="bg-muted px-1 rounded">{"{save_path}"}</code> - Save path</li>
            <li><code className="bg-muted px-1 rounded">{"{content_path}"}</code> - Content path</li>
            <li><code className="bg-muted px-1 rounded">{"{category}"}</code> - Category</li>
            <li><code className="bg-muted px-1 rounded">{"{tags}"}</code> - Tags (comma-separated)</li>
            <li><code className="bg-muted px-1 rounded">{"{state}"}</code> - Torrent state</li>
            <li><code className="bg-muted px-1 rounded">{"{size}"}</code> - Size in bytes</li>
            <li><code className="bg-muted px-1 rounded">{"{progress}"}</code> - Progress (0-1)</li>
            <li><code className="bg-muted px-1 rounded">{"{comment}"}</code> - Torrent comment</li>
          </ul>
        </div>
      </div>

      <div className="space-y-2">
        <Label>Path Mappings</Label>
        <div className="space-y-2">
          {pathMappings.map((mapping, index) => (
            <div key={index} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input
                  placeholder="Remote path (e.g., /mnt/remote-storage)"
                  value={mapping.from}
                  onChange={(e) => {
                    const newMappings = [...pathMappings]
                    newMappings[index] = { ...newMappings[index], from: e.target.value }
                    setPathMappings(newMappings)
                  }}
                />
              </div>
              <div className="flex-1">
                <Input
                  placeholder="Local path (e.g., /home/user/mounts/remote)"
                  value={mapping.to}
                  onChange={(e) => {
                    const newMappings = [...pathMappings]
                    newMappings[index] = { ...newMappings[index], to: e.target.value }
                    setPathMappings(newMappings)
                  }}
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  const newMappings = pathMappings.filter((_, i) => i !== index)
                  setPathMappings(newMappings)
                }}
                aria-label="Remove path mapping"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              setPathMappings([...pathMappings, { from: "", to: "" }])
            }}
          >
            <Plus className="mr-2 h-4 w-4" />
            Add Path Mapping
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Path mappings convert remote paths to local mount points. Useful when running external programs on a local qui server while qBittorrent is remote.
          Paths are matched by longest prefix first. Use the same path separator style as the remote qBittorrent instance (/ for Linux, \ for Windows).
        </p>
      </div>

      <div className="space-y-3">
        <div className="flex items-center space-x-2">
          <Switch
            id="useTerminal"
            checked={useTerminal}
            onCheckedChange={setUseTerminal}
          />
          <Label htmlFor="useTerminal" className="cursor-pointer">
            Launch in terminal window
          </Label>
        </div>
        <p className="text-xs text-muted-foreground ml-9">
          When enabled, the program will open in a new terminal window. Disable for GUI applications or programs that don't need terminal output.
        </p>

        <div className="flex items-center space-x-2">
          <Switch
            id="enabled"
            checked={enabled}
            onCheckedChange={setEnabled}
          />
          <Label htmlFor="enabled" className="cursor-pointer">
            Enable this program
          </Label>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving..." : program ? "Update" : "Create"}
        </Button>
      </div>
    </form>
  )
}
