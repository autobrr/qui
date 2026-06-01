/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstances } from "@/hooks/useInstances"
import { useIncognitoMode } from "@/lib/incognito"
import { DEFAULT_REANNOUNCE_SETTINGS, instanceUrlSchema } from "@/lib/instance-validation"
import { formatErrorMessage } from "@/lib/utils"
import type { Instance, InstanceFormData } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PreferencesFormShell } from "./PreferencesFormShell"

interface InstanceSettingsPanelProps {
  instance: Instance
  onSuccess?: () => void
}

export function InstanceSettingsPanel({ instance, onSuccess }: InstanceSettingsPanelProps) {
  const { t } = useTranslation("instances")
  const { updateInstance, isUpdating } = useInstances()
  const [incognitoMode] = useIncognitoMode()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  const [authType, setAuthType] = useState<"none" | "usernamePassword" | "apiKey">(
    instance?.hasApiKey ? "apiKey" : instance?.username ? "usernamePassword" : "none"
  )

  useEffect(() => {
    setShowBasicAuth(!!instance?.basicUsername)
  }, [instance?.basicUsername])

  const handleSubmit = (data: InstanceFormData) => {
    if (authType === "apiKey") {
      const hasPreservedAPIKey = instance.hasApiKey && data.apiKey === "<redacted>"
      if (!hasPreservedAPIKey && !data.apiKey?.trim()) {
        toast.error(t("preferences.settingsPanel.toast.missingCredentialsTitle"), {
          description: t("preferences.settingsPanel.validation.apiKeyRequired"),
        })
        return
      }
    }

    let submitData: InstanceFormData

    if (showBasicAuth) {
      if (data.basicPassword === "<redacted>") {
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { basicPassword, ...dataWithoutPassword } = data
        submitData = dataWithoutPassword
      } else {
        submitData = data
      }
    } else {
      submitData = {
        ...data,
        basicUsername: "",
        basicPassword: "",
      }
    }

    if (authType === "none") {
      submitData = {
        ...submitData,
        username: "",
        password: "",
        apiKey: "",
      }
    } else if (authType === "usernamePassword") {
      submitData = {
        ...submitData,
        apiKey: "",
      }
      if (submitData.password === "") {
        // Omit empty password to preserve existing credentials
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { password, ...rest } = submitData
        submitData = rest
      }
    } else {
      submitData = {
        ...submitData,
        username: "",
        password: "",
      }

      if (submitData.apiKey === "<redacted>") {
        // Omit redacted placeholder to preserve existing API key
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const { apiKey, ...rest } = submitData
        submitData = rest
      }
    }

    updateInstance({ id: instance.id, data: submitData }, {
      onSuccess: () => {
        toast.success(t("preferences.settingsPanel.toast.updatedTitle"), {
          description: t("preferences.settingsPanel.toast.updatedDescription"),
        })
        onSuccess?.()
      },
      onError: (error) => {
        toast.error(t("preferences.settingsPanel.toast.updateFailedTitle"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : t("preferences.settingsPanel.toast.updateFailedDescription"),
        })
      },
    })
  }

  const form = useForm({
    defaultValues: {
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
    },
    onSubmit: ({ value }) => {
      handleSubmit(value)
    },
  })

  // Reset form when instance changes
  const prevInstanceId = useRef(instance?.id)
  useEffect(() => {
    if (prevInstanceId.current !== instance?.id) {
      prevInstanceId.current = instance?.id
      form.reset({
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
      })
      setShowBasicAuth(!!instance?.basicUsername)
      setAuthType(instance?.hasApiKey ? "apiKey" : instance?.username ? "usernamePassword" : "none")
    }
  }, [instance, form])

  return (
    <PreferencesFormShell
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      footer={(
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {(isSubmitting || isUpdating) ? t("preferences.common.saving") : t("preferences.common.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      )}
    >
      <div className="space-y-6">
        {/* Connection Settings */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field
            name="name"
            validators={{
              onChange: ({ value }) =>
                !value ? t("preferences.settingsPanel.validation.nameRequired") : undefined,
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor={field.name}>
                  {t("preferences.settingsPanel.labels.instanceName")} <span className="text-destructive" aria-hidden="true">*</span>
                </Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("preferences.settingsPanel.placeholders.instanceName")}
                  data-1p-ignore
                  autoComplete="off"
                  aria-required="true"
                  aria-invalid={field.state.meta.isTouched && !!field.state.meta.errors[0]}
                />
                {field.state.meta.isTouched && field.state.meta.errors[0] && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
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
                <Label htmlFor={field.name}>
                  {t("preferences.settingsPanel.labels.url")} <span className="text-destructive" aria-hidden="true">*</span>
                </Label>
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
                  placeholder={t("preferences.settingsPanel.placeholders.url")}
                  className={incognitoMode ? "blur-sm select-none" : ""}
                  aria-required="true"
                  aria-invalid={field.state.meta.isTouched && !!field.state.meta.errors[0]}
                />
                {field.state.meta.isTouched && field.state.meta.errors[0] && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>
        </div>

        {/* Security Options */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="tlsSkipVerify">
            {(field) => (
              <label
                htmlFor="tls-skip-verify"
                className="flex items-center justify-between gap-4 rounded-lg border bg-muted/40 p-4 cursor-pointer"
              >
                <div className="space-y-0.5">
                  <span className="text-sm font-medium">{t("preferences.settingsPanel.labels.skipTlsVerification")}</span>
                  <p id="tls-skip-verify-desc" className="text-xs text-muted-foreground">
                    {t("preferences.settingsPanel.labels.skipTlsDescription")}
                  </p>
                </div>
                <Switch
                  id="tls-skip-verify"
                  checked={field.state.value}
                  onCheckedChange={(checked) => field.handleChange(checked)}
                  aria-describedby="tls-skip-verify-desc"
                />
              </label>
            )}
          </form.Field>

          <form.Field name="hasLocalFilesystemAccess">
            {(field) => (
              <label
                htmlFor="local-filesystem-access"
                className="flex items-center justify-between gap-4 rounded-lg border bg-muted/40 p-4 cursor-pointer"
              >
                <div className="space-y-0.5">
                  <span className="text-sm font-medium">{t("preferences.settingsPanel.labels.localFilesystemAccess")}</span>
                  <p id="local-filesystem-access-desc" className="text-xs text-muted-foreground">
                    {t("preferences.settingsPanel.labels.localFilesystemDescription")}
                  </p>
                </div>
                <Switch
                  id="local-filesystem-access"
                  checked={field.state.value}
                  onCheckedChange={(checked) => field.handleChange(checked)}
                  aria-describedby="local-filesystem-access-desc"
                />
              </label>
            )}
          </form.Field>
        </div>

        {/* Authentication */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
          <div className="rounded-lg border bg-muted/40 p-4 flex flex-col">
            <div className="space-y-2">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">{t("preferences.settingsPanel.labels.qbittorrentAuth")}</span>
                <p id="auth-type-desc" className="text-xs text-muted-foreground">
                  {t("preferences.settingsPanel.labels.qbittorrentAuthDescription")}
                </p>
              </div>
              <select
                id="auth-type"
                value={authType}
                onChange={(e) => setAuthType(e.target.value as "none" | "usernamePassword" | "apiKey")}
                aria-describedby="auth-type-desc"
                className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background"
              >
                <option value="none">{t("form.authType.none")}</option>
                <option value="usernamePassword">{t("form.authType.usernamePassword")}</option>
                <option value="apiKey">{t("form.authType.apiKey")}</option>
              </select>
            </div>

            {authType === "usernamePassword" && (
              <div className="grid grid-cols-1 gap-4 mt-4 pt-4 border-t">
                <form.Field name="username">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">{t("preferences.settingsPanel.labels.username")}</Label>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("preferences.settingsPanel.placeholders.username")}
                        data-1p-ignore
                        autoComplete="off"
                        className={incognitoMode ? "blur-sm select-none" : ""}
                      />
                    </div>
                  )}
                </form.Field>

                <form.Field name="password">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">{t("preferences.settingsPanel.labels.password")}</Label>
                      <Input
                        id={field.name}
                        type="password"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("preferences.settingsPanel.placeholders.passwordKeepCurrent")}
                        data-1p-ignore
                        autoComplete="off"
                      />
                      {field.state.meta.isTouched && field.state.meta.errors[0] && (
                        <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )}
                </form.Field>
              </div>
            )}

            {authType === "apiKey" && (
              <div className="grid grid-cols-1 gap-4 mt-4 pt-4 border-t">
                <form.Field name="apiKey">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">{t("form.authType.apiKey")}</Label>
                      <Input
                        id={field.name}
                        type="password"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onFocus={() => {
                          if (field.state.value === "<redacted>") {
                            field.handleChange("")
                          }
                        }}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("preferences.settingsPanel.placeholders.passwordKeepCurrent")}
                        data-1p-ignore
                        autoComplete="off"
                      />
                    </div>
                  )}
                </form.Field>
              </div>
            )}
          </div>

          {/* HTTP Basic Auth */}
          <div className="rounded-lg border bg-muted/40 p-4 flex flex-col">
            <label htmlFor="basic-auth-toggle" className="flex items-center justify-between cursor-pointer">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">{t("preferences.settingsPanel.labels.httpBasicAuth")}</span>
                <p id="basic-auth-toggle-desc" className="text-xs text-muted-foreground">
                  {t("preferences.settingsPanel.labels.httpBasicAuthDescription")}
                </p>
              </div>
              <Switch
                id="basic-auth-toggle"
                checked={showBasicAuth}
                onCheckedChange={setShowBasicAuth}
                aria-describedby="basic-auth-toggle-desc"
              />
            </label>

            {showBasicAuth && (
              <div className="grid grid-cols-1 gap-4 mt-4 pt-4 border-t">
                <form.Field name="basicUsername">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">{t("preferences.settingsPanel.labels.username")}</Label>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("preferences.settingsPanel.placeholders.basicUsername")}
                        data-1p-ignore
                        autoComplete="off"
                        className={incognitoMode ? "blur-sm select-none" : ""}
                      />
                    </div>
                  )}
                </form.Field>

                <form.Field
                  name="basicPassword"
                  validators={{
                    onChange: ({ value }) =>
                      showBasicAuth && value === "" ? t("preferences.settingsPanel.validation.passwordRequired") : undefined,
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">{t("preferences.settingsPanel.labels.password")}</Label>
                      <Input
                        id={field.name}
                        type="password"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onFocus={() => {
                          if (field.state.value === "<redacted>") {
                            field.handleChange("")
                          }
                        }}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder={t("preferences.settingsPanel.placeholders.basicPassword")}
                        data-1p-ignore
                        autoComplete="off"
                      />
                      {field.state.meta.errors[0] && (
                        <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )}
                </form.Field>
              </div>
            )}
          </div>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
