/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AppWindow, ChevronDown, Plus, SquareTerminal, Trash2, X } from "lucide-react"
import { useId, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { APIError, api } from "@/lib/api"
import type { ExternalProgram, ExternalProgramPayload, PathMapping } from "@/types"

const PROGRAMS_KEY = ["externalPrograms"]
const TOGGLE_KEY = ["externalPrograms", "toggle"]

/** The automations that use a program, from the 409 answer to a delete. */
interface AutomationReference {
  id: number
  instanceId: number
  name: string
}

interface DeleteState {
  program: ExternalProgram
  /** The automations that use the program, once the server refused the first delete. */
  conflict: AutomationReference[] | null
  open: boolean
}

const PLACEHOLDERS = [
  ["{hash}", "hash"],
  ["{name}", "name"],
  ["{save_path}", "savePath"],
  ["{content_path}", "contentPath"],
  ["{category}", "category"],
  ["{tags}", "tags"],
  ["{state}", "state"],
  ["{size}", "size"],
  ["{progress}", "progress"],
  ["{comment}", "comment"],
] as const

function toPayload(program: ExternalProgram): ExternalProgramPayload {
  const { name, path, args_template, enabled, use_terminal, path_mappings } = program
  return { name, path, args_template, enabled, use_terminal, path_mappings: path_mappings ?? [] }
}

export function ExternalProgramsManager() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<ExternalProgram | null>(null)
  // A new key on each open gives the form fresh state.
  const [editorSession, setEditorSession] = useState(0)
  // The dialog keeps its program and its text while it fades out.
  const [deleting, setDeleting] = useState<DeleteState | null>(null)

  const errorText = (error: unknown) => error instanceof Error && error.message ? error.message : t("externalPrograms.unknownError")

  const programsQuery = useQuery({
    queryKey: PROGRAMS_KEY,
    queryFn: () => api.listExternalPrograms(),
    staleTime: 30 * 1000,
  })

  const saveMutation = useMutation({
    mutationFn: ({ id, data }: { id: number | null; data: ExternalProgramPayload }) =>
      id === null ? api.createExternalProgram(data) : api.updateExternalProgram(id, data),
    onSuccess: (saved, { id }) => {
      // The switch sends every field of its row, so the row holds the saved values before the switch is in reach again.
      if (id !== null) queryClient.setQueryData<ExternalProgram[]>(PROGRAMS_KEY, (old) => old?.map((item) => item.id === id ? saved : item))
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_KEY })
      setEditorOpen(false)
      toast.success(t(id === null ? "externalPrograms.toasts.created" : "externalPrograms.toasts.updated"))
    },
  })

  const setEnabledInCache = (id: number, enabled: boolean) =>
    queryClient.setQueryData<ExternalProgram[]>(PROGRAMS_KEY, (old) => old?.map((item) => item.id === id ? { ...item, enabled } : item))

  // The switch and the editor of a program take no save while its switch save runs, so two saves of one program never overlap.
  const toggleMutation = useMutation({
    mutationKey: TOGGLE_KEY,
    mutationFn: (program: ExternalProgram) => api.updateExternalProgram(program.id, toPayload(program)),
    onMutate: async (program) => {
      await queryClient.cancelQueries({ queryKey: PROGRAMS_KEY })
      setEnabledInCache(program.id, program.enabled)
    },
    onError: (error, program) => {
      setEnabledInCache(program.id, !program.enabled)
      toast.error(t("externalPrograms.toasts.updateFailed", { error: errorText(error) }))
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: PROGRAMS_KEY }),
  })
  const toggling = usePendingIds(TOGGLE_KEY)

  const deleteMutation = useMutation({
    mutationFn: ({ id, force }: { id: number; force: boolean }) => api.deleteExternalProgram(id, force),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_KEY })
      // A forced delete removes the program action from the automations that used it.
      void queryClient.invalidateQueries({ queryKey: ["automations"] })
      toast.success(t("externalPrograms.toasts.deleted"))
    },
    onError: (error) => {
      const automations = error instanceof APIError && error.status === 409
        ? (error.data as { automations?: AutomationReference[] } | undefined)?.automations
        : undefined
      if (automations) {
        // The dialog stays open and lists the automations, and the next confirm forces the delete.
        setDeleting((current) => current && { ...current, conflict: automations })
        return
      }
      toast.error(t("externalPrograms.toasts.deleteFailed", { error: errorText(error) }))
    },
  })

  const openEditor = (program: ExternalProgram | null) => {
    saveMutation.reset()
    setEditing(program)
    setEditorSession((session) => session + 1)
    setEditorOpen(true)
  }

  const command = (program: ExternalProgram) => [program.path, program.args_template].filter(Boolean).join(" ")

  return (
    <>
      <Section
        title={t("externalPrograms.cardTitle")}
        description={t("externalPrograms.cardDescription")}
        actions={
          <Button variant="outline" size="sm" onClick={() => openEditor(null)}>
            <Plus />
            {t("externalPrograms.createButton")}
          </Button>
        }
        queries={[programsQuery]}
      >
        <ResourceList
          label={t("externalPrograms.cardTitle")}
          items={programsQuery.data ?? []}
          itemKey={(program) => program.id}
          row={(program) => ({
            title: program.name,
            icon: program.use_terminal ? SquareTerminal : AppWindow,
            meta: [
              <span key="command" className="font-mono">{command(program)}</span>,
              ...(program.path_mappings?.length ? [t("externalPrograms.pathMappingsCount", { count: program.path_mappings.length })] : []),
            ],
            enabled: {
              checked: program.enabled,
              onCheckedChange: (enabled) => toggleMutation.mutate({ ...program, enabled }),
              disabled: toggling.has(program.id),
            },
            onOpen: () => openEditor(program),
            actions: [
              {
                label: t("externalPrograms.delete"),
                icon: Trash2,
                destructive: true,
                onSelect: () => setDeleting({ program, conflict: null, open: true }),
              },
            ],
          })}
          empty={{
            title: t("externalPrograms.emptyTitle"),
            description: t("externalPrograms.emptyDescription"),
            onAdd: () => openEditor(null),
          }}
        />
      </Section>
      {/* Outside the section, so a failed refetch of the list never takes an open form away. */}
      <ProgramEditor
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        program={editing}
        pending={saveMutation.isPending}
        submitDisabled={editing !== null && toggling.has(editing.id)}
        error={saveMutation.error
          ? t(editing ? "externalPrograms.toasts.updateFailed" : "externalPrograms.toasts.createFailed", { error: errorText(saveMutation.error) })
          : undefined}
        onSave={(data) => saveMutation.mutate({ id: editing?.id ?? null, data })}
      />
      {deleting && (
        <ConfirmDialog
          open={deleting.open}
          onOpenChange={(open) => !open && setDeleting((current) => current && { ...current, open: false })}
          title={t("externalPrograms.deleteTitle")}
          description={deleting.conflict
            ? t("externalPrograms.deleteConflictIntro")
            : t("externalPrograms.deleteDescription", { name: deleting.program.name })}
          confirmLabel={t(deleting.conflict ? "externalPrograms.deleteAnyway" : "externalPrograms.delete")}
          onConfirm={() => deleteMutation.mutateAsync({ id: deleting.program.id, force: deleting.conflict !== null })}
        >
          {deleting.conflict && (
            <div className="space-y-3 text-sm">
              <ul className="max-h-40 divide-y overflow-y-auto rounded-lg border">
                {deleting.conflict.map((automation) => (
                  <li key={automation.id} className="px-3 py-2 [overflow-wrap:anywhere]">{automation.name}</li>
                ))}
              </ul>
              <p className="text-muted-foreground">{t("externalPrograms.deleteConflictDescription")}</p>
            </div>
          )}
        </ConfirmDialog>
      )}
    </>
  )
}

interface ProgramEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The program to edit, or null for a new one. */
  program: ExternalProgram | null
  pending: boolean
  submitDisabled: boolean
  error?: string
  onSave: (data: ExternalProgramPayload) => void
}

interface FormErrors {
  name?: string
  path?: string
}

function ProgramEditor({ open, onOpenChange, program, pending, submitDisabled, error, onSave }: ProgramEditorProps) {
  const { t } = useTranslation("settings")
  const [name, setName] = useState(program?.name ?? "")
  const [path, setPath] = useState(program?.path ?? "")
  const [argsTemplate, setArgsTemplate] = useState(program?.args_template ?? "")
  const [enabled, setEnabled] = useState(program?.enabled ?? true)
  const [useTerminal, setUseTerminal] = useState(program?.use_terminal ?? true)
  const [pathMappings, setPathMappings] = useState<PathMapping[]>(program?.path_mappings ?? [])
  const [errors, setErrors] = useState<FormErrors>({})
  const nameRef = useRef<HTMLInputElement>(null)
  const pathRef = useRef<HTMLInputElement>(null)

  const submit = () => {
    const next: FormErrors = {
      name: name.trim() ? undefined : t("externalPrograms.validation.nameRequired"),
      path: path.trim() ? undefined : t("externalPrograms.validation.pathRequired"),
    }
    setErrors(next)
    if (next.name) {
      nameRef.current?.focus()
    } else if (next.path) {
      pathRef.current?.focus()
    } else {
      onSave({
        name: name.trim(),
        path: path.trim(),
        args_template: argsTemplate.trim(),
        enabled,
        use_terminal: useTerminal,
        path_mappings: pathMappings.filter((mapping) => mapping.from.trim() !== "" && mapping.to.trim() !== ""),
      })
    }
  }

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t(program ? "externalPrograms.editTitle" : "externalPrograms.createTitle")}
      description={program ? undefined : t("externalPrograms.createDescription")}
      submitLabel={t(program ? "externalPrograms.save" : "externalPrograms.createButton")}
      onSubmit={submit}
      pending={pending}
      submitDisabled={submitDisabled}
      error={error}
    >
      <Field label={t("externalPrograms.nameLabel")} error={errors.name}>
        <Input
          ref={nameRef}
          value={name}
          placeholder={t("externalPrograms.namePlaceholder")}
          autoComplete="off"
          onChange={(event) => {
            setName(event.target.value)
            setErrors((prev) => ({ ...prev, name: undefined }))
          }}
        />
      </Field>
      <Field label={t("externalPrograms.pathLabel")} error={errors.path}>
        <Input
          ref={pathRef}
          value={path}
          placeholder={t("externalPrograms.pathPlaceholder")}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          className="font-mono md:text-[13px]"
          onChange={(event) => {
            setPath(event.target.value)
            setErrors((prev) => ({ ...prev, path: undefined }))
          }}
        />
      </Field>
      <div className="space-y-2">
        <Field label={t("externalPrograms.argumentsLabel")}>
          <Textarea
            value={argsTemplate}
            placeholder={t("externalPrograms.argumentsPlaceholder")}
            rows={3}
            autoCapitalize="off"
            spellCheck={false}
            className="font-mono md:text-[13px]"
            onChange={(event) => setArgsTemplate(event.target.value)}
          />
        </Field>
        <PlaceholderList />
      </div>
      <PathMappingsField mappings={pathMappings} onChange={setPathMappings} />
      <SwitchRow
        label={t("externalPrograms.launchInTerminal")}
        help={t("externalPrograms.launchInTerminalDescription")}
        checked={useTerminal}
        onCheckedChange={setUseTerminal}
      />
      <SwitchRow label={t("externalPrograms.enableProgram")} checked={enabled} onCheckedChange={setEnabled} />
    </Editor>
  )
}

/** The placeholders that the arguments can hold, folded away until the user opens them. */
function PlaceholderList() {
  const { t } = useTranslation("settings")
  return (
    <Collapsible>
      <CollapsibleTrigger className="group -mx-1 inline-flex min-h-8 items-center gap-1 rounded-md px-1 text-xs text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-sm:min-h-10">
        {t("externalPrograms.availablePlaceholders")}
        <ChevronDown aria-hidden="true" className="size-3.5 transition-transform group-data-[state=open]:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <dl className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 rounded-lg border px-3 py-2.5 text-xs sm:grid-cols-[auto_minmax(0,1fr)_auto_minmax(0,1fr)]">
          {PLACEHOLDERS.map(([token, key]) => (
            <div key={token} className="contents">
              <dt className="font-mono text-foreground">{token}</dt>
              <dd className="text-muted-foreground">{t(`externalPrograms.placeholders.${key}`)}</dd>
            </div>
          ))}
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}

/** Pairs of a remote path and a local path. A phone puts the two paths of a pair one above the other, each with its label. */
function PathMappingsField({ mappings, onChange }: { mappings: PathMapping[]; onChange: (mappings: PathMapping[]) => void }) {
  const { t } = useTranslation("settings")
  const labelId = useId()

  const update = (index: number, change: Partial<PathMapping>) =>
    onChange(mappings.map((mapping, i) => i === index ? { ...mapping, ...change } : mapping))

  const pathInput = (index: number, side: "from" | "to") => (
    <label className="block min-w-0 space-y-1">
      <span className="block text-xs text-muted-foreground sm:sr-only">
        {t(side === "from" ? "externalPrograms.remotePath" : "externalPrograms.localPath")}
      </span>
      <Input
        value={mappings[index][side]}
        placeholder={t(side === "from" ? "externalPrograms.remotePathPlaceholder" : "externalPrograms.localPathPlaceholder")}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        className="font-mono md:text-[13px]"
        onChange={(event) => update(index, { [side]: event.target.value })}
      />
    </label>
  )

  return (
    <div role="group" aria-labelledby={labelId} className="space-y-2">
      <div className="flex min-h-5 items-center gap-1.5">
        <span id={labelId} className="text-sm font-medium leading-5">{t("externalPrograms.pathMappings")}</span>
        <FieldHelp>{t("externalPrograms.pathMappingsDescription")}</FieldHelp>
      </div>
      {mappings.length > 0 && (
        <>
          <div aria-hidden="true" className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2.25rem] gap-2 text-xs text-muted-foreground max-sm:hidden">
            <span>{t("externalPrograms.remotePath")}</span>
            <span>{t("externalPrograms.localPath")}</span>
          </div>
          <ul className="space-y-2 max-sm:space-y-3">
            {mappings.map((_, index) => (
              <li key={index} className="flex items-start gap-2 max-sm:rounded-lg max-sm:border max-sm:p-3">
                <div className="grid min-w-0 flex-1 gap-2 sm:grid-cols-2">
                  {pathInput(index, "from")}
                  {pathInput(index, "to")}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t("externalPrograms.removePathMapping")}
                  className="size-9 shrink-0 text-muted-foreground hover:text-destructive max-sm:-mt-1 max-sm:-mr-1 max-sm:size-10"
                  onClick={() => onChange(mappings.filter((_mapping, i) => i !== index))}
                >
                  <X />
                </Button>
              </li>
            ))}
          </ul>
        </>
      )}
      <Button type="button" variant="outline" size="sm" className="max-sm:h-10 max-sm:w-full" onClick={() => onChange([...mappings, { from: "", to: "" }])}>
        <Plus />
        {t("externalPrograms.addPathMapping")}
      </Button>
    </div>
  )
}
