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

const UNKNOWN_ERROR_KEY = "externalPrograms.unknownError"

function getErrorMessage(error: unknown): string {
  if (error instanceof APIError) return error.message
  if (error instanceof Error) return error.message
  return UNKNOWN_ERROR_KEY
}

export function ExternalProgramsManager() {
  const { t } = useTranslation("settings")
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
      toast.success(t("externalPrograms.toasts.created"))
    },
    onError: (error: unknown) => {
      toast.error(t("externalPrograms.toasts.createFailed", { error: t(getErrorMessage(error)) }))
    },
  })

  const updateMutation = useMutation({
    mutationFn: async ({ id, data }: { id: number; data: ExternalProgramUpdate }) => {
      return api.updateExternalProgram(id, data)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["externalPrograms"] })
      setEditProgram(null)
      toast.success(t("externalPrograms.toasts.updated"))
    },
    onError: (error: unknown) => {
      toast.error(t("externalPrograms.toasts.updateFailed", { error: t(getErrorMessage(error)) }))
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
      toast.success(t("externalPrograms.toasts.deleted"))
    },
    onError: (error: unknown) => {
      if (error instanceof APIError && error.status === 409) {
        const data = error.data as { automations?: AutomationReference[] } | undefined
        if (data?.automations) {
          setDeleteConflict(data.automations)
          return
        }
      }
      toast.error(t("externalPrograms.toasts.deleteFailed", { error: t(getErrorMessage(error)) }))
    },
  })

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm" className="w-full sm:w-auto">
              <Plus className="mr-2 h-4 w-4" />
              {t("externalPrograms.createButton")}
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-2xl max-w-full max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{t("externalPrograms.createTitle")}</DialogTitle>
              <DialogDescription>
                {t("externalPrograms.createDescription")}
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

      {isLoading && <div className="text-center py-8">{t("externalPrograms.loading")}</div>}
      {error && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-destructive">{t("externalPrograms.loadFailed")}</div>
          </CardContent>
        </Card>
      )}

      {!isLoading && !error && (!programs || programs.length === 0) && (
        <Card>
          <CardContent className="pt-6">
            <div className="text-center text-muted-foreground">
              {t("externalPrograms.empty")}
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
                        {program.enabled ? t("externalPrograms.enabled") : t("externalPrograms.disabled")}
                      </Badge>
                    </div>
                    <CardDescription className="text-xs">
                      {t("externalPrograms.created", { date: formatDate(new Date(program.created_at)) })}
                      {program.updated_at !== program.created_at &&
                        ` • ${t("externalPrograms.updated", { date: formatDate(new Date(program.updated_at)) })}`}
                    </CardDescription>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setEditProgram(program)}
                      aria-label={t("externalPrograms.ariaLabels.edit", { name: program.name })}
                    >
                      <Edit className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteProgram(program)}
                      aria-label={t("externalPrograms.ariaLabels.delete", { name: program.name })}
                    >
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-2">
                <div>
                  <div className="text-sm font-medium mb-1">{t("externalPrograms.programPath")}</div>
                  <code className="text-xs bg-muted px-2 py-1 rounded block break-all">
                    {program.path}
                  </code>
                </div>
                {program.args_template && (
                  <div>
                    <div className="text-sm font-medium mb-1">{t("externalPrograms.argumentsTemplate")}</div>
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
              <DialogTitle>{t("externalPrograms.editTitle")}</DialogTitle>
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
            <AlertDialogTitle>{t("externalPrograms.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3">
                {deleteConflict ? (
                  <>
                    <p className="text-amber-600 dark:text-amber-500">
                      {t("externalPrograms.deleteConflictIntro")}
                    </p>
                    <ul className="list-disc list-inside text-sm space-y-1 max-h-32 overflow-y-auto">
                      {deleteConflict.map((ref) => (
                        <li key={ref.id}>{ref.name}</li>
                      ))}
                    </ul>
                    <p>
                      {t("externalPrograms.deleteConflictDescription")}
                    </p>
                  </>
                ) : (
                  <p>{t("externalPrograms.deleteDescription", { name: deleteProgram?.name ?? "" })}</p>
                )}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("externalPrograms.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteProgram && deleteMutation.mutate({ id: deleteProgram.id, force: deleteConflict !== null })}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={deleteMutation.isPending}
            >
              {deleteConflict ? t("externalPrograms.deleteAnyway") : t("externalPrograms.delete")}
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
  const { t } = useTranslation("settings")
  const [name, setName] = useState(program?.name || "")
  const [path, setPath] = useState(program?.path || "")
  const [argsTemplate, setArgsTemplate] = useState(program?.args_template || "")
  const [enabled, setEnabled] = useState(program?.enabled !== false)
  const [useTerminal, setUseTerminal] = useState(program?.use_terminal !== false)
  const [pathMappings, setPathMappings] = useState<PathMapping[]>(program?.path_mappings || [])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()

    if (!name.trim()) {
      toast.error(t("externalPrograms.validation.nameRequired"))
      return
    }

    if (!path.trim()) {
      toast.error(t("externalPrograms.validation.pathRequired"))
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
        <Label htmlFor="name">{t("externalPrograms.nameLabel")}</Label>
        <Input
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("externalPrograms.namePlaceholder")}
          required
        />
      </div>

      <div className="space-y-2">
        <Label htmlFor="path">{t("externalPrograms.pathLabel")}</Label>
        <Input
          id="path"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder={t("externalPrograms.pathPlaceholder")}
          required
        />
        <p className="text-xs text-muted-foreground">
          {t("externalPrograms.pathDescription")}
        </p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="args">{t("externalPrograms.argumentsLabel")}</Label>
        <Textarea
          id="args"
          value={argsTemplate}
          onChange={(e) => setArgsTemplate(e.target.value)}
          placeholder={t("externalPrograms.argumentsPlaceholder")}
          rows={3}
        />
        <div className="text-xs text-muted-foreground space-y-1">
          <div>{t("externalPrograms.argumentsDescription")}</div>
          <div>{t("externalPrograms.availablePlaceholders")}</div>
          <ul className="list-disc list-inside pl-2 space-y-0.5">
            <li><code className="bg-muted px-1 rounded">{"{hash}"}</code> - {t("externalPrograms.placeholders.hash")}</li>
            <li><code className="bg-muted px-1 rounded">{"{name}"}</code> - {t("externalPrograms.placeholders.name")}</li>
            <li><code className="bg-muted px-1 rounded">{"{save_path}"}</code> - {t("externalPrograms.placeholders.savePath")}</li>
            <li><code className="bg-muted px-1 rounded">{"{content_path}"}</code> - {t("externalPrograms.placeholders.contentPath")}</li>
            <li><code className="bg-muted px-1 rounded">{"{category}"}</code> - {t("externalPrograms.placeholders.category")}</li>
            <li><code className="bg-muted px-1 rounded">{"{tags}"}</code> - {t("externalPrograms.placeholders.tags")}</li>
            <li><code className="bg-muted px-1 rounded">{"{state}"}</code> - {t("externalPrograms.placeholders.state")}</li>
            <li><code className="bg-muted px-1 rounded">{"{size}"}</code> - {t("externalPrograms.placeholders.size")}</li>
            <li><code className="bg-muted px-1 rounded">{"{progress}"}</code> - {t("externalPrograms.placeholders.progress")}</li>
            <li><code className="bg-muted px-1 rounded">{"{comment}"}</code> - {t("externalPrograms.placeholders.comment")}</li>
          </ul>
        </div>
      </div>

      <div className="space-y-2">
        <Label>{t("externalPrograms.pathMappings")}</Label>
        <div className="space-y-2">
          {pathMappings.map((mapping, index) => (
            <div key={index} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input
                  placeholder={t("externalPrograms.remotePathPlaceholder")}
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
                  placeholder={t("externalPrograms.localPathPlaceholder")}
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
                aria-label={t("externalPrograms.removePathMapping")}
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
            {t("externalPrograms.addPathMapping")}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("externalPrograms.pathMappingsDescription")}
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
            {t("externalPrograms.launchInTerminal")}
          </Label>
        </div>
        <p className="text-xs text-muted-foreground ml-9">
          {t("externalPrograms.launchInTerminalDescription")}
        </p>

        <div className="flex items-center space-x-2">
          <Switch
            id="enabled"
            checked={enabled}
            onCheckedChange={setEnabled}
          />
          <Label htmlFor="enabled" className="cursor-pointer">
            {t("externalPrograms.enableProgram")}
          </Label>
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel} disabled={isPending}>
          {t("externalPrograms.cancel")}
        </Button>
        <Button type="submit" disabled={isPending}>
          {isPending ? t("externalPrograms.saving") : program ? t("externalPrograms.updateButton") : t("externalPrograms.createSubmit")}
        </Button>
      </div>
    </form>
  )
}
