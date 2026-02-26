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
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

// Type for automation references in delete conflict response
interface AutomationReference {
  id: number
  instanceId: number
  name: string
}

function getErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message
  if (error instanceof Error) return error.message
  return fallback
}

export function ExternalProgramsManager() {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [editProgram, setEditProgram] = useState<ExternalProgram | null>(null)
  const [deleteProgram, setDeleteProgram] = useState<ExternalProgram | null>(null)
  const [deleteConflict, setDeleteConflict] = useState<AutomationReference[] | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  const { data: programs, isLoading, error } = useQuery({
    queryKey: ["externalPrograms"],
    queryFn: () => api.listExternalPrograms(),
    staleTime: 30 * 1000,
  })

  const createMutation = useMutation({
    mutationFn: async (data: ExternalProgramCreate) => {
      return api.createExternalProgram(data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      setShowCreateDialog(false)
      toast.success(tr("externalProgramsManager.toasts.created"))
    },
    onError: (error: unknown) => {
      toast.error(
        tr("externalProgramsManager.toasts.failedCreate", {
          error: getErrorMessage(error, tr("externalProgramsManager.common.unknownError")),
        })
      )
    },
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: number; data: ExternalProgramUpdate }) => {
      return api.updateExternalProgram(id, data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      setEditProgram(null)
      toast.success(tr("externalProgramsManager.toasts.updated"))
    },
    onError: (error: unknown) => {
      toast.error(
        tr("externalProgramsManager.toasts.failedUpdate", {
          error: getErrorMessage(error, tr("externalProgramsManager.common.unknownError")),
        })
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async ({ id, force }: { id: number; force?: boolean }) => {
      return api.deleteExternalProgram(id, force)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      queryClient.invalidateQueries({ queryKey: ["automations"] })
      setDeleteProgram(null)
      setDeleteConflict(null)
      toast.success(tr("externalProgramsManager.toasts.deleted"))
    },
    onError: (error: unknown) => {
      if (error instanceof APIError && error.status === 409) {
        const data = error.data as { automations?: AutomationReference[] } | undefined
        if (data?.automations) {
          setDeleteConflict(data.automations)
          return
        }
      }
      toast.error(
        tr("externalProgramsManager.toasts.failedDelete", {
          error: getErrorMessage(error, tr("externalProgramsManager.common.unknownError")),
        })
      )
    },
  })

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm" className="w-full sm:w-auto">
              <Plus className="mr-2 h-4 w-4" />
              {tr("externalProgramsManager.actions.createExternalProgram")}
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{tr("externalProgramsManager.dialogs.createTitle")}</DialogTitle>
              <DialogDescription>
                {tr("externalProgramsManager.dialogs.createDescription")}
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

      {isLoading && <div className="text-center py-8">{tr("externalProgramsManager.states.loading")}</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">{tr("externalProgramsManager.states.failedLoad")}</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!programs || programs.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              {tr("externalProgramsManager.states.empty")}
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
                        {program.enabled
                          ? tr("externalProgramsManager.values.enabled")
                          : tr("externalProgramsManager.values.disabled")}
                      </Badge>
                    </div>
                    <CardDescription className="text-xs">
                      {tr("externalProgramsManager.labels.createdOn", {
                        date: formatDate(new Date(program.created_at)),
                      })}
                      {program.updated_at !== program.created_at && (
                        <>
                          {" • "}
                          {tr("externalProgramsManager.labels.updatedOn", {
                            date: formatDate(new Date(program.updated_at)),
                          })}
                        </>
                      )}
                    </CardDescription>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditProgram(program)}
                      aria-label={tr("externalProgramsManager.aria.editProgram", { name: program.name })}
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteProgram(program)}
                      aria-label={tr("externalProgramsManager.aria.deleteProgram", { name: program.name })}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div>
                  <div className="text-sm font-medium mb-1">{tr("externalProgramsManager.labels.programPath")}</div>
                  <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                    {program.path}
                  </code>
                </div>
                {program.args_template && (
                  <div>
                    <div className="text-sm font-medium mb-1">
                      {tr("externalProgramsManager.labels.argumentsTemplate")}
                    </div>
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

      {editProgram && (
        <Dialog open={true} onOpenChange={() => setEditProgram(null)}>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{tr("externalProgramsManager.dialogs.editTitle")}</DialogTitle>
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

      <AlertDialog
        open={deleteProgram !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteProgram(null)
            setDeleteConflict(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{tr("externalProgramsManager.dialogs.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3">
                {deleteConflict ? (
                  <>
                    <p className="text-amber-600 dark:text-amber-500">
                      {tr("externalProgramsManager.dialogs.deleteConflictIntro")}
                    </p>
                    <ul className="list-disc list-inside text-sm space-y-1 max-h-32 overflow-y-auto">
                      {deleteConflict.map((ref) => (
                        <li key={ref.id}>{ref.name}</li>
                      ))}
                    </ul>
                    <p>{tr("externalProgramsManager.dialogs.deleteConflictOutro")}</p>
                  </>
                ) : (
                  <p>{tr("externalProgramsManager.dialogs.deleteConfirm", { name: deleteProgram?.name })}</p>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{tr("externalProgramsManager.actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleteProgram &&
                deleteMutation.mutate({ id: deleteProgram.id, force: deleteConflict !== null })
              }
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={deleteMutation.isPending}
            >
              {deleteConflict
                ? tr("externalProgramsManager.actions.deleteAnyway")
                : tr("externalProgramsManager.actions.delete")}
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
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const [name, setName] = useState(program?.name || "")
  const [path, setPath] = useState(program?.path || "")
  const [argsTemplate, setArgsTemplate] = useState(program?.args_template || "")
  const [enabled, setEnabled] = useState(program?.enabled !== false)
  const [useTerminal, setUseTerminal] = useState(program?.use_terminal !== false)
  const [pathMappings, setPathMappings] = useState<PathMapping[]>(program?.path_mappings || [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error(tr("externalProgramsManager.fieldErrors.nameRequired"))
      return
    }

    if (!path.trim()) {
      toast.error(tr("externalProgramsManager.fieldErrors.programPathRequired"))
      return
    }

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
        <Label htmlFor="name">{tr("externalProgramsManager.labels.nameRequiredMark")}</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={tr("externalProgramsManager.placeholders.name")}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="path">{tr("externalProgramsManager.labels.programPathRequiredMark")}</Label>
        <Input
          id="path"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder={tr("externalProgramsManager.placeholders.programPath")}
          required
        />
        <p className="text-xs text-muted-foreground">
          {tr("externalProgramsManager.labels.fullPathToExecutable")}
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="args">{tr("externalProgramsManager.labels.argumentsTemplateLabel")}</Label>
        <Textarea
          id="args"
          value={argsTemplate}
          onChange={(e) => setArgsTemplate(e.target.value)}
          placeholder={tr("externalProgramsManager.placeholders.argsTemplate")}
          rows={3}
        />
        <div className="text-xs text-muted-foreground space-y-1">
          <div>{tr("externalProgramsManager.labels.fullPathToScriptWithArguments")}</div>
          <div>{tr("externalProgramsManager.labels.availablePlaceholders")}</div>
          <ul className="list-disc list-inside pl-2 space-y-0.5">
            <li><code className="bg-muted px-1 rounded">{"{hash}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.hash")}</li>
            <li><code className="bg-muted px-1 rounded">{"{name}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.name")}</li>
            <li><code className="bg-muted px-1 rounded">{"{save_path}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.savePath")}</li>
            <li><code className="bg-muted px-1 rounded">{"{content_path}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.contentPath")}</li>
            <li><code className="bg-muted px-1 rounded">{"{category}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.category")}</li>
            <li><code className="bg-muted px-1 rounded">{"{tags}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.tags")}</li>
            <li><code className="bg-muted px-1 rounded">{"{state}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.state")}</li>
            <li><code className="bg-muted px-1 rounded">{"{size}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.size")}</li>
            <li><code className="bg-muted px-1 rounded">{"{progress}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.progress")}</li>
            <li><code className="bg-muted px-1 rounded">{"{comment}"}</code> - {tr("externalProgramsManager.placeholderDescriptions.comment")}</li>
          </ul>
        </div>
      </div>

      <div className="space-y-2">
        <Label>{tr("externalProgramsManager.labels.pathMappings")}</Label>
        <div className="space-y-2">
          {pathMappings.map((mapping, index) => (
            <div key={index} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input
                  placeholder={tr("externalProgramsManager.placeholders.remotePath")}
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
                  placeholder={tr("externalProgramsManager.placeholders.localPath")}
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
                aria-label={tr("externalProgramsManager.aria.removePathMapping")}
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
            {tr("externalProgramsManager.actions.addPathMapping")}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          {tr("externalProgramsManager.labels.pathMappingsHelp")}
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
            {tr("externalProgramsManager.labels.launchInTerminal")}
          </Label>
        </div>
        <p className="text-xs text-muted-foreground ml-9">
          {tr("externalProgramsManager.labels.launchInTerminalHelp")}
        </p>

        <div className="flex items-center space-x-2">
          <Switch
            id="enabled"
            checked={enabled}
            onCheckedChange={setEnabled}
          />
          <Label htmlFor="enabled" className="cursor-pointer">
            {tr("externalProgramsManager.labels.enableProgram")}
          </Label>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
          {tr("externalProgramsManager.actions.cancel")}
        </Button>
        <Button type="submit" disabled={isPending}>
          {isPending
            ? tr("externalProgramsManager.actions.saving")
            : program
              ? tr("externalProgramsManager.actions.saveUpdate")
              : tr("externalProgramsManager.actions.saveCreate")}
        </Button>
      </div>
    </form>
  )
}
