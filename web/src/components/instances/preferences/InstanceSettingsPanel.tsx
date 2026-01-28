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
import { toast } from "sonner"

interface InstanceSettingsPanelProps {
  instance: Instance
  onSuccess?: () => void
}

export function InstanceSettingsPanel({ instance, onSuccess }: InstanceSettingsPanelProps) {
  const { updateInstance, isUpdating } = useInstances()
  const [incognitoMode] = useIncognitoMode()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  const [useCredentials, setUseCredentials] = useState(instance?.username !== "")

  useEffect(() => {
    setUseCredentials(instance?.username !== "")
  }, [instance?.username])

  useEffect(() => {
    setShowBasicAuth(!!instance?.basicUsername)
  }, [instance?.basicUsername])

  const handleSubmit = (data: InstanceFormData) => {
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

    if (!useCredentials) {
      submitData = {
        ...submitData,
        username: "",
        password: "",
      }
    } else if (submitData.password === "") {
      // Omit empty password to preserve existing credentials
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { password, ...rest } = submitData
      submitData = rest
    }

    updateInstance({ id: instance.id, data: submitData }, {
      onSuccess: () => {
        toast.success("Instance Updated", {
          description: "Instance settings updated successfully.",
        })
        onSuccess?.()
      },
      onError: (error) => {
        toast.error("Update Failed", {
          description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to update instance",
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
        basicUsername: instance?.basicUsername ?? "",
        basicPassword: instance?.basicUsername ? "<redacted>" : "",
        tlsSkipVerify: instance?.tlsSkipVerify ?? false,
        hasLocalFilesystemAccess: instance?.hasLocalFilesystemAccess ?? false,
        reannounceSettings: instance?.reannounceSettings ?? DEFAULT_REANNOUNCE_SETTINGS,
      })
      setShowBasicAuth(!!instance?.basicUsername)
      setUseCredentials(instance?.username !== "")
    }
  }, [instance, form])

  return (
    <div className="space-y-6">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          form.handleSubmit()
        }}
        className="space-y-6"
      >
        {/* Connection Settings */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field
            name="name"
            validators={{
              onChange: ({ value }) =>
                !value ? "Instance name is required" : undefined,
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor={field.name}>
                  Instance Name <span className="text-destructive" aria-hidden="true">*</span>
                </Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder="e.g., Main Server"
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
                  URL <span className="text-destructive" aria-hidden="true">*</span>
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
                  placeholder="http://localhost:8080"
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
                  <span className="text-sm font-medium">Skip TLS Verification</span>
                  <p id="tls-skip-verify-desc" className="text-xs text-muted-foreground">
                    Allow connections to qBittorrent instances that use self-signed or otherwise untrusted certificates.
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
                  <span className="text-sm font-medium">Local Filesystem Access</span>
                  <p id="local-filesystem-access-desc" className="text-xs text-muted-foreground">
                    Enable if qui can access this instance's download paths (required for hardlink detection in automations).
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
            <label htmlFor="credentials-toggle" className="flex items-center justify-between cursor-pointer">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">qBittorrent Login</span>
                <p id="credentials-toggle-desc" className="text-xs text-muted-foreground">
                  Disable if qBittorrent bypasses authentication for localhost or whitelisted IPs.
                </p>
              </div>
              <Switch
                id="credentials-toggle"
                checked={useCredentials}
                onCheckedChange={setUseCredentials}
                aria-describedby="credentials-toggle-desc"
              />
            </label>

            {useCredentials && (
              <div className="grid grid-cols-1 gap-4 mt-4 pt-4 border-t">
                <form.Field name="username">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">Username</Label>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder="admin"
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
                      <Label htmlFor={field.name} className="text-sm">Password</Label>
                      <Input
                        id={field.name}
                        type="password"
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder="Leave empty to keep current"
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
          </div>

          {/* HTTP Basic Auth */}
          <div className="rounded-lg border bg-muted/40 p-4 flex flex-col">
            <label htmlFor="basic-auth-toggle" className="flex items-center justify-between cursor-pointer">
              <div className="space-y-0.5">
                <span className="text-sm font-medium">HTTP Basic Authentication</span>
                <p id="basic-auth-toggle-desc" className="text-xs text-muted-foreground">
                  Enable if your qBittorrent is behind a reverse proxy with Basic Auth
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
                      <Label htmlFor={field.name} className="text-sm">Username</Label>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder="Username"
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
                      showBasicAuth && value === "" ? "Password required" : undefined,
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor={field.name} className="text-sm">Password</Label>
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
                        placeholder="Password"
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

        {/* Save Button */}
        <div className="flex justify-end">
          <form.Subscribe
            selector={(state) => [state.canSubmit, state.isSubmitting]}
          >
            {([canSubmit, isSubmitting]) => (
              <Button
                type="submit"
                disabled={!canSubmit || isSubmitting || isUpdating}
                className="min-w-32"
              >
                {(isSubmitting || isUpdating) ? "Saving..." : "Save Changes"}
              </Button>
            )}
          </form.Subscribe>
        </div>
      </form>
    </div>
  )
}
