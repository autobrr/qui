/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Eye, EyeOff, KeyRound, Plus, Server, Trash2 } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList, type ResourceAction } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { getBaseUrl } from "@/lib/base-url"
import { useIncognitoMode } from "@/lib/incognito"
import { cn } from "@/lib/utils"
import type { InstanceResponse } from "@/types"

const KEYS_KEY = ["clientApiKeys"]

type NewClientApiKey = Awaited<ReturnType<typeof api.createClientApiKey>>

const errorMessage = (error: unknown, fallback: string) =>
  error instanceof Error && error.message ? error.message : fallback

function resolveProxyPath(proxyPath: string) {
  const normalizedProxyPath = proxyPath.startsWith("/") ? proxyPath : `/${proxyPath}`
  const base = getBaseUrl()
  const baseWithoutTrailingSlash = base.endsWith("/") ? base.slice(0, -1) : base

  if (!baseWithoutTrailingSlash) {
    return normalizedProxyPath
  }

  if (
    normalizedProxyPath === baseWithoutTrailingSlash ||
    normalizedProxyPath.startsWith(`${baseWithoutTrailingSlash}/`)
  ) {
    return normalizedProxyPath
  }

  return `${baseWithoutTrailingSlash}${normalizedProxyPath}`
}

function fullProxyUrl(proxyPath: string) {
  const { protocol, hostname, port } = window.location
  const origin = port ? `${protocol}//${hostname}:${port}` : `${protocol}//${hostname}`
  return `${origin}${resolveProxyPath(proxyPath)}`
}

export function ClientApiKeysManager() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editorSession, setEditorSession] = useState(0)
  const [newKey, setNewKey] = useState<NewClientApiKey | null>(null)

  const keysQuery = useQuery({
    queryKey: KEYS_KEY,
    queryFn: () => api.getClientApiKeys(),
    staleTime: 30 * 1000,
    retry: (failureCount, error) => {
      if (error && error.message?.includes("404")) {
        return false
      }
      return failureCount < 3
    },
  })

  const instancesQuery = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
    staleTime: 60 * 1000,
  })

  const createMutation = useMutation({
    mutationFn: (data: { clientName: string; instanceId: number }) => api.createClientApiKey(data),
    onSuccess: (data) => {
      setNewKey(data)
      setEditorOpen(false)
      void queryClient.invalidateQueries({ queryKey: KEYS_KEY })
      toast.success(t("clientApiKeys.toasts.created"))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteClientApiKey(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: KEYS_KEY })
      toast.success(t("clientApiKeys.toasts.deleted"))
    },
    onError: (error) => {
      toast.error(t("clientApiKeys.toasts.deleteFailed", { error: errorMessage(error, t("clientApiKeys.unknownError")) }))
    },
  })

  const canCreate = keysQuery.isSuccess && !newKey
  const openEditor = () => {
    createMutation.reset()
    setEditorSession((session) => session + 1)
    setEditorOpen(true)
  }

  const hostAction: ResourceAction = {
    label: t(incognitoMode ? "clientApiKeys.labels.showHost" : "clientApiKeys.labels.hideHost"),
    icon: incognitoMode ? Eye : EyeOff,
    onSelect: () => setIncognitoMode(!incognitoMode),
  }

  return (
    <>
      <Section
        title={t("clientApiKeys.cardTitle")}
        description={t("clientApiKeys.cardDescription")}
        actions={
          <Button variant="outline" size="sm" disabled={!canCreate} onClick={openEditor}>
            <Plus />
            {t("clientApiKeys.createButton")}
          </Button>
        }
        queries={[keysQuery]}
      >
        {newKey && <NewKeyNotice newKey={newKey} onDone={() => setNewKey(null)} />}
        <ResourceList
          label={t("clientApiKeys.cardTitle")}
          items={keysQuery.data ?? []}
          itemKey={(key) => key.id}
          row={(key) => ({
            title: key.clientName,
            icon: KeyRound,
            status: key.instance ? undefined : { tone: "error", label: t("clientApiKeys.instanceDeleted") },
            meta: [
              ...(key.instance
                ? [
                  key.instance.name,
                  <span key="host" className={cn(incognitoMode && "blur-sm select-none")}>{key.instance.host}</span>,
                ]
                : []),
              key.lastUsedAt
                ? t("clientApiKeys.lastUsed", { date: formatDate(new Date(key.lastUsedAt)) })
                : t("clientApiKeys.neverUsed"),
              t("clientApiKeys.created", { date: formatDate(new Date(key.createdAt)) }),
            ],
            actions: [
              ...(key.instance?.host ? [hostAction] : []),
              {
                label: t("clientApiKeys.deleteDialog.delete"),
                icon: Trash2,
                destructive: true,
                onSelect: () => deleteMutation.mutateAsync(key.id),
                confirm: {
                  title: t("clientApiKeys.deleteDialog.title"),
                  description: t("clientApiKeys.deleteDialog.description", { name: key.clientName }),
                  confirmLabel: t("clientApiKeys.deleteDialog.delete"),
                },
              },
            ],
          })}
          empty={{
            title: t("clientApiKeys.emptyTitle"),
            description: t("clientApiKeys.emptyDescription"),
            onAdd: canCreate ? openEditor : undefined,
          }}
        />
      </Section>
      <ClientKeyEditor
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        instances={instancesQuery.data}
        pending={createMutation.isPending}
        error={createMutation.error ? errorMessage(createMutation.error, t("clientApiKeys.toasts.createFailed")) : undefined}
        onSave={(data) => createMutation.mutate(data)}
      />
    </>
  )
}

function NewKeyNotice({ newKey, onDone }: { newKey: NewClientApiKey; onDone: () => void }) {
  const { t } = useTranslation("settings")
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const url = fullProxyUrl(newKey.proxyUrl)
  const toggleLabel = t(incognitoMode ? "clientApiKeys.newKey.showProxyUrl" : "clientApiKeys.newKey.hideProxyUrl")

  return (
    <div role="status" className="space-y-3 rounded-lg border border-primary/30 bg-muted/40 p-4 max-sm:p-3">
      <div className="space-y-0.5">
        <p className="text-sm font-medium [overflow-wrap:anywhere]">{t("clientApiKeys.newKey.title", { name: newKey.clientApiKey.clientName })}</p>
        <p className="text-xs text-muted-foreground">{t("clientApiKeys.newKey.warning")}</p>
      </div>
      <div className="flex items-center gap-1 rounded-md border bg-background py-1 pr-1 pl-3">
        <code
          aria-label={t("clientApiKeys.newKey.proxyUrl")}
          className={cn("min-w-0 flex-1 py-1 font-mono text-13 [overflow-wrap:anywhere]", incognitoMode && "blur-sm select-none")}
        >
          {url}
        </code>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label={toggleLabel}
              className="size-8 shrink-0 text-muted-foreground hover:text-foreground max-sm:size-10"
              onClick={() => setIncognitoMode(!incognitoMode)}
            >
              {incognitoMode ? <Eye /> : <EyeOff />}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{toggleLabel}</TooltipContent>
        </Tooltip>
        <CopyButton value={url} label={t("clientApiKeys.newKey.proxyUrl")} className="size-8 max-sm:size-10" iconClassName="size-4" />
      </div>
      <div className="flex justify-end">
        <Button variant="outline" size="sm" className="max-sm:h-10 max-sm:w-full" onClick={onDone}>
          {t("clientApiKeys.newKey.done")}
        </Button>
      </div>
    </div>
  )
}

interface ClientKeyEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instances?: readonly InstanceResponse[]
  pending: boolean
  error?: string
  onSave: (data: { clientName: string; instanceId: number }) => void
}

function ClientKeyEditor({ open, onOpenChange, instances, pending, error, onSave }: ClientKeyEditorProps) {
  const { t } = useTranslation("settings")
  const [incognitoMode] = useIncognitoMode()
  const [clientName, setClientName] = useState("")
  const [instanceId, setInstanceId] = useState("")
  const [errors, setErrors] = useState<{ clientName?: string; instanceId?: string }>({})
  const nameRef = useRef<HTMLInputElement>(null)
  const instanceRef = useRef<HTMLButtonElement>(null)

  const submit = () => {
    const name = clientName.trim()
    const id = parseInt(instanceId, 10)
    const next = {
      clientName: name ? undefined : t("clientApiKeys.validation.clientNameRequired"),
      instanceId: id ? undefined : t("clientApiKeys.validation.selectInstance"),
    }
    setErrors(next)
    if (next.clientName) {
      nameRef.current?.focus()
    } else if (next.instanceId) {
      instanceRef.current?.focus()
    } else {
      onSave({ clientName: name, instanceId: id })
    }
  }

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t("clientApiKeys.createDialog.title")}
      description={t("clientApiKeys.createDialog.description")}
      submitLabel={t("clientApiKeys.createButton")}
      onSubmit={submit}
      pending={pending}
      error={error}
    >
      <Field label={t("clientApiKeys.createDialog.clientNameLabel")} error={errors.clientName}>
        <Input
          ref={nameRef}
          value={clientName}
          placeholder={t("clientApiKeys.createDialog.clientNamePlaceholder")}
          autoComplete="off"
          onChange={(event) => {
            setClientName(event.target.value)
            setErrors((prev) => ({ ...prev, clientName: undefined }))
          }}
        />
      </Field>
      <Field label={t("clientApiKeys.createDialog.instanceLabel")} error={errors.instanceId}>
        {(control) => (
          <Select
            value={instanceId}
            onValueChange={(value) => {
              setInstanceId(value)
              setErrors((prev) => ({ ...prev, instanceId: undefined }))
            }}
          >
            <SelectTrigger ref={instanceRef} {...control} className="w-full">
              <SelectValue placeholder={t("clientApiKeys.createDialog.instancePlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {instances?.map((instance) => (
                <SelectItem key={instance.id} value={instance.id.toString()}>
                  <Server className="size-4" />
                  <span className="truncate">{instance.name}</span>
                  <span className={cn("truncate text-xs text-muted-foreground", incognitoMode && "blur-sm select-none")}>{instance.host}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </Field>
    </Editor>
  )
}
