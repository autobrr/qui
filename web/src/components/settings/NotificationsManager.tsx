/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bell, ChevronDown, Plus, Send, Trash2 } from "lucide-react"
import { useId, useMemo, useRef, useState, type Ref } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import {
  groupNotificationEvents,
  maskNotificationUrl,
  normalizeNotificationUrl,
  type NotificationEventGroup
} from "@/components/settings/notification-targets"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { NotificationEventDefinition, NotificationTarget, NotificationTargetRequest } from "@/types"

const TARGETS_KEY = ["notificationTargets"]

const getErrorMessage = (error: unknown, fallback: string) =>
  error instanceof Error && error.message ? error.message : fallback

const TOGGLE_KEY = ["notificationTargets", "toggle"]
const TEST_KEY = ["notificationTargets", "test"]

export function NotificationsManager() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<NotificationTarget | null>(null)
  const [editorSession, setEditorSession] = useState(0)

  const eventsQuery = useQuery({
    queryKey: ["notificationEvents"],
    queryFn: () => api.listNotificationEvents(),
    staleTime: 5 * 60 * 1000,
  })
  const targetsQuery = useQuery({
    queryKey: TARGETS_KEY,
    queryFn: () => api.listNotificationTargets(),
    staleTime: 30 * 1000,
  })
  const definitions = eventsQuery.data
  const knownTypes = useMemo(() => new Set(definitions?.map((event) => event.type)), [definitions])

  const saveMutation = useMutation({
    mutationFn: ({ id, data }: { id: number | null; data: NotificationTargetRequest }) =>
      id === null ? api.createNotificationTarget(data) : api.updateNotificationTarget(id, data),
    onSuccess: (saved, { id }) => {
      if (id !== null) queryClient.setQueryData<NotificationTarget[]>(TARGETS_KEY, (old) => old?.map((item) => item.id === id ? saved : item))
      void queryClient.invalidateQueries({ queryKey: TARGETS_KEY })
      setEditorOpen(false)
      toast.success(t(id === null ? "notifications.toasts.created" : "notifications.toasts.updated"))
    },
  })

  const setEnabledInCache = (id: number, enabled: boolean) =>
    queryClient.setQueryData<NotificationTarget[]>(TARGETS_KEY, (old) => old?.map((item) => item.id === id ? { ...item, enabled } : item))

  const toggleMutation = useMutation({
    mutationKey: TOGGLE_KEY,
    mutationFn: ({ id, name, url, enabled }: NotificationTarget) => api.updateNotificationTarget(id, { name, url, enabled }),
    onMutate: async (target) => {
      await queryClient.cancelQueries({ queryKey: TARGETS_KEY })
      setEnabledInCache(target.id, target.enabled)
    },
    onError: (error, target) => {
      setEnabledInCache(target.id, !target.enabled)
      toast.error(getErrorMessage(error, t("notifications.toasts.updateFailed")))
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: TARGETS_KEY }),
  })
  const toggling = usePendingIds(TOGGLE_KEY)

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteNotificationTarget(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: TARGETS_KEY })
      toast.success(t("notifications.toasts.deleted"))
    },
    onError: (error) => toast.error(getErrorMessage(error, t("notifications.toasts.deleteFailed"))),
  })

  const testMutation = useMutation({
    mutationKey: TEST_KEY,
    mutationFn: (id: number) => api.testNotificationTarget(id),
    onSuccess: () => toast.success(t("notifications.toasts.testSent")),
    onError: (error) => toast.error(getErrorMessage(error, t("notifications.toasts.testFailed"))),
  })
  const testing = usePendingIds(TEST_KEY)

  const openEditor = (target: NotificationTarget | null) => {
    saveMutation.reset()
    setEditing(target)
    setEditorSession((session) => session + 1)
    setEditorOpen(true)
  }

  const eventSummary = ({ eventTypes }: NotificationTarget) => {
    if (!eventTypes?.length) return t("notifications.allEvents")
    const selected = eventTypes.filter((type) => knownTypes.has(type)).length
    return selected === knownTypes.size ? t("notifications.form.allEvents") : t("notifications.eventSummary", { selected, total: knownTypes.size })
  }

  return (
    <>
      <Section
        title={t("notifications.cardTitle")}
        description={t("notifications.cardDescription")}
        actions={
          <Button variant="outline" size="sm" disabled={!definitions} onClick={() => openEditor(null)}>
            <Plus />
            {t("notifications.addButton")}
          </Button>
        }
        queries={[targetsQuery, eventsQuery]}
      >
        <ResourceList
          label={t("notifications.cardTitle")}
          items={targetsQuery.data ?? []}
          itemKey={(target) => target.id}
          row={(target) => ({
            title: target.name,
            icon: Bell,
            meta: [maskNotificationUrl(target.url), eventSummary(target)],
            enabled: {
              checked: target.enabled,
              onCheckedChange: (enabled) => toggleMutation.mutate({ ...target, enabled }),
              disabled: toggling.has(target.id),
            },
            onOpen: () => openEditor(target),
            actions: [
              {
                label: t("notifications.actions.sendTest"),
                icon: Send,
                pending: testing.has(target.id),
                onSelect: () => testMutation.mutate(target.id),
              },
              {
                label: t("notifications.dialog.delete"),
                icon: Trash2,
                destructive: true,
                onSelect: () => deleteMutation.mutateAsync(target.id),
                confirm: {
                  title: t("notifications.dialog.deleteTitle"),
                  description: t("notifications.dialog.deleteDescription", { name: target.name }),
                  confirmLabel: t("notifications.dialog.delete"),
                },
              },
            ],
          })}
          empty={{
            title: t("notifications.emptyTitle"),
            description: t("notifications.emptyDescription"),
            onAdd: () => openEditor(null),
          }}
        />
      </Section>
      {definitions && (
        <TargetEditor
          key={editorSession}
          open={editorOpen}
          onOpenChange={setEditorOpen}
          target={editing}
          definitions={definitions}
          pending={saveMutation.isPending}
          submitDisabled={editing !== null && toggling.has(editing.id)}
          error={saveMutation.error ? getErrorMessage(saveMutation.error, t(editing ? "notifications.toasts.updateFailed" : "notifications.toasts.createFailed")) : undefined}
          onSave={(data) => saveMutation.mutate({ id: editing?.id ?? null, data })}
        />
      )}
    </>
  )
}

interface TargetEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: NotificationTarget | null
  definitions: readonly NotificationEventDefinition[]
  pending: boolean
  submitDisabled: boolean
  error?: string
  onSave: (data: NotificationTargetRequest) => void
}

interface FormErrors {
  name?: string
  url?: string
  events?: string
}

function TargetEditor({ open, onOpenChange, target, definitions, pending, submitDisabled, error, onSave }: TargetEditorProps) {
  const { t } = useTranslation("settings")
  const groups = useMemo(() => groupNotificationEvents(definitions), [definitions])
  const [name, setName] = useState(target?.name ?? "")
  const [url, setUrl] = useState(target?.url ?? "")
  const [enabled, setEnabled] = useState(target?.enabled ?? true)
  const [eventTypes, setEventTypes] = useState<ReadonlySet<string>>(() => {
    const known = definitions.map((event) => event.type)
    return new Set(target ? known.filter((type) => target.eventTypes?.includes(type)) : known)
  })
  const [errors, setErrors] = useState<FormErrors>({})
  const nameRef = useRef<HTMLInputElement>(null)
  const urlRef = useRef<HTMLInputElement>(null)
  const eventsRef = useRef<HTMLDivElement>(null)

  const submit = () => {
    const trimmedName = name.trim()
    const trimmedUrl = normalizeNotificationUrl(url).trim()
    const next: FormErrors = {
      name: trimmedName ? undefined : t("notifications.validation.nameRequired"),
      url: trimmedUrl ? undefined : t("notifications.validation.urlRequired"),
      events: eventTypes.size > 0 ? undefined : t("notifications.validation.selectEvent"),
    }
    setErrors(next)
    if (next.name) {
      nameRef.current?.focus()
    } else if (next.url) {
      urlRef.current?.focus()
    } else if (next.events) {
      eventsRef.current?.focus()
    } else {
      onSave({ name: trimmedName, url: trimmedUrl, enabled, eventTypes: [...eventTypes] })
    }
  }

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t(target ? "notifications.dialog.editTitle" : "notifications.dialog.newTitle")}
      description={t(target ? "notifications.dialog.editDescription" : "notifications.dialog.newDescription")}
      submitLabel={t(target ? "notifications.dialog.save" : "notifications.addButton")}
      onSubmit={submit}
      pending={pending}
      submitDisabled={submitDisabled}
      error={error}
    >
      <Field label={t("notifications.form.name")} error={errors.name}>
        <Input
          ref={nameRef}
          value={name}
          placeholder={t("notifications.form.namePlaceholder")}
          autoComplete="off"
          onChange={(event) => {
            setName(event.target.value)
            setErrors((prev) => ({ ...prev, name: undefined }))
          }}
        />
      </Field>
      <Field
        label={t("notifications.form.url")}
        help={
          <>
            {t("notifications.form.urlDescriptionPrefix")} <span className="font-mono">{t("notifications.form.examples.notifiarr")}</span>.{" "}
            {t("notifications.form.urlDescriptionSuffix")} <span className="font-mono">{t("notifications.form.examples.discord")}</span>.
          </>
        }
        error={errors.url}
      >
        <Input
          ref={urlRef}
          value={url}
          placeholder={t("notifications.form.urlPlaceholder")}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
          className="font-mono text-[13px]"
          onChange={(event) => {
            setUrl(normalizeNotificationUrl(event.target.value))
            setErrors((prev) => ({ ...prev, url: undefined }))
          }}
        />
      </Field>
      <SwitchRow label={t("notifications.form.enabled")} checked={enabled} onCheckedChange={setEnabled} />
      <EventPicker
        ref={eventsRef}
        groups={groups}
        selected={eventTypes}
        error={errors.events}
        onChange={(next) => {
          setEventTypes(next)
          setErrors((prev) => ({ ...prev, events: undefined }))
        }}
      />
    </Editor>
  )
}

interface EventPickerProps {
  ref: Ref<HTMLDivElement>
  groups: readonly NotificationEventGroup[]
  selected: ReadonlySet<string>
  error?: string
  onChange: (next: ReadonlySet<string>) => void
}

function EventPicker({ ref, groups, selected, error, onChange }: EventPickerProps) {
  const { t } = useTranslation("settings")
  const labelId = useId()
  const errorId = useId()
  const allTypes = groups.flatMap((group) => group.events.map((event) => event.type))

  const setTypes = (types: readonly string[], on: boolean) => {
    const next = new Set(selected)
    for (const type of types) {
      if (on) next.add(type)
      else next.delete(type)
    }
    onChange(next)
  }

  return (
    <div ref={ref} role="group" tabIndex={-1} aria-labelledby={labelId} aria-describedby={error ? errorId : undefined} className="scroll-mb-4 space-y-2 outline-none">
      <p id={labelId} className="text-sm font-medium leading-5">{t("notifications.form.events")}</p>
      <div className={cn("divide-y rounded-lg border", error && "border-destructive")}>
        <EventCheckRow
          label={t("notifications.form.allEvents")}
          count={selected.size}
          total={allTypes.length}
          onCheckedChange={(on) => setTypes(allTypes, on)}
        />
        {groups.map((group) => (
          <EventGroupRows key={group.id} group={group} selected={selected} onSet={setTypes} />
        ))}
      </div>
      {error && <p id={errorId} className="text-xs text-destructive">{error}</p>}
    </div>
  )
}

const CHECKBOX_TAP = "relative pointer-coarse:after:absolute pointer-coarse:after:-inset-3"

function checkState(count: number, total: number) {
  if (count === 0) return false
  return count === total ? true : "indeterminate"
}

function EventCheckRow({ label, count, total, onCheckedChange }: { label: string; count: number; total: number; onCheckedChange: (on: boolean) => void }) {
  const id = useId()
  return (
    <div className="flex min-h-11 items-center gap-3 px-3">
      <Checkbox id={id} checked={checkState(count, total)} onCheckedChange={() => onCheckedChange(count < total)} className={CHECKBOX_TAP} />
      <label htmlFor={id} className="flex-1 cursor-pointer text-sm font-medium">{label}</label>
      <span className="text-xs tabular-nums text-muted-foreground">{count}/{total}</span>
    </div>
  )
}

function EventGroupRows({ group, selected, onSet }: { group: NotificationEventGroup; selected: ReadonlySet<string>; onSet: (types: readonly string[], on: boolean) => void }) {
  const { t } = useTranslation("settings")
  const types = group.events.map((event) => event.type)
  const count = types.filter((type) => selected.has(type)).length
  const label = t(`notifications.groups.${group.id}`)

  return (
    <Collapsible>
      <div className="flex min-h-11 items-center gap-1 pr-1 pl-3">
        <Checkbox
          checked={checkState(count, types.length)}
          onCheckedChange={() => onSet(types, count < types.length)}
          aria-label={t("notifications.form.selectGroup", { group: label })}
          className={CHECKBOX_TAP}
        />
        <CollapsibleTrigger className="group flex min-h-10 flex-1 items-center gap-2 rounded-md px-2 text-left text-sm outline-none transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50">
          <span className="flex-1">{label}</span>
          <span className="text-xs tabular-nums text-muted-foreground">{count}/{types.length}</span>
          <ChevronDown aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" />
        </CollapsibleTrigger>
      </div>
      <CollapsibleContent>
        <ul className="space-y-0.5 pb-2">
          {group.events.map((event) => (
            <EventOption key={event.type} event={event} checked={selected.has(event.type)} onCheckedChange={(on) => onSet([event.type], on)} />
          ))}
        </ul>
      </CollapsibleContent>
    </Collapsible>
  )
}

function EventOption({ event, checked, onCheckedChange }: { event: NotificationEventDefinition; checked: boolean; onCheckedChange: (on: boolean) => void }) {
  const id = useId()
  const descriptionId = useId()
  return (
    <li className="flex items-start gap-3 py-1.5 pr-3 pl-9">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(next) => onCheckedChange(next === true)}
        aria-describedby={descriptionId}
        className={cn("mt-0.5", CHECKBOX_TAP)}
      />
      <label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">
        <span className="block text-sm">{event.label}</span>
        <span id={descriptionId} className="block text-xs text-muted-foreground">{event.description}</span>
      </label>
    </li>
  )
}
