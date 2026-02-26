/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Shield, Server, Lock } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useIncognitoMode } from "@/lib/incognito"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface ProxySettingsFormProps {
  instanceId: number
  onSuccess?: () => void
}

function SwitchSetting({
  label,
  description,
  checked,
  onChange,
}: {
  label: string
  description?: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-center justify-between space-x-4">
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  )
}

function NumberInput({
  label,
  value,
  onChange,
  min = 0,
  max,
  description,
  placeholder,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  description?: string
  placeholder?: string
}) {
  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">{label}</Label>
      {description && (
        <p className="text-xs text-muted-foreground">{description}</p>
      )}
      <Input
        type="number"
        min={min}
        max={max}
        value={value || ""}
        onChange={(e) => {
          const val = parseInt(e.target.value)
          onChange(isNaN(val) ? 0 : val)
        }}
        placeholder={placeholder}
      />
    </div>
  )
}

export function ProxySettingsForm({ instanceId, onSuccess }: ProxySettingsFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const [incognitoMode] = useIncognitoMode()

  const form = useForm({
    defaultValues: {
      proxy_type: 0,
      proxy_ip: "",
      proxy_port: 0,
      proxy_username: "",
      proxy_password: "",
      proxy_auth_enabled: false,
      proxy_peer_connections: false,
      proxy_torrents_only: false,
      proxy_hostname_lookup: false,
    },
    onSubmit: async ({ value }) => {
      try {
        await updatePreferences(value)
        toast.success(tr("proxySettingsForm.toasts.updated"))
        onSuccess?.()
      } catch (error) {
        toast.error(tr("proxySettingsForm.toasts.failedUpdate"))
        console.error("Failed to update proxy settings:", error)
      }
    },
  })

  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("proxy_type", typeof preferences.proxy_type === "string" ? parseInt(preferences.proxy_type) : preferences.proxy_type)
      form.setFieldValue("proxy_ip", preferences.proxy_ip)
      form.setFieldValue("proxy_port", preferences.proxy_port)
      form.setFieldValue("proxy_username", preferences.proxy_username)
      form.setFieldValue("proxy_password", preferences.proxy_password)
      form.setFieldValue("proxy_auth_enabled", preferences.proxy_auth_enabled)
      form.setFieldValue("proxy_peer_connections", preferences.proxy_peer_connections)
      form.setFieldValue("proxy_torrents_only", preferences.proxy_torrents_only)
      form.setFieldValue("proxy_hostname_lookup", preferences.proxy_hostname_lookup)
    }
  }, [preferences, form])

  if (isLoading || !preferences) {
    return <div className="flex items-center justify-center py-8">{tr("proxySettingsForm.loading")}</div>
  }

  const getProxyTypeLabel = (value: number | string) => {
    // Handle both number and string values for compatibility
    const numValue = typeof value === "string" ? parseInt(value) : value
    switch (numValue) {
      case 0: return tr("proxySettingsForm.proxyTypes.none")
      case 1: return tr("proxySettingsForm.proxyTypes.socks4")
      case 2: return tr("proxySettingsForm.proxyTypes.socks5")
      case 3: return tr("proxySettingsForm.proxyTypes.http")
      default: return tr("proxySettingsForm.proxyTypes.none")
    }
  }

  const getProxyTypeValue = () => {
    const currentValue = form.getFieldValue("proxy_type")
    if (typeof currentValue === "string") {
      return currentValue
    }
    return currentValue.toString()
  }

  const isProxyEnabled = () => {
    const proxyType = form.getFieldValue("proxy_type")
    const numValue = typeof proxyType === "string" ? parseInt(proxyType) : proxyType
    return numValue > 0
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      {/* Proxy Type Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4" />
          <h3 className="text-lg font-medium">{tr("proxySettingsForm.sections.configuration")}</h3>
        </div>

        <form.Field name="proxy_type">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">{tr("proxySettingsForm.fields.proxyType")}</Label>
              <Select
                value={getProxyTypeValue()}
                onValueChange={(value) => {
                  const numValue = parseInt(value)
                  field.handleChange(numValue)
                  // Clear proxy settings when disabled
                  if (numValue === 0) {
                    form.setFieldValue("proxy_ip", "")
                    form.setFieldValue("proxy_port", 8080)
                    form.setFieldValue("proxy_username", "")
                    form.setFieldValue("proxy_password", "")
                    form.setFieldValue("proxy_auth_enabled", false)
                  }
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">{getProxyTypeLabel(0)}</SelectItem>
                  <SelectItem value="1">{getProxyTypeLabel(1)}</SelectItem>
                  <SelectItem value="2">{getProxyTypeLabel(2)}</SelectItem>
                  <SelectItem value="3">{getProxyTypeLabel(3)}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {tr("proxySettingsForm.fields.proxyTypeDescription")}
              </p>
            </div>
          )}
        </form.Field>
      </div>

      {/* Proxy Server Details */}
      {isProxyEnabled() && (
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4" />
            <h3 className="text-lg font-medium">{tr("proxySettingsForm.sections.server")}</h3>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <form.Field name="proxy_ip">
              {(field) => (
                <div className="space-y-2 md:col-span-2">
                  <Label htmlFor="proxy_ip">{tr("proxySettingsForm.fields.proxyServer")}</Label>
                  <Input
                    id="proxy_ip"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={tr("proxySettingsForm.placeholders.proxyServer")}
                    className={incognitoMode ? "blur-sm select-none" : ""}
                  />
                  <p className="text-xs text-muted-foreground">
                    {tr("proxySettingsForm.fields.proxyServerDescription")}
                  </p>
                </div>
              )}
            </form.Field>

            <form.Field name="proxy_port">
              {(field) => (
                <NumberInput
                  label={tr("proxySettingsForm.fields.port")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  max={65535}
                  description={tr("proxySettingsForm.fields.portDescription")}
                />
              )}
            </form.Field>
          </div>
        </div>
      )}

      {/* Authentication */}
      {isProxyEnabled() && (
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Lock className="h-4 w-4" />
            <h3 className="text-lg font-medium">{tr("proxySettingsForm.sections.authentication")}</h3>
          </div>

          <form.Field name="proxy_auth_enabled">
            {(field) => (
              <SwitchSetting
                label={tr("proxySettingsForm.fields.useAuthentication")}
                description={tr("proxySettingsForm.fields.useAuthenticationDescription")}
                checked={field.state.value}
                onChange={(checked) => {
                  field.handleChange(checked)
                  // Clear credentials when disabled
                  if (!checked) {
                    form.setFieldValue("proxy_username", "")
                    form.setFieldValue("proxy_password", "")
                  }
                }}
              />
            )}
          </form.Field>

          {form.getFieldValue("proxy_auth_enabled") && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <form.Field name="proxy_username">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="proxy_username">{tr("proxySettingsForm.fields.username")}</Label>
                    <Input
                      id="proxy_username"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={tr("proxySettingsForm.placeholders.username")}
                      autoComplete="username"
                      className={incognitoMode ? "blur-sm select-none" : ""}
                    />
                  </div>
                )}
              </form.Field>

              <form.Field name="proxy_password">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="proxy_password">{tr("proxySettingsForm.fields.password")}</Label>
                    <Input
                      id="proxy_password"
                      type="password"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={tr("proxySettingsForm.placeholders.password")}
                      autoComplete="current-password"
                    />
                  </div>
                )}
              </form.Field>
            </div>
          )}
        </div>
      )}

      {/* Proxy Options */}
      {isProxyEnabled() && (
        <div className="space-y-4">
          <h3 className="text-lg font-medium">{tr("proxySettingsForm.sections.options")}</h3>

          <div className="space-y-4">
            <form.Field name="proxy_peer_connections">
              {(field) => (
              <SwitchSetting
                  label={tr("proxySettingsForm.fields.peerConnections")}
                  description={tr("proxySettingsForm.fields.peerConnectionsDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="proxy_torrents_only">
              {(field) => (
              <SwitchSetting
                  label={tr("proxySettingsForm.fields.torrentsOnly")}
                  description={tr("proxySettingsForm.fields.torrentsOnlyDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="proxy_hostname_lookup">
              {(field) => (
              <SwitchSetting
                  label={tr("proxySettingsForm.fields.hostnameLookups")}
                  description={tr("proxySettingsForm.fields.hostnameLookupsDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
        </div>
      )}

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || isUpdating}
            className="w-full"
          >
            {isSubmitting || isUpdating ? tr("proxySettingsForm.actions.updating") : tr("proxySettingsForm.actions.update")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}
