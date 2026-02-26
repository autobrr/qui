/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstances } from "@/hooks/useInstances"
import { DEFAULT_REANNOUNCE_SETTINGS, instanceUrlSchema } from "@/lib/instance-validation"
import { formatErrorMessage } from "@/lib/utils"
import type { Instance, InstanceFormData } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface InstanceFormProps {
  instance?: Instance
  onSuccess: () => void
  onCancel: () => void
  /** When provided, renders without internal buttons (for external DialogFooter) */
  formId?: string
}

export function InstanceForm({ instance, onSuccess, onCancel, formId }: InstanceFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { createInstance, updateInstance, isCreating, isUpdating } = useInstances()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  const [authBypass, setAuthBypass] = useState(instance?.username === "")

  useEffect(() => {
    setAuthBypass(instance?.username === "")
  }, [instance?.username])

  const handleSubmit = (data: InstanceFormData) => {
    let submitData: InstanceFormData

    if (showBasicAuth) {
      // If basic auth is enabled, only include basicPassword if it's not the redacted placeholder
      if (data.basicPassword === "<redacted>") {
        // Don't send basicPassword at all - this preserves existing password
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { basicPassword, ...dataWithoutPassword } = data
        submitData = dataWithoutPassword
      } else {
        // Send the actual password (could be empty to clear, or new password)
        submitData = data
      }
    } else {
      // Basic auth disabled - clear basic auth credentials
      submitData = {
        ...data,
        basicUsername: "",
        basicPassword: "",
      }
    }

    if (authBypass) {
      submitData = {
        ...submitData,
        username: "",
        password: "",
      }
    }

    if (instance) {
      updateInstance({ id: instance.id, data: submitData }, {
        onSuccess: () => {
          toast.success(tr("instanceForm.toasts.updatedTitle"), {
            description: tr("instanceForm.toasts.updatedDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(tr("instanceForm.toasts.updateFailedTitle"), {
            description: error instanceof Error
              ? formatErrorMessage(error.message)
              : tr("instanceForm.toasts.updateFailedDescription"),
          })
        },
      })
    } else {
      createInstance(submitData, {
        onSuccess: () => {
          toast.success(tr("instanceForm.toasts.createdTitle"), {
            description: tr("instanceForm.toasts.createdDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(tr("instanceForm.toasts.createFailedTitle"), {
            description: error instanceof Error
              ? formatErrorMessage(error.message)
              : tr("instanceForm.toasts.createFailedDescription"),
          })
        },
      })
    }
  }

  const form = useForm({
    defaultValues: {
      name: instance?.name ?? "",
      host: instance?.host ?? "http://localhost:8080",
      username: instance?.username ?? "",
      password: "",
      basicUsername: instance?.basicUsername ?? "",
      basicPassword: instance?.basicUsername ? "<redacted>" : "",
      tlsSkipVerify: instance?.tlsSkipVerify ?? false,
      hasLocalFilesystemAccess: instance?.hasLocalFilesystemAccess ?? false,
      reannounceSettings: instance?.reannounceSettings ?? DEFAULT_REANNOUNCE_SETTINGS,
    },
    onSubmit: ({ value }) => {
      handleSubmit(value)
    },
  })

  return (
    <>
      <form
        id={formId}
        onSubmit={(e) => {
          e.preventDefault()
          form.handleSubmit()
        }}
        className="space-y-4"
      >
        <form.Field
          name="name"
          validators={{
            onChange: ({ value }) =>
              !value ? tr("instanceForm.validation.nameRequired") : undefined,
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={field.name}>{tr("instanceForm.fields.instanceName")}</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={tr("instanceForm.placeholders.instanceName")}
                data-1p-ignore
                autoComplete="off"
              />
              {field.state.meta.isTouched && field.state.meta.errors[0] && (
                <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
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
            <div className="space-y-2">
              <Label htmlFor={field.name}>{tr("instanceForm.fields.url")}</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={() => {
                  field.handleBlur()
                  const parsed = instanceUrlSchema.safeParse(field.state.value)
                  if (parsed.success && parsed.data !== field.state.value) {
                    field.handleChange(parsed.data)
                  }
                }}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={tr("instanceForm.placeholders.url")}
              />
              {field.state.meta.isTouched && field.state.meta.errors[0] && (
                <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field name="tlsSkipVerify">
          {(field) => (
            <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/40 p-4">
              <div className="space-y-1">
                <Label htmlFor="tls-skip-verify">{tr("instanceForm.fields.skipTlsVerification")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {tr("instanceForm.fields.skipTlsVerificationDescription")}
                </p>
              </div>
              <Switch
                id="tls-skip-verify"
                checked={field.state.value}
                onCheckedChange={(checked) => field.handleChange(checked)}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="hasLocalFilesystemAccess">
          {(field) => (
            <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/40 p-4">
              <div className="space-y-1">
                <Label htmlFor="local-filesystem-access">{tr("instanceForm.fields.localFilesystemAccess")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {tr("instanceForm.fields.localFilesystemAccessDescription")}
                </p>
              </div>
              <Switch
                id="local-filesystem-access"
                checked={field.state.value}
                onCheckedChange={(checked) => field.handleChange(checked)}
              />
            </div>
          )}
        </form.Field>

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="auth-bypass-toggle">{tr("instanceForm.fields.authenticationBypass")}</Label>
              <p className="text-sm text-muted-foreground pr-2">
                {tr("instanceForm.fields.authenticationBypassDescription")}
              </p>
            </div>
            <Switch
              id="auth-bypass-toggle"
              checked={authBypass}
              onCheckedChange={setAuthBypass}
            />
          </div>
        </div>

        {!authBypass && (
          <>
            <form.Field name="username">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor={field.name}>{tr("instanceForm.fields.username")}</Label>
                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={tr("instanceForm.placeholders.username")}
                    data-1p-ignore
                    autoComplete="off"
                  />
                </div>
              )}
            </form.Field>

            <form.Field
              name="password"
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor={field.name}>{tr("instanceForm.fields.password")}</Label>
                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={instance
                      ? tr("instanceForm.placeholders.passwordKeepCurrent")
                      : tr("instanceForm.placeholders.password")}
                    data-1p-ignore
                    autoComplete="off"
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>
          </>
        )}

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="basic-auth-toggle">{tr("instanceForm.fields.httpBasicAuthentication")}</Label>
              <p className="text-sm text-muted-foreground">
                {tr("instanceForm.fields.httpBasicAuthenticationDescription")}
              </p>
            </div>
            <Switch
              id="basic-auth-toggle"
              checked={showBasicAuth}
              onCheckedChange={setShowBasicAuth}
            />
          </div>

          {showBasicAuth && (
            <div className="space-y-4 pl-6 border-l-2 border-muted">
              <form.Field name="basicUsername">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{tr("instanceForm.fields.basicAuthUsername")}</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={tr("instanceForm.placeholders.basicAuthUsername")}
                      data-1p-ignore
                      autoComplete="off"
                    />
                  </div>
                )}
              </form.Field>

              <form.Field
                name="basicPassword"
                validators={{
                  onChange: ({ value }) =>
                    showBasicAuth && value === ""
                      ? tr("instanceForm.validation.basicAuthPasswordRequired")
                      : undefined,
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{tr("instanceForm.fields.basicAuthPassword")}</Label>
                    <Input
                      id={field.name}
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
                      placeholder={tr("instanceForm.placeholders.basicAuthPassword")}
                      data-1p-ignore
                      autoComplete="off"
                    />
                    {field.state.meta.errors[0] && (
                      <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                    )}
                  </div>
                )}
              </form.Field>
            </div>
          )}
        </div>

        {!formId && (
          <div className="flex gap-2">
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
            >
              {([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  disabled={!canSubmit || isSubmitting || isCreating || isUpdating}
                >
                  {(isCreating || isUpdating)
                    ? tr("instanceForm.actions.saving")
                    : instance
                      ? tr("instanceForm.actions.updateInstance")
                      : tr("instanceForm.actions.addInstance")}
                </Button>
              )}
            </form.Subscribe>

            <Button
              type="button"
              variant="outline"
              onClick={onCancel}
            >
              {tr("instanceForm.actions.cancel")}
            </Button>
          </div>
        )}
      </form>

    </>
  )
}
