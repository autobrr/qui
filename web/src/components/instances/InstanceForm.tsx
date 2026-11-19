/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useForm } from "@tanstack/react-form"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { MigrationHint } from "@/components/instances/MigrationHint"
import { PublicPeerAddressField } from "@/components/instances/PublicPeerAddressField"
import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useInstances } from "@/hooks/useInstances"
import { formatErrorMessage } from "@/lib/format-error-message"
import { DEFAULT_REANNOUNCE_SETTINGS, instanceUrlSchema } from "@/lib/instance-validation"
import type { Instance, InstanceFormData } from "@/types"

type InstanceAuthType = "none" | "usernamePassword" | "apiKey"

// The text fields in the order of the form. A failed submit focuses the first one with an error.
const TEXT_FIELDS = ["name", "host", "username", "password", "apiKey", "basicUsername", "basicPassword"] as const
type TextField = typeof TEXT_FIELDS[number]

interface InstanceFormProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The instance to edit. Without it, the form adds a new instance. */
  instance?: Instance
  /** Runs when the save succeeds. The form does not close itself. */
  onSuccess: () => void
}

function getInstanceAuthType(instance?: Instance): InstanceAuthType {
  return instance?.hasApiKey ? "apiKey" : instance?.username ? "usernamePassword" : "none"
}

function getInstanceFormDefaults(instance?: Instance): InstanceFormData {
  return {
    name: instance?.name ?? "",
    host: instance?.host ?? "http://localhost:8080",
    username: instance?.username ?? "",
    password: "",
    apiKey: instance?.hasApiKey ? "<redacted>" : "",
    basicUsername: instance?.basicUsername ?? "",
    basicPassword: instance?.basicUsername ? "<redacted>" : "",
    tlsSkipVerify: instance?.tlsSkipVerify ?? false,
    hasLocalFilesystemAccess: instance?.hasLocalFilesystemAccess ?? false,
    reannounceSettings: instance?.reannounceSettings ?? DEFAULT_REANNOUNCE_SETTINGS,
    publicPeerAddress: instance?.publicPeerAddress ?? "",
  }
}

/** The request body for the form values: credentials that the auth type does not use are cleared, and kept secrets are left out. */
function toSubmitData(data: InstanceFormData, authType: InstanceAuthType, showBasicAuth: boolean): InstanceFormData {
  let submitData: InstanceFormData

  if (showBasicAuth) {
    if (data.basicPassword === "<redacted>") {
      // Without basicPassword, the server keeps the saved one.
      const { basicPassword, ...dataWithoutPassword } = data
      submitData = dataWithoutPassword
    } else {
      // Send the actual password (could be empty to clear, or new password)
      submitData = data
    }
  } else {
    submitData = { ...data, basicUsername: "", basicPassword: "" }
  }

  if (authType === "none") {
    submitData = { ...submitData, username: "", password: "", apiKey: "" }
  }

  if (authType === "usernamePassword") {
    submitData = { ...submitData, apiKey: "" }
    if (submitData.password === "") {
      const { password, ...rest } = submitData
      submitData = rest
    }
  }

  if (authType === "apiKey") {
    submitData = { ...submitData, username: "", password: "" }
    if (submitData.apiKey === "" || submitData.apiKey === "<redacted>") {
      const { apiKey, ...rest } = submitData
      submitData = rest
    }
  }

  return submitData
}

/** The editor that adds a qBittorrent instance, or edits the connection of one. */
export function InstanceForm({ open, onOpenChange, instance, onSuccess }: InstanceFormProps) {
  const { t } = useTranslation("instances")
  const { createInstance, updateInstance, isCreating, isUpdating } = useInstances()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  const [authType, setAuthType] = useState<InstanceAuthType>(() => getInstanceAuthType(instance))
  const [saveError, setSaveError] = useState<string>()
  const inputs = useRef<Partial<Record<TextField, HTMLInputElement | null>>>({})

  const save = (data: InstanceFormData) => {
    const submitData = toSubmitData(data, authType, showBasicAuth)
    setSaveError(undefined)

    if (instance) {
      updateInstance({ id: instance.id, data: submitData }, {
        onSuccess: () => {
          toast.success(t("form.toast.instanceUpdatedTitle"), { description: t("form.toast.instanceUpdatedDescription") })
          onSuccess()
        },
        onError: (error) => setSaveError(error instanceof Error ? formatErrorMessage(error.message) : t("form.toast.updateFailedDescription")),
      })
    } else {
      createInstance(submitData, {
        onSuccess: () => {
          toast.success(t("form.toast.instanceCreatedTitle"), { description: t("form.toast.instanceCreatedDescription") })
          onSuccess()
        },
        onError: (error) => setSaveError(error instanceof Error ? formatErrorMessage(error.message) : t("form.toast.createFailedDescription")),
      })
    }
  }

  const form = useForm({
    defaultValues: getInstanceFormDefaults(instance),
    onSubmit: ({ value }) => save(value),
    onSubmitInvalid: ({ formApi }) => {
      const invalid = TEXT_FIELDS.find((name) => formApi.getFieldMeta(name)?.errors.length)
      if (invalid) inputs.current[invalid]?.focus()
    },
  })

  const inputRef = (name: TextField) => (input: HTMLInputElement | null) => {
    inputs.current[name] = input
  }
  const shownError = (meta: { isTouched: boolean; errors: unknown[] }) =>
    meta.isTouched && meta.errors[0] ? String(meta.errors[0]) : undefined

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={instance ? t("form.editTitle") : t("form.addTitle")}
      description={instance ? undefined : t("form.addDescription")}
      submitLabel={instance ? t("form.buttons.updateInstance") : t("form.buttons.addInstance")}
      onSubmit={() => void form.handleSubmit()}
      pending={isCreating || isUpdating}
      error={saveError}
    >
      {!instance && <MigrationHint />}

      <form.Field
        name="name"
        validators={{ onChange: ({ value }) => !value ? t("form.validation.nameRequired") : undefined }}
      >
        {(field) => (
          <Field label={t("form.labels.instanceName")} error={shownError(field.state.meta)}>
            <Input
              ref={inputRef("name")}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder={t("form.placeholders.instanceName")}
              autoComplete="off"
            />
          </Field>
        )}
      </form.Field>

      <form.Field
        name="host"
        validators={{
          onChange: ({ value }) => {
            const result = instanceUrlSchema.safeParse(value)
            return result.success ? undefined : result.error.issues[0]?.message
          },
        }}
      >
        {(field) => (
          <Field label={t("form.labels.url")} error={shownError(field.state.meta)}>
            <Input
              ref={inputRef("host")}
              value={field.state.value}
              onBlur={() => {
                field.handleBlur()
                const parsed = instanceUrlSchema.safeParse(field.state.value)
                if (parsed.success && parsed.data !== field.state.value) {
                  field.handleChange(parsed.data)
                }
              }}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder={t("form.placeholders.url")}
              inputMode="url"
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              className="font-mono text-[13px]"
            />
          </Field>
        )}
      </form.Field>

      <Field label={t("form.labels.authType")} help={t("form.labels.authTypeDescription")}>
        {(control) => (
          <Select value={authType} onValueChange={(value) => setAuthType(value as InstanceAuthType)}>
            <SelectTrigger {...control} className="w-full max-sm:data-[size=default]:h-10">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{t("form.authType.none")}</SelectItem>
              <SelectItem value="usernamePassword">{t("form.authType.usernamePassword")}</SelectItem>
              <SelectItem value="apiKey">{t("form.authType.apiKey")}</SelectItem>
            </SelectContent>
          </Select>
        )}
      </Field>

      {authType === "usernamePassword" && (
        <div className="grid gap-5 sm:grid-cols-2">
          <form.Field
            name="username"
            validators={{ onChange: ({ value }) => !value?.trim() ? t("form.validation.usernameRequired") : undefined }}
          >
            {(field) => (
              <Field label={t("form.labels.username")} error={shownError(field.state.meta)}>
                <Input
                  ref={inputRef("username")}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("form.placeholders.username")}
                  autoComplete="off"
                  autoCapitalize="off"
                />
              </Field>
            )}
          </form.Field>

          <form.Field
            name="password"
            // An instance that already has a username keeps its saved password when the field stays empty.
            validators={{ onChange: ({ value }) => !value?.trim() && !instance?.username ? t("form.validation.passwordRequired") : undefined }}
          >
            {(field) => (
              <Field label={t("form.labels.password")} error={shownError(field.state.meta)}>
                <Input
                  ref={inputRef("password")}
                  type="password"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={instance ? t("form.placeholders.passwordExisting") : t("form.placeholders.passwordNew")}
                  autoComplete="off"
                />
              </Field>
            )}
          </form.Field>
        </div>
      )}

      {authType === "apiKey" && (
        <form.Field
          name="apiKey"
          validators={{
            onChange: ({ value }) => {
              const keepsSavedKey = instance?.hasApiKey && value === "<redacted>"
              return !keepsSavedKey && !value?.trim() ? t("form.validation.apiKeyRequired") : undefined
            },
          }}
        >
          {(field) => (
            <Field label={t("form.authType.apiKey")} error={shownError(field.state.meta)}>
              <Input
                ref={inputRef("apiKey")}
                type="password"
                value={field.state.value}
                onBlur={() => {
                  field.handleBlur()
                  if (instance?.hasApiKey && field.state.value === "") {
                    field.handleChange("<redacted>")
                  }
                }}
                onFocus={() => {
                  if (field.state.value === "<redacted>") {
                    field.handleChange("")
                  }
                }}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={instance ? t("form.placeholders.apiKeyExisting") : t("form.placeholders.apiKeyNew")}
                autoComplete="off"
              />
            </Field>
          )}
        </form.Field>
      )}

      <div className="space-y-5 border-t pt-5">
        <SwitchRow
          label={t("form.labels.httpBasicAuth")}
          help={t("form.labels.httpBasicAuthDescription")}
          checked={showBasicAuth}
          onCheckedChange={setShowBasicAuth}
        />

        {showBasicAuth && (
          <div className="grid gap-5 sm:grid-cols-2">
            <form.Field name="basicUsername">
              {(field) => (
                <Field label={t("form.labels.basicAuthUsername")}>
                  <Input
                    ref={inputRef("basicUsername")}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("form.placeholders.basicAuthUsername")}
                    autoComplete="off"
                    autoCapitalize="off"
                  />
                </Field>
              )}
            </form.Field>

            <form.Field
              name="basicPassword"
              validators={{ onChange: ({ value }) => value === "" ? t("form.validation.basicAuthPasswordRequired") : undefined }}
            >
              {(field) => (
                <Field label={t("form.labels.basicAuthPassword")} error={field.state.meta.errors[0] ? String(field.state.meta.errors[0]) : undefined}>
                  <Input
                    ref={inputRef("basicPassword")}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onFocus={() => {
                      // Clear the redacted placeholder when user focuses to edit
                      if (field.state.value === "<redacted>") {
                        field.handleChange("")
                      }
                    }}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("form.placeholders.basicAuthPassword")}
                    autoComplete="off"
                  />
                </Field>
              )}
            </form.Field>
          </div>
        )}

        <form.Field name="tlsSkipVerify">
          {(field) => (
            <SwitchRow
              label={t("form.labels.skipTlsVerification")}
              help={t("form.labels.skipTlsDescription")}
              checked={field.state.value}
              onCheckedChange={field.handleChange}
            />
          )}
        </form.Field>

        <form.Field name="hasLocalFilesystemAccess">
          {(field) => (
            <SwitchRow
              label={t("form.labels.localFilesystemAccess")}
              help={t("form.labels.localFilesystemDescription")}
              description={instance ? undefined : t("form.sshAfterSave")}
              checked={field.state.value}
              onCheckedChange={field.handleChange}
            />
          )}
        </form.Field>

        {instance && (
          <form.Field name="publicPeerAddress">
            {(field) => (
              <PublicPeerAddressField
                value={field.state.value ?? ""}
                resolvedValue={instance.resolvedPublicPeerAddress}
                onChange={field.handleChange}
              />
            )}
          </form.Field>
        )}
      </div>
    </Editor>
  )
}
