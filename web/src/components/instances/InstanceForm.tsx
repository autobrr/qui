/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstances } from "@/hooks/useInstances"
import { formatErrorMessage } from "@/lib/utils"
import type { Instance, InstanceFormData } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { z } from "zod"

interface InstanceFormProps {
  instance?: Instance
  onSuccess: () => void
  onCancel: () => void
}

export function InstanceForm({ instance, onSuccess, onCancel }: InstanceFormProps) {
  const { t } = useTranslation()
  const { createInstance, updateInstance, isCreating, isUpdating } = useInstances()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  const [authBypass, setAuthBypass] = useState(false)

  // URL validation schema
  const urlSchema = z
    .string()
    .min(1, t("instances.form.url.required"))
    .transform((value) => {
      return value.includes("://") ? value : `http://${value}`
    })
    .refine((url) => {
      try {
        new URL(url)
        return true
      } catch {
        return false
      }
    }, t("instances.form.url.invalid"))
    .refine((url) => {
      const parsed = new URL(url)
      return parsed.protocol === "http:" || parsed.protocol === "https:"
    }, t("instances.form.url.protocol"))
    .refine((url) => {
      const parsed = new URL(url)
      const hostname = parsed.hostname

      const isIPv4 = /^(\d{1,3}\.){3}\d{1,3}$/.test(hostname)
      const isIPv6 = hostname.startsWith("[") && hostname.endsWith("]")

      if (isIPv4 || isIPv6) {
        // default ports such as 80 and 443 are omitted from the result of new URL()
        const hasExplicitPort = url.match(/:(\d+)(?:\/|$)/)
        if (!hasExplicitPort) {
          return false
        }
      }

      return true
    }, t("instances.form.url.portRequired"))

  const handleSubmit = (data: InstanceFormData) => {
    let submitData: InstanceFormData

    if (showBasicAuth) {
      // If basic auth is enabled, only include basicPassword if it's not the redacted placeholder
      if (data.basicPassword === t("instances.form.redacted")) {
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

    if (instance) {
      updateInstance({ id: instance.id, data: submitData }, {
        onSuccess: () => {
          toast.success(t("instances.form.notifications.updateSuccessTitle"), {
            description: t("instances.form.notifications.updateSuccessDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(t("instances.form.notifications.updateErrorTitle"), {
            description: error instanceof Error ? formatErrorMessage(error.message) : t("instances.form.notifications.updateErrorDescription"),
          })
        },
      })
    } else {
      createInstance(submitData, {
        onSuccess: () => {
          toast.success(t("instances.form.notifications.createSuccessTitle"), {
            description: t("instances.form.notifications.createSuccessDescription"),
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error(t("instances.form.notifications.createErrorTitle"), {
            description: error instanceof Error ? formatErrorMessage(error.message) : t("instances.form.notifications.createErrorDescription"),
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
      basicPassword: instance?.basicUsername ? t("instances.form.redacted") : "",
      tlsSkipVerify: instance?.tlsSkipVerify ?? false,
    },
    onSubmit: ({ value }) => {
      handleSubmit(value)
    },
  })

  return (
    <>
      <form
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
              !value ? t("instances.form.name.required") : undefined,
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={field.name}>{t("instances.form.name.label")}</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={t("instances.form.name.placeholder")}
                data-1p-ignore
                autoComplete='off'
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
              const result = urlSchema.safeParse(value)
              return result.success ? undefined : result.error.issues[0]?.message
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
                                <Label htmlFor={field.name}>{t("common.url")}</Label>              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder={t("instances.form.url.placeholder")}
              />
              {field.state.meta.isTouched && field.state.meta.errors[0] && (
                <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
              )}
            </div>
          )}
        </form.Field>

        <form.Field name="tlsSkipVerify">
          {(field) => (
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/30 p-4">
              <div className="space-y-1">
                <Label htmlFor="tls-skip-verify">{t("instances.form.tls.label")}</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  {t("instances.form.tls.description")}
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

        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label htmlFor="auth-bypass-toggle">{t("instances.form.authBypass.label")}</Label>
              <p className="text-sm text-muted-foreground pr-2">
                {t("instances.form.authBypass.description")}
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
                                        <Label htmlFor={field.name}>{t("common.username")}</Label>                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("instances.form.qbitAuth.usernamePlaceholder")}
                    data-1p-ignore
                    autoComplete='off'
                  />
                </div>
              )}
            </form.Field>

            <form.Field
              name="password"
            >
              {(field) => (
                <div className="space-y-2">
                                        <Label htmlFor={field.name}>{t("common.password")}</Label>                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={instance ? t("instances.form.qbitAuth.passwordPlaceholderExisting") : t("instances.form.qbitAuth.passwordPlaceholder")}
                    data-1p-ignore
                    autoComplete='off'
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
              <Label htmlFor="basic-auth-toggle">{t("instances.form.basicAuth.label")}</Label>
              <p className="text-sm text-muted-foreground">
                {t("instances.form.basicAuth.description")}
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
                    <Label htmlFor={field.name}>{t("instances.form.basicAuth.usernameLabel")}</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("instances.form.basicAuth.usernamePlaceholder")}
                      data-1p-ignore
                      autoComplete='off'
                    />
                  </div>
                )}
              </form.Field>

              <form.Field
                name="basicPassword"
                validators={{
                  onChange: ({ value }) =>
                    showBasicAuth && value === ""? t("instances.form.basicAuth.required"): undefined,
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{t("instances.form.basicAuth.passwordLabel")}</Label>
                    <Input
                      id={field.name}
                      type="password"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onFocus={() => {
                        // Clear the redacted placeholder when user focuses to edit
                        if (field.state.value === t("instances.form.redacted")) {
                          field.handleChange("")
                        }
                      }}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("instances.form.basicAuth.passwordPlaceholder")}
                      data-1p-ignore
                      autoComplete='off'
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


        <div className="flex gap-2">
          <form.Subscribe
            selector={(state) => [state.canSubmit, state.isSubmitting]}
          >
            {([canSubmit, isSubmitting]) => (
              <Button
                type="submit"
                disabled={!canSubmit || isSubmitting || isCreating || isUpdating}
              >
                {(isCreating || isUpdating) ? t("instances.form.buttons.saving") : instance ? t("instances.form.buttons.update") : t("instances.add")}
              </Button>
            )}
          </form.Subscribe>

          <Button
            type="button"
            variant="outline"
            onClick={onCancel}
          >
            {t("common.cancel")}
          </Button>
        </div>
      </form>

    </>
  )
}
