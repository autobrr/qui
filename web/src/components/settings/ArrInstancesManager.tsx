/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Clapperboard, Loader2, Plus, Trash2, Tv, Zap } from "lucide-react"
import { useRef, useState, type ChangeEvent } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList, type ResourceRow } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { ArrInstance, ArrInstanceFormData, ArrInstanceType, ArrInstanceUpdateData, ArrTestResponse } from "@/types/arr"

const INSTANCES_KEY = ["arrInstances"]
const TOGGLE_KEY = ["arrInstances", "toggle"]
const TEST_KEY = ["arrInstances", "test"]

// The server keeps the saved basic auth password when the update sends this value back.
const REDACTED = "<redacted>"

const TYPES: readonly ArrInstanceType[] = ["sonarr", "radarr"]

const errorMessage = (error: unknown, fallback: string) =>
  error instanceof Error && error.message ? error.message : fallback

type SaveRequest =
  | { id: null; data: ArrInstanceFormData }
  | { id: number; data: ArrInstanceUpdateData }

/** The Integrations tab: the Sonarr and Radarr instances that cross-seed asks for IDs. */
export function ArrInstancesManager() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<ArrInstance | null>(null)
  // A new key on each open gives the form fresh state.
  const [editorSession, setEditorSession] = useState(0)

  const instancesQuery = useQuery({
    queryKey: INSTANCES_KEY,
    queryFn: () => api.listArrInstances(),
    staleTime: 30 * 1000,
  })
  // In the order of TYPES, so that the headings keep their order.
  const instances = TYPES.flatMap((type) => instancesQuery.data?.filter((instance) => instance.type === type) ?? [])
  const typeName = (type: ArrInstanceType) => t(type === "sonarr" ? "arrInstances.form.types.sonarrShort" : "arrInstances.form.types.radarrShort")

  const unknownError = t("arrInstances.unknownError")

  const saveMutation = useMutation({
    mutationFn: (request: SaveRequest) =>
      request.id === null ? api.createArrInstance(request.data) : api.updateArrInstance(request.id, request.data),
    onSuccess: (_instance, { id }) => {
      void queryClient.invalidateQueries({ queryKey: INSTANCES_KEY })
      setEditorOpen(false)
      toast.success(t(id === null ? "arrInstances.toasts.created" : "arrInstances.toasts.updated"))
    },
  })

  const setEnabledInCache = (id: number, enabled: boolean) =>
    queryClient.setQueryData<ArrInstance[]>(INSTANCES_KEY, (old) => old?.map((item) => item.id === id ? { ...item, enabled } : item))

  // The switch of an instance takes no input while its save runs, so two saves of one instance never overlap.
  const toggleMutation = useMutation({
    mutationKey: TOGGLE_KEY,
    // The server changes only the fields that the request holds.
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) => api.updateArrInstance(id, { enabled }),
    onMutate: async ({ id, enabled }) => {
      await queryClient.cancelQueries({ queryKey: INSTANCES_KEY })
      setEnabledInCache(id, enabled)
    },
    onError: (error, { id, enabled }) => {
      setEnabledInCache(id, !enabled)
      toast.error(t("arrInstances.toasts.updateFailed", { error: errorMessage(error, unknownError) }))
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: INSTANCES_KEY }),
  })
  const toggling = usePendingIds(TOGGLE_KEY)

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteArrInstance(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: INSTANCES_KEY })
      toast.success(t("arrInstances.toasts.deleted"))
    },
    onError: (error) => toast.error(t("arrInstances.toasts.deleteFailed", { error: errorMessage(error, unknownError) })),
  })

  const testMutation = useMutation({
    mutationKey: TEST_KEY,
    mutationFn: (id: number) => api.testArrInstance(id),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: INSTANCES_KEY })
      if (result.success) {
        toast.success(t("arrInstances.toasts.connectionSuccess"))
      } else {
        toast.error(t("arrInstances.toasts.connectionFailed", { error: result.error || unknownError }))
      }
    },
    onError: (error) => toast.error(t("arrInstances.toasts.testFailed", { error: errorMessage(error, unknownError) })),
  })
  const testing = usePendingIds(TEST_KEY)

  const openEditor = (instance: ArrInstance | null) => {
    saveMutation.reset()
    setEditing(instance)
    setEditorSession((session) => session + 1)
    setEditorOpen(true)
  }

  const saveError = saveMutation.error
    ? t(editing ? "arrInstances.toasts.updateFailed" : "arrInstances.toasts.createFailed", { error: errorMessage(saveMutation.error, unknownError) })
    : undefined

  const row = (instance: ArrInstance): ResourceRow => ({
    title: instance.name,
    icon: instance.type === "sonarr" ? Tv : Clapperboard,
    status: instance.last_test_status === "ok"
      ? { tone: "ok", label: t("arrInstances.status.connected") }
      : instance.last_test_status === "error" ? { tone: "error", label: t("arrInstances.status.failed") } : undefined,
    meta: [
      instance.base_url,
      ...(instance.last_test_at ? [t("arrInstances.tested", { date: formatDate(new Date(instance.last_test_at)) })] : []),
    ],
    enabled: {
      checked: instance.enabled,
      onCheckedChange: (enabled) => toggleMutation.mutate({ id: instance.id, enabled }),
      disabled: toggling.has(instance.id),
    },
    onOpen: () => openEditor(instance),
    actions: [
      {
        label: t("arrInstances.actions.testConnection"),
        icon: Zap,
        pending: testing.has(instance.id),
        onSelect: () => testMutation.mutate(instance.id),
      },
      {
        label: t("arrInstances.actions.delete"),
        icon: Trash2,
        destructive: true,
        onSelect: () => deleteMutation.mutateAsync(instance.id),
        confirm: {
          title: t("arrInstances.deleteDialog.title"),
          description: t("arrInstances.deleteDialog.description", { name: instance.name }),
          confirmLabel: t("arrInstances.actions.delete"),
        },
      },
    ],
    detail: instance.last_test_status === "error" && instance.last_test_error
      ? <p className="text-xs text-destructive [overflow-wrap:anywhere]">{instance.last_test_error}</p>
      : undefined,
  })

  return (
    <>
      <Section
        title={t("integrations.title")}
        description={t("integrations.description")}
        actions={
          <Button variant="outline" size="sm" onClick={() => openEditor(null)}>
            <Plus />
            {t("arrInstances.addButton")}
          </Button>
        }
        queries={[instancesQuery]}
      >
        <ResourceList
          label={t("integrations.title")}
          items={instances}
          itemKey={(instance) => instance.id}
          row={row}
          group={(instance) => typeName(instance.type)}
          empty={{
            title: t("arrInstances.emptyTitle"),
            description: t("arrInstances.emptyDescription"),
            onAdd: () => openEditor(null),
          }}
        />
      </Section>
      {/* Outside the section, so a failed refetch of the list never takes an open form away. */}
      <ArrInstanceEditor
        key={editorSession}
        open={editorOpen}
        onOpenChange={setEditorOpen}
        instance={editing}
        pending={saveMutation.isPending}
        error={saveError}
        onSave={(request) => saveMutation.mutate(request)}
      />
    </>
  )
}

interface ArrInstanceEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The instance to edit, or null for a new one. */
  instance: ArrInstance | null
  pending: boolean
  error?: string
  onSave: (request: SaveRequest) => void
}

// A new test clears the last result, so the result mounts again, and comes into view, after each test.
const scrollIntoView = (element: HTMLElement | null) => element?.scrollIntoView({ block: "nearest" })

type FieldName = "name" | "baseUrl" | "apiKey" | "basicUsername" | "basicPassword"
type FormErrors = Partial<Record<FieldName, string>>
// The order of the fields in the form, for the focus on the first error.
const FIELD_ORDER: readonly FieldName[] = ["name", "baseUrl", "apiKey", "basicUsername", "basicPassword"]

function ArrInstanceEditor({ open, onOpenChange, instance, pending, error, onSave }: ArrInstanceEditorProps) {
  const { t } = useTranslation("settings")
  const isEdit = instance !== null
  const [type, setType] = useState<ArrInstanceType>(instance?.type ?? "sonarr")
  const [name, setName] = useState(instance?.name ?? "")
  const [baseUrl, setBaseUrl] = useState(instance?.base_url ?? "")
  const [apiKey, setApiKey] = useState("")
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basic_username)
  const [basicUsername, setBasicUsername] = useState(instance?.basic_username ?? "")
  const [basicPassword, setBasicPassword] = useState(instance?.basic_username ? REDACTED : "")
  const [enabled, setEnabled] = useState(instance?.enabled !== false)
  const [priority, setPriority] = useState(instance?.priority ?? 0)
  const [timeoutSeconds, setTimeoutSeconds] = useState(instance?.timeout_seconds ?? 15)
  const [errors, setErrors] = useState<FormErrors>({})
  const inputs = useRef<Partial<Record<FieldName, HTMLInputElement | null>>>({})
  const unknownError = t("arrInstances.unknownError")
  const typeName = t(type === "sonarr" ? "arrInstances.form.types.sonarrShort" : "arrInstances.form.types.radarrShort")

  const testMutation = useMutation({
    mutationFn: () => api.testArrConnection({
      type,
      base_url: baseUrl.trim(),
      api_key: apiKey.trim(),
      basic_username: showBasicAuth ? basicUsername.trim() : undefined,
      basic_password: showBasicAuth ? basicPassword : undefined,
    }),
    onSuccess: (result) => {
      if (result.success) {
        toast.success(t("arrInstances.toasts.connectionSuccess"))
      } else {
        toast.error(t("arrInstances.toasts.connectionFailed", { error: result.error || unknownError }))
      }
    },
    onError: (testError) => toast.error(t("arrInstances.toasts.testFailed", { error: errorMessage(testError, unknownError) })),
  })
  const testResult: ArrTestResponse | null = testMutation.error
    ? { success: false, error: errorMessage(testMutation.error, unknownError) }
    : testMutation.data ?? null

  /** Shows the errors, and focuses the first field that has one. Returns true when the form has none. */
  const check = (next: FormErrors) => {
    setErrors(next)
    const first = FIELD_ORDER.find((field) => next[field])
    if (first) inputs.current[first]?.focus()
    return first === undefined
  }

  const basicAuthErrors = (forTest: boolean): FormErrors => {
    if (!showBasicAuth) return {}
    let password: string | undefined
    if (forTest) {
      password = !basicPassword || basicPassword === REDACTED ? t("arrInstances.validation.basicPasswordRequired") : undefined
    } else if (!basicPassword) {
      password = t(isEdit ? "arrInstances.validation.basicPasswordKeepOrEnter" : "arrInstances.validation.basicPasswordRequiredSubmit")
    }
    return {
      basicUsername: basicUsername.trim() ? undefined : t("arrInstances.validation.basicUsernameRequired"),
      basicPassword: password,
    }
  }

  const test = () => {
    const required = t("arrInstances.validation.baseUrlAndApiKeyRequired")
    const valid = check({
      baseUrl: baseUrl.trim() ? undefined : required,
      apiKey: apiKey.trim() ? undefined : required,
      ...basicAuthErrors(true),
    })
    if (valid) testMutation.mutate()
  }

  const submit = () => {
    const valid = check({
      name: name.trim() ? undefined : t("arrInstances.validation.nameRequired"),
      baseUrl: baseUrl.trim() ? undefined : t("arrInstances.validation.baseUrlRequired"),
      apiKey: isEdit || apiKey.trim() ? undefined : t("arrInstances.validation.apiKeyRequired"),
      ...basicAuthErrors(false),
    })
    if (!valid) return

    const basicUser = basicUsername.trim()
    if (isEdit) {
      const data: ArrInstanceUpdateData = {
        name: name.trim(),
        base_url: baseUrl.trim(),
        enabled,
        priority,
        timeout_seconds: timeoutSeconds,
      }
      if (apiKey.trim()) data.api_key = apiKey.trim()
      if (showBasicAuth) {
        data.basic_username = basicUser
        if (basicPassword !== REDACTED) data.basic_password = basicPassword
      } else {
        data.basic_username = ""
        data.basic_password = ""
      }
      onSave({ id: instance.id, data })
    } else {
      const data: ArrInstanceFormData = {
        type,
        name: name.trim(),
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        enabled,
        priority,
        timeout_seconds: timeoutSeconds,
      }
      if (showBasicAuth) {
        data.basic_username = basicUser
        data.basic_password = basicPassword
      }
      onSave({ id: null, data })
    }
  }

  /** The props of a text input of the form: its value, its ref for the focus on an error, and the reset of its error. */
  const input = (field: FieldName, value: string, setValue: (value: string) => void) => ({
    ref: (element: HTMLInputElement | null) => {
      inputs.current[field] = element
    },
    value,
    onChange: (event: ChangeEvent<HTMLInputElement>) => {
      setValue(event.target.value)
      setErrors((prev) => ({ ...prev, [field]: undefined }))
    },
  })

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t(isEdit ? "arrInstances.editDialog.title" : "arrInstances.addDialog.title")}
      description={isEdit ? undefined : t("arrInstances.addDialog.description")}
      submitLabel={t(isEdit ? "arrInstances.actions.save" : "arrInstances.addButton")}
      onSubmit={submit}
      pending={pending}
      error={error}
      secondaryActions={
        <Button type="button" variant="outline" disabled={testMutation.isPending} onClick={test}>
          {testMutation.isPending ? <Loader2 className="animate-spin" /> : <Zap />}
          {t("arrInstances.actions.testConnection")}
        </Button>
      }
    >
      {!isEdit && (
        <Field label={t("arrInstances.form.type")}>
          {(control) => (
            <Select value={type} onValueChange={(value) => setType(value as ArrInstanceType)}>
              <SelectTrigger {...control} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="sonarr">{t("arrInstances.form.types.sonarr")}</SelectItem>
                <SelectItem value="radarr">{t("arrInstances.form.types.radarr")}</SelectItem>
              </SelectContent>
            </Select>
          )}
        </Field>
      )}
      <Field label={t("arrInstances.form.name")} error={errors.name}>
        <Input
          {...input("name", name, setName)}
          placeholder={t("arrInstances.form.namePlaceholder", { type: typeName })}
          autoComplete="off"
        />
      </Field>
      <Field label={t("arrInstances.form.baseUrl")} error={errors.baseUrl}>
        <Input
          {...input("baseUrl", baseUrl, setBaseUrl)}
          inputMode="url"
          placeholder={`http://localhost:${type === "sonarr" ? "8989" : "7878"}`}
          autoComplete="off"
          autoCapitalize="off"
          spellCheck={false}
        />
      </Field>
      <Field
        label={t("arrInstances.form.apiKey")}
        help={t("arrInstances.form.apiKeyDescription", { type: typeName })}
        description={isEdit ? t("arrInstances.form.apiKeyKeep") : undefined}
        error={errors.apiKey}
      >
        <Input
          {...input("apiKey", apiKey, setApiKey)}
          type="password"
          placeholder={isEdit ? t("arrInstances.form.apiKeyPlaceholderEdit") : t("arrInstances.form.apiKeyPlaceholder")}
          autoComplete="off"
        />
      </Field>
      <SwitchRow
        label={t("arrInstances.form.basicAuth")}
        help={t("arrInstances.form.basicAuthDescription")}
        checked={showBasicAuth}
        onCheckedChange={(checked) => {
          setShowBasicAuth(checked)
          if (!checked) {
            setBasicUsername("")
            setBasicPassword("")
          } else if (!basicUsername.trim()) {
            setBasicPassword("")
          }
          setErrors((prev) => ({ ...prev, basicUsername: undefined, basicPassword: undefined }))
        }}
      />
      {showBasicAuth && (
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label={t("arrInstances.form.basicUsername")} error={errors.basicUsername}>
            <Input {...input("basicUsername", basicUsername, setBasicUsername)} autoComplete="off" />
          </Field>
          <Field
            label={t("arrInstances.form.basicPassword")}
            description={instance?.basic_username ? t("arrInstances.form.basicPasswordKeep") : undefined}
            error={errors.basicPassword}
          >
            <Input {...input("basicPassword", basicPassword, setBasicPassword)} type="password" autoComplete="off" />
          </Field>
        </div>
      )}
      <div className="grid grid-cols-2 gap-4">
        <Field label={t("arrInstances.form.priority")} help={t("arrInstances.form.priorityDescription")}>
          <Input type="number" inputMode="numeric" value={priority} min={0} onChange={(event) => setPriority(parseInt(event.target.value) || 0)} />
        </Field>
        <Field label={t("arrInstances.form.timeoutSeconds")}>
          <Input type="number" inputMode="numeric" value={timeoutSeconds} min={1} max={120} onChange={(event) => setTimeoutSeconds(parseInt(event.target.value) || 15)} />
        </Field>
      </div>
      <SwitchRow label={t("arrInstances.form.enabled")} checked={enabled} onCheckedChange={setEnabled} />
      {testResult && (
        <div
          ref={scrollIntoView}
          role="status"
          className={cn(
            "flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm",
            testResult.success ? "border-emerald-500/40 bg-emerald-500/5" : "border-destructive/40 bg-destructive/5"
          )}
        >
          <span aria-hidden="true" className={cn("mt-1.5 size-2 shrink-0 rounded-full", testResult.success ? "bg-emerald-500" : "bg-destructive")} />
          <div className="min-w-0">
            <p className="font-medium">{t(testResult.success ? "arrInstances.toasts.connectionSuccess" : "arrInstances.status.failed")}</p>
            {!testResult.success && (
              <p className="mt-0.5 text-xs text-muted-foreground [overflow-wrap:anywhere]">{testResult.error || unknownError}</p>
            )}
          </div>
        </div>
      )}
    </Editor>
  )
}
