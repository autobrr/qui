/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useRef, useState, type ChangeEvent } from "react"
import { useTranslation } from "react-i18next"

import { Editor } from "@/components/kit/Editor"
import { Facts } from "@/components/kit/Facts"
import { Field } from "@/components/kit/Field"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { TorznabIndexer, TorznabIndexerFormData } from "@/types"

const REDACTED = "<redacted>"

export type IndexerSaveRequest =
  | { id: null; data: TorznabIndexerFormData }
  | { id: number; data: Partial<TorznabIndexerFormData> }

interface IndexerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The indexer to edit, or null for a new one. */
  indexer: TorznabIndexer | null
  pending: boolean
  error?: string
  onSave: (request: IndexerSaveRequest) => void
}

type Backend = TorznabIndexer["backend"]

interface FormValues {
  name: string
  backend: Backend
  baseUrl: string
  indexerId: string
  apiKey: string
  basicAuth: boolean
  basicUsername: string
  basicPassword: string
  priority: string
  timeout: string
  enabled: boolean
}

type FieldName = "name" | "baseUrl" | "indexerId" | "apiKey" | "basicUsername" | "basicPassword" | "priority" | "timeout"
type FormErrors = Partial<Record<FieldName, string>>

// The order in which a failed save puts focus on a field.
const FIELD_ORDER: readonly FieldName[] = ["name", "indexerId", "baseUrl", "apiKey", "priority", "timeout", "basicUsername", "basicPassword"]

function initialValues(indexer: TorznabIndexer | null): FormValues {
  if (!indexer) {
    return { name: "", backend: "jackett", baseUrl: "", indexerId: "", apiKey: "", basicAuth: false, basicUsername: "", basicPassword: "", priority: "0", timeout: "30", enabled: true }
  }
  const basicAuth = Boolean(indexer.basic_username)
  return {
    name: indexer.name,
    backend: indexer.backend,
    baseUrl: indexer.base_url,
    indexerId: indexer.indexer_id,
    // The server never returns the API key.
    apiKey: "",
    basicAuth,
    basicUsername: indexer.basic_username ?? "",
    basicPassword: basicAuth ? REDACTED : "",
    priority: String(indexer.priority),
    timeout: String(indexer.timeout_seconds),
    enabled: indexer.enabled,
  }
}

const isWholeNumber = (value: string) => /^\d+$/.test(value.trim())

function isAbsoluteUrl(value: string) {
  try {
    new URL(value)
    return true
  } catch {
    return false
  }
}

/** The form to add or edit one Torznab indexer. */
export function IndexerDialog({ open, onOpenChange, indexer, pending, error, onSave }: IndexerDialogProps) {
  const { t } = useTranslation("settings")
  const [values, setValues] = useState(() => initialValues(indexer))
  const [errors, setErrors] = useState<FormErrors>({})
  const inputs = useRef<Partial<Record<FieldName, HTMLInputElement | null>>>({})
  const editing = indexer !== null
  const baseUrlPlaceholder = values.backend === "prowlarr" ? "http://localhost:9696" : "http://localhost:9117"

  const validate = (): FormErrors => {
    const baseUrl = values.baseUrl.trim()
    const password = values.basicPassword
    const timeout = Number(values.timeout)
    return {
      name: values.name.trim() ? undefined : t("indexers.dialog.validation.nameRequired"),
      baseUrl: !baseUrl
        ? t("indexers.dialog.validation.baseUrlRequired")
        : isAbsoluteUrl(baseUrl) ? undefined : t("indexers.dialog.validation.baseUrlInvalid", { example: baseUrlPlaceholder }),
      indexerId: values.backend === "prowlarr" && !values.indexerId.trim() ? t("indexers.dialog.validation.indexerIdRequired") : undefined,
      apiKey: !editing && !values.apiKey.trim() ? t("indexers.dialog.validation.apiKeyRequired") : undefined,
      basicUsername: values.basicAuth && !values.basicUsername.trim() ? t("indexers.dialog.toast.basicUsernameRequired") : undefined,
      basicPassword: !values.basicAuth || password.trim() || password === REDACTED
        ? undefined
        : t(editing ? "indexers.dialog.toast.basicPasswordRequiredOrKeep" : "indexers.dialog.toast.basicPasswordRequired"),
      priority: isWholeNumber(values.priority) ? undefined : t("indexers.dialog.validation.priorityInvalid"),
      timeout: isWholeNumber(values.timeout) && timeout >= 5 && timeout <= 120 ? undefined : t("indexers.dialog.validation.timeoutInvalid"),
    }
  }

  const submit = () => {
    const next = validate()
    setErrors(next)
    const firstInvalid = FIELD_ORDER.find((name) => next[name])
    if (firstInvalid) {
      inputs.current[firstInvalid]?.focus()
      return
    }

    const indexerId = values.indexerId.trim()
    const basicUsername = values.basicUsername.trim()
    const priority = Number(values.priority)
    const timeout = Number(values.timeout)

    if (!indexer) {
      const data: TorznabIndexerFormData = {
        name: values.name,
        base_url: values.baseUrl,
        api_key: values.apiKey.trim(),
        backend: values.backend,
        enabled: values.enabled,
        priority,
        timeout_seconds: timeout,
      }
      if (indexerId) data.indexer_id = indexerId
      if (values.basicAuth) {
        data.basic_username = basicUsername
        data.basic_password = values.basicPassword
      }
      onSave({ id: null, data })
      return
    }

    const data: Partial<TorznabIndexerFormData> = {
      name: values.name,
      base_url: values.baseUrl,
      backend: values.backend,
      enabled: values.enabled,
      priority,
      timeout_seconds: timeout,
      indexer_id: indexerId,
    }
    const apiKey = values.apiKey.trim()
    if (apiKey) data.api_key = apiKey
    if (values.basicAuth) {
      data.basic_username = basicUsername
      if (values.basicPassword !== REDACTED) data.basic_password = values.basicPassword
    } else {
      // Empty values clear the stored basic auth.
      data.basic_username = ""
      data.basic_password = ""
    }
    onSave({ id: indexer.id, data })
  }

  const update = (patch: Partial<FormValues>) => setValues((prev) => ({ ...prev, ...patch }))

  const text = (name: FieldName & keyof FormValues) => ({
    ref: (input: HTMLInputElement | null) => {
      inputs.current[name] = input
    },
    value: values[name] as string,
    autoComplete: "off",
    onChange: (event: ChangeEvent<HTMLInputElement>) => {
      update({ [name]: event.target.value })
      setErrors((prev) => ({ ...prev, [name]: undefined }))
    },
  })

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t(editing ? "indexers.dialog.editTitle" : "indexers.dialog.addTitle")}
      description={t(editing ? "indexers.dialog.editDescription" : "indexers.dialog.addDescription")}
      submitLabel={t(editing ? "indexers.dialog.buttons.save" : "indexers.dialog.addTitle")}
      onSubmit={submit}
      pending={pending}
      error={error}
    >
      <Field label={t("indexers.dialog.labels.name")} error={errors.name}>
        <Input {...text("name")} placeholder={t("indexers.dialog.placeholders.name")} />
      </Field>
      <div className="grid gap-5 sm:grid-cols-2">
        <Field label={t("indexers.dialog.labels.backend")}>
          {(control) => (
            <Select
              value={values.backend}
              onValueChange={(backend) => {
                update({ backend: backend as Backend, indexerId: backend === "native" ? "" : values.indexerId })
                setErrors((prev) => ({ ...prev, indexerId: undefined }))
              }}
            >
              <SelectTrigger {...control} className="w-full max-sm:data-[size=default]:h-10">
                <SelectValue placeholder={t("indexers.dialog.placeholders.selectBackend")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="jackett">{t("indexers.dialog.backends.jackett")}</SelectItem>
                <SelectItem value="prowlarr">{t("indexers.dialog.backends.prowlarr")}</SelectItem>
                <SelectItem value="native">{t("indexers.dialog.backends.native")}</SelectItem>
              </SelectContent>
            </Select>
          )}
        </Field>
        {values.backend !== "native" && (
          <Field
            label={t("indexers.dialog.labels.indexerId")}
            help={t(values.backend === "prowlarr" ? "indexers.dialog.hints.indexerIdProwlarr" : "indexers.dialog.hints.indexerIdJackett")}
            error={errors.indexerId}
          >
            <Input
              {...text("indexerId")}
              placeholder={t(values.backend === "prowlarr" ? "indexers.dialog.placeholders.indexerIdProwlarr" : "indexers.dialog.placeholders.indexerIdJackett")}
            />
          </Field>
        )}
      </div>
      <Field label={t("indexers.dialog.labels.baseUrl")} error={errors.baseUrl}>
        <Input {...text("baseUrl")} inputMode="url" autoCapitalize="off" spellCheck={false} placeholder={baseUrlPlaceholder} className="font-mono text-[13px]" />
      </Field>
      <Field label={t("indexers.dialog.labels.apiKey")} error={errors.apiKey}>
        <Input
          {...text("apiKey")}
          type="password"
          placeholder={t(editing ? "indexers.dialog.placeholders.apiKeyEdit" : "indexers.dialog.placeholders.apiKeyCreate")}
        />
      </Field>
      <div className="grid grid-cols-2 gap-4">
        <Field label={t("indexers.dialog.labels.priority")} error={errors.priority}>
          <Input {...text("priority")} type="number" inputMode="numeric" />
        </Field>
        <Field label={t("indexers.dialog.labels.timeout")} error={errors.timeout}>
          <Input {...text("timeout")} type="number" inputMode="numeric" />
        </Field>
      </div>
      <SwitchRow
        label={t("indexers.dialog.labels.basicAuth")}
        help={t("indexers.dialog.labels.basicAuthDescription")}
        checked={values.basicAuth}
        onCheckedChange={(basicAuth) => {
          // Turning it off throws the credentials away, so turning it on again asks for both.
          update(basicAuth ? { basicAuth } : { basicAuth, basicUsername: "", basicPassword: "" })
          setErrors((prev) => ({ ...prev, basicUsername: undefined, basicPassword: undefined }))
        }}
      />
      {values.basicAuth && (
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label={t("indexers.dialog.labels.basicUsername")} error={errors.basicUsername}>
            <Input {...text("basicUsername")} placeholder={t("indexers.dialog.placeholders.username")} />
          </Field>
          <Field
            label={t("indexers.dialog.labels.basicPassword")}
            description={editing ? t("indexers.dialog.hints.basicPasswordKeep") : undefined}
            error={errors.basicPassword}
          >
            <Input {...text("basicPassword")} type="password" placeholder={editing ? REDACTED : t("indexers.dialog.placeholders.password")} />
          </Field>
        </div>
      )}
      <SwitchRow label={t("indexers.dialog.labels.enabled")} checked={values.enabled} onCheckedChange={(enabled) => update({ enabled })} />
      {indexer && (
        <Facts
          items={[
            {
              label: t("indexers.table.capabilities"),
              value: indexer.capabilities?.length ? indexer.capabilities.join(", ") : t("indexers.table.noCapabilities"),
              mono: Boolean(indexer.capabilities?.length),
            },
            // The list shows two lines of the last test error. The editor shows all of it.
            ...(indexer.last_test_status === "error" && indexer.last_test_error
              ? [{ label: t("indexers.toast.testFailed"), value: indexer.last_test_error, mono: true, copy: indexer.last_test_error }]
              : []),
          ]}
        />
      )}
    </Editor>
  )
}
