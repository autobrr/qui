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
        toast.success("Proxy settings updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update proxy settings")
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
    return <div className="flex items-center justify-center py-8">Loading proxy settings...</div>
  }

  const getProxyTypeLabel = (value: number | string) => {
    // Handle both number and string values for compatibility
    const numValue = typeof value === "string" ? parseInt(value) : value
    switch (numValue) {
      case 0: return "None"
      case 1: return "SOCKS4"
      case 2: return "SOCKS5"
      case 3: return "HTTP"
      default: return "None"
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
          <h3 className="text-lg font-medium">Proxy Configuration</h3>
        </div>

        <form.Field name="proxy_type">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Proxy Type</Label>
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
                Select proxy type for routing connections
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
            <h3 className="text-lg font-medium">Proxy Server</h3>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <form.Field name="proxy_ip">
              {(field) => (
                <div className="space-y-2 md:col-span-2">
                  <Label htmlFor="proxy_ip">Proxy Server</Label>
                  <Input
                    id="proxy_ip"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="proxy.example.com"
                    className={incognitoMode ? "blur-sm select-none" : ""}
                  />
                  <p className="text-xs text-muted-foreground">
                    Proxy server hostname or IP address
                  </p>
                </div>
              )}
            </form.Field>

            <form.Field name="proxy_port">
              {(field) => (
                <NumberInput
                  label="Port"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  max={65535}
                  description="Proxy server port"
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
            <h3 className="text-lg font-medium">Authentication</h3>
          </div>

          <form.Field name="proxy_auth_enabled">
            {(field) => (
              <SwitchSetting
                label="Use authentication"
                description="Enable if your proxy server requires username/password"
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
                    <Label htmlFor="proxy_username">Username</Label>
                    <Input
                      id="proxy_username"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="Username"
                      autoComplete="username"
                      className={incognitoMode ? "blur-sm select-none" : ""}
                    />
                  </div>
                )}
              </form.Field>

              <form.Field name="proxy_password">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="proxy_password">Password</Label>
                    <Input
                      id="proxy_password"
                      type="password"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="Password"
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
          <h3 className="text-lg font-medium">Proxy Options</h3>

          <div className="space-y-4">
            <form.Field name="proxy_peer_connections">
              {(field) => (
                <SwitchSetting
                  label="Use proxy for peer connections"
                  description="Route BitTorrent peer connections through proxy"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="proxy_torrents_only">
              {(field) => (
                <SwitchSetting
                  label="Use proxy only for torrents"
                  description="Only use proxy for BitTorrent traffic, not for other connections"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="proxy_hostname_lookup">
              {(field) => (
                <SwitchSetting
                  label="Use proxy for hostname lookups"
                  description="Resolve hostnames through the proxy server"
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
            {isSubmitting || isUpdating ? "Updating..." : "Update Proxy Settings"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}
