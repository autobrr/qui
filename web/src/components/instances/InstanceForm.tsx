/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useInstances } from "@/hooks/useInstances"
import { formatErrorMessage } from "@/lib/utils"
import type { Instance, InstanceFormData, InstanceReannounceSettings } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { z } from "zod"

const DEFAULT_REANNOUNCE_SETTINGS: InstanceReannounceSettings = {
  enabled: false,
  initialWaitSeconds: 15,
  reannounceIntervalSeconds: 7,
  maxAgeSeconds: 600,
  maxRetries: 50,
  aggressive: false,
  monitorAll: false,
  excludeCategories: false,
  categories: [],
  excludeTags: false,
  tags: [],
  excludeTrackers: false,
  trackers: [],
}

// URL validation schema
const urlSchema = z
  .string()
  .min(1, "URL is required")
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
  }, "Please enter a valid URL")
  .refine((url) => {
    const parsed = new URL(url)
    return parsed.protocol === "http:" || parsed.protocol === "https:"
  }, "Only HTTP and HTTPS protocols are supported")
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
  }, "Port is required when using an IP address (e.g., :8080)")

interface InstanceFormProps {
  instance?: Instance
  onSuccess: () => void
  onCancel: () => void
  /** When provided, renders without internal buttons (for external DialogFooter) */
  formId?: string
}

export function InstanceForm({ instance, onSuccess, onCancel, formId }: InstanceFormProps) {
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
          toast.success("Instance Updated", {
            description: "Instance updated successfully. Connection testing in background...",
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error("Update Failed", {
            description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to update instance",
          })
        },
      })
    } else {
      createInstance(submitData, {
        onSuccess: () => {
          toast.success("Instance Created", {
            description: "Instance created successfully. Connection testing in background...",
          })
          onSuccess()
        },
        onError: (error) => {
          toast.error("Create Failed", {
            description: error instanceof Error ? formatErrorMessage(error.message) : "Failed to create instance",
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
              !value ? "Instance name is required" : undefined,
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={field.name}>Instance Name</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="e.g., Main Server or Home qBittorrent"
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
              const result = urlSchema.safeParse(value)
              return result.success ? undefined : result.error.issues[0]?.message
            },
          }}
        >
          {(field) => (
            <div className="space-y-2">
              <Label htmlFor={field.name}>URL</Label>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="http://localhost:8080 or 192.168.1.100:8080"
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
                <Label htmlFor="tls-skip-verify">Skip TLS Certificate Verification</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  Allow connections to qBittorrent instances that use self-signed or otherwise untrusted certificates.
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
            <div className="flex items-start justify-between gap-4 rounded-lg border border-border/60 bg-muted/30 p-4">
              <div className="space-y-1">
                <Label htmlFor="local-filesystem-access">Local Filesystem Access</Label>
                <p className="text-sm text-muted-foreground max-w-prose">
                  Enable if qui can access this instance's download paths (required for hardlink detection in automations).
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
              <Label htmlFor="auth-bypass-toggle">Authentication Bypass</Label>
              <p className="text-sm text-muted-foreground pr-2">
                Enable when qBittorrent bypasses authentication for localhost or whitelisted IPs
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
                  <Label htmlFor={field.name}>Username</Label>
                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="qBittorrent username (usually admin)"
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
                  <Label htmlFor={field.name}>Password</Label>
                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={instance ? "Leave empty to keep current password" : "qBittorrent password"}
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
              <Label htmlFor="basic-auth-toggle">HTTP Basic Authentication</Label>
              <p className="text-sm text-muted-foreground">
                Enable if your qBittorrent is behind a reverse proxy with Basic Auth
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
                    <Label htmlFor={field.name}>Basic Auth Username</Label>
                    <Input
                      id={field.name}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="Basic auth username"
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
                    showBasicAuth && value === "" ? "Basic auth password is required when basic auth is enabled" : undefined,
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>Basic Auth Password</Label>
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
                      placeholder="Enter basic auth password (required)"
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
                  {(isCreating || isUpdating) ? "Saving..." : instance ? "Update Instance" : "Add Instance"}
                </Button>
              )}
            </form.Subscribe>

            <Button
              type="button"
              variant="outline"
              onClick={onCancel}
            >
              Cancel
            </Button>
          </div>
        )}
      </form>

    </>
  )
}
