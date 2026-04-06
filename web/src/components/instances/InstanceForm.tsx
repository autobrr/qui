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
  const { t } = useTranslation("instances")
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
          toast.success(t("form.toast.instanceUpdatedTitle"), {
            description: t("form.toast.instanceUpdatedDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(t("form.toast.updateFailedTitle"), {
            description: error instanceof Error ? formatErrorMessage(error.message) : t("form.toast.updateFailedDescription"),
          })
        },
      })
    } else {
      createInstance(submitData, {
        onSuccess: () => {
          toast.success(t("form.toast.instanceCreatedTitle"), {
            description: t("form.toast.instanceCreatedDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(t("form.toast.createFailedTitle"), {
            description: error instanceof Error ? formatErrorMessage(error.message) : t("form.toast.createFailedDescription"),
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
              !value ? t("form.validation.nameRequired") : undefined,
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={field.name}>{t("form.labels.instanceName")}</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={t("form.placeholders.instanceName")}
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
              <Label htmlFor={field.name}>{t("form.labels.url")}</Label>
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
                placeholder={t("form.placeholders.url")}
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
                <Label htmlFor="tls-skip-verify">{t("form.labels.skipTlsVerification")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {t("form.labels.skipTlsDescription")}
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
                <Label htmlFor="local-filesystem-access">{t("form.labels.localFilesystemAccess")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {t("form.labels.localFilesystemDescription")}
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
              <Label htmlFor="auth-bypass-toggle">{t("form.labels.authBypass")}</Label>
              <p className="text-sm text-muted-foreground pr-2">
                {t("form.labels.authBypassDescription")}
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
                  <Label htmlFor={field.name}>{t("form.labels.username")}</Label>
                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("form.placeholders.username")}
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
                  <Label htmlFor={field.name}>{t("form.labels.password")}</Label>
                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={instance ? t("form.placeholders.passwordExisting") : t("form.placeholders.passwordNew")}
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
              <Label htmlFor="basic-auth-toggle">{t("form.labels.httpBasicAuth")}</Label>
              <p className="text-sm text-muted-foreground">
                {t("form.labels.httpBasicAuthDescription")}
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
                    <Label htmlFor={field.name}>{t("form.labels.basicAuthUsername")}</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("form.placeholders.basicAuthUsername")}
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
                    showBasicAuth && value === "" ? t("form.validation.basicAuthPasswordRequired") : undefined,
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{t("form.labels.basicAuthPassword")}</Label>
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
                      placeholder={t("form.placeholders.basicAuthPassword")}
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
                  {(isCreating || isUpdating) ? t("form.buttons.saving") : instance ? t("form.buttons.updateInstance") : t("form.buttons.addInstance")}
                </Button>
              )}
            </form.Subscribe>

            <Button
              type="button"
              variant="outline"
              onClick={onCancel}
            >
              {t("form.buttons.cancel")}
            </Button>
          </div>
        )}
      </form>

    </>
  )
}
