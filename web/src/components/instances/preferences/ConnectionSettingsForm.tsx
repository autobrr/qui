/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { NumberInputWithUnlimited } from "@/components/forms/NumberInputWithUnlimited"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useQBittorrentFieldVisibility } from "@/hooks/useQBittorrentAppInfo"
import { useForm } from "@tanstack/react-form"
import { AlertTriangle, Globe, Server, Shield, Wifi } from "lucide-react"
import React from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"

const sanitizeBtProtocol = (value: unknown): 0 | 1 | 2 => {
  const numeric = typeof value === "number" ? value : parseInt(String(value), 10)

  if (Number.isNaN(numeric)) {
    return 0
  }

  return Math.min(2, Math.max(0, numeric)) as 0 | 1 | 2
}

const sanitizeUtpTcpMixedMode = (value: unknown): 0 | 1 => {
  const numeric = typeof value === "number" ? value : parseInt(String(value), 10)
  return numeric === 1 ? 1 : 0
}

const scheduleMicrotask = (callback: () => void) => {
  if (typeof queueMicrotask === "function") {
    queueMicrotask(callback)
  } else {
    setTimeout(callback, 0)
  }
}

interface ConnectionSettingsFormProps {
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
    <div className="flex items-center gap-3">
      <Switch checked={checked} onCheckedChange={onChange} />
      <div className="space-y-0.5">
        <Label className="text-sm font-medium">{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
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

export function ConnectionSettingsForm({ instanceId, onSuccess }: ConnectionSettingsFormProps) {
  const { t } = useTranslation()
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const fieldVisibility = useQBittorrentFieldVisibility(instanceId)

  const form = useForm({
    defaultValues: {
      listen_port: 0,
      random_port: false,
      upnp: false,
      upnp_lease_duration: 0,
      bittorrent_protocol: 0,
      utp_tcp_mixed_mode: 0,
      current_network_interface: "",
      current_interface_address: "",
      reannounce_when_address_changed: false,
      max_connec: 0,
      max_connec_per_torrent: 0,
      max_uploads: 0,
      max_uploads_per_torrent: 0,
      enable_multi_connections_from_same_ip: false,
      outgoing_ports_min: 0,
      outgoing_ports_max: 0,
      ip_filter_enabled: false,
      ip_filter_path: "",
      ip_filter_trackers: false,
      banned_IPs: "",
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success(t("instancePreferences.content.connectionSettings.notifications.saveSuccess"))
        onSuccess?.()
      } catch (error) {
        toast.error(t("instancePreferences.content.connectionSettings.notifications.saveError"))
        console.error("Failed to update connection settings:", error)
      }
    },
  })

  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("listen_port", preferences.listen_port)
      form.setFieldValue("random_port", preferences.random_port)
      form.setFieldValue("upnp", preferences.upnp)
      form.setFieldValue("upnp_lease_duration", preferences.upnp_lease_duration)
      form.setFieldValue("bittorrent_protocol", sanitizeBtProtocol(preferences.bittorrent_protocol))
      form.setFieldValue("utp_tcp_mixed_mode", sanitizeUtpTcpMixedMode(preferences.utp_tcp_mixed_mode))
      form.setFieldValue("current_network_interface", preferences.current_network_interface)
      form.setFieldValue("current_interface_address", preferences.current_interface_address)
      form.setFieldValue("reannounce_when_address_changed", preferences.reannounce_when_address_changed)
      form.setFieldValue("max_connec", preferences.max_connec)
      form.setFieldValue("max_connec_per_torrent", preferences.max_connec_per_torrent)
      form.setFieldValue("max_uploads", preferences.max_uploads)
      form.setFieldValue("max_uploads_per_torrent", preferences.max_uploads_per_torrent)
      form.setFieldValue("enable_multi_connections_from_same_ip", preferences.enable_multi_connections_from_same_ip)
      form.setFieldValue("outgoing_ports_min", preferences.outgoing_ports_min)
      form.setFieldValue("outgoing_ports_max", preferences.outgoing_ports_max)
      form.setFieldValue("ip_filter_enabled", preferences.ip_filter_enabled)
      form.setFieldValue("ip_filter_path", preferences.ip_filter_path)
      form.setFieldValue("ip_filter_trackers", preferences.ip_filter_trackers)
      form.setFieldValue("banned_IPs", preferences.banned_IPs)
    }
  }, [preferences, form])

  if (isLoading || !preferences) {
    return <div className="flex items-center justify-center py-8">{t("instancePreferences.content.connectionSettings.loading")}</div>
  }

  const getBittorrentProtocolLabel = (value: number) => {
    switch (value) {
      case 0: return t("instancePreferences.content.connectionSettings.protocol.tcp_and_utp")
      case 1: return t("instancePreferences.content.connectionSettings.protocol.tcp")
      case 2: return t("instancePreferences.content.connectionSettings.protocol.utp")
      default: return t("instancePreferences.content.connectionSettings.protocol.tcp_and_utp")
    }
  }

  const getUtpTcpMixedModeLabel = (value: number) => {
    switch (value) {
      case 0: return t("instancePreferences.content.connectionSettings.protocol.preferTcp")
      case 1: return t("instancePreferences.content.connectionSettings.protocol.peerProportional")
      default: return t("instancePreferences.content.connectionSettings.protocol.preferTcp")
    }
  }


  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      {fieldVisibility.isUnknown && (
        <Alert className="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-400/70 dark:bg-amber-950/50">
          <AlertTriangle className="h-4 w-4 text-amber-600" />
          <AlertTitle>{t("instancePreferences.content.connectionSettings.warnings.limitedDetails.title")}</AlertTitle>
          <AlertDescription>
            {t("instancePreferences.content.connectionSettings.warnings.limitedDetails.description")}
          </AlertDescription>
        </Alert>
      )}

      {/* Listening Port Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4" />
          <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.port.title")}</h3>
        </div>

        <div className="space-y-4">
          {/* Input boxes row */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field
              name="listen_port"
              validators={{
                onChange: ({ value }) => {
                  if (value < 0 || value > 65535) {
                    return t("instancePreferences.content.connectionSettings.port.validation.portRange")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("instancePreferences.content.connectionSettings.port.portRange")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={65535}
                    description={t("instancePreferences.content.connectionSettings.port.portRangeDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            {fieldVisibility.showUpnpLeaseField && (
              <form.Field name="upnp_lease_duration">
                {(field) => (
                  <NumberInput
                    label={t("instancePreferences.content.connectionSettings.port.upnpLease")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    description={t("instancePreferences.content.connectionSettings.port.upnpLeaseDescription")}
                  />
                )}
              </form.Field>
            )}
          </div>

          {/* Toggles row */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field name="random_port">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.connectionSettings.port.randomPort")}
                  description={t("instancePreferences.content.connectionSettings.port.randomPortDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="upnp">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.connectionSettings.port.upnp")}
                  description={t("instancePreferences.content.connectionSettings.port.upnpDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
        </div>
      </div>

      {/* Protocol Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Wifi className="h-4 w-4" />
          <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.protocol.title")}</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="bittorrent_protocol">
            {(field) => {
              const sanitizedValue = sanitizeBtProtocol(field.state.value)

              if (field.state.value !== sanitizedValue) {
                scheduleMicrotask(() => field.handleChange(sanitizedValue))
              }

              return (
                <div className="space-y-2">
                  <Label className="text-sm font-medium">{t("instancePreferences.content.connectionSettings.protocol.bittorrentProtocol")}</Label>
                  <Select
                    value={sanitizedValue.toString()}
                    onValueChange={(value) => {
                      const parsed = parseInt(value, 10)

                      if (!Number.isNaN(parsed)) {
                        field.handleChange(sanitizeBtProtocol(parsed))
                      }
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">{getBittorrentProtocolLabel(0)}</SelectItem>
                      <SelectItem value="1">{getBittorrentProtocolLabel(1)}</SelectItem>
                      <SelectItem value="2">{getBittorrentProtocolLabel(2)}</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    {t("instancePreferences.content.connectionSettings.protocol.bittorrentProtocolDescription")}
                  </p>
                </div>
              )
            }}
          </form.Field>

          <form.Field name="utp_tcp_mixed_mode">
            {(field) => {
              const sanitizedValue = sanitizeUtpTcpMixedMode(field.state.value)

              // Coerce the form state whenever we fall back to the sanitized value
              if (field.state.value !== sanitizedValue) {
                scheduleMicrotask(() => field.handleChange(sanitizedValue))
              }

              return (
                <div className="space-y-2">
                  <Label className="text-sm font-medium">{t("instancePreferences.content.connectionSettings.protocol.mixedMode")}</Label>
                  <Select
                    value={sanitizedValue.toString()}
                    onValueChange={(value) => {
                      const parsed = parseInt(value, 10)

                      if (!Number.isNaN(parsed)) {
                        field.handleChange(sanitizeUtpTcpMixedMode(parsed))
                      }
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={t("instancePreferences.content.connectionSettings.protocol.mixedModePlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">{getUtpTcpMixedModeLabel(0)}</SelectItem>
                      <SelectItem value="1">{getUtpTcpMixedModeLabel(1)}</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    {t("instancePreferences.content.connectionSettings.protocol.mixedModeDescription")}
                  </p>
                </div>
              )
            }}
          </form.Field>
        </div>

      </div>

      {/* Network Interface Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4" />
          <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.networkInterface.title")}</h3>
        </div>

        <div className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field name="current_network_interface">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="network_interface">{t("instancePreferences.content.connectionSettings.networkInterface.interface")}</Label>
                  <Input
                    id="network_interface"
                    value={field.state.value || t("instancePreferences.content.connectionSettings.networkInterface.autoDetect")}
                    readOnly
                    className="bg-muted"
                    disabled
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("instancePreferences.content.connectionSettings.networkInterface.interfaceDescription")}
                  </p>
                </div>
              )}
            </form.Field>

            <form.Field name="current_interface_address">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="interface_address">{t("instancePreferences.content.connectionSettings.networkInterface.address")}</Label>
                  <Input
                    id="interface_address"
                    value={field.state.value || t("instancePreferences.content.connectionSettings.networkInterface.autoDetect")}
                    readOnly
                    disabled
                    className="bg-muted"
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("instancePreferences.content.connectionSettings.networkInterface.addressDescription")}
                  </p>
                </div>
              )}
            </form.Field>
          </div>

          <form.Field name="reannounce_when_address_changed">
            {(field) => (
              <SwitchSetting
                label={t("instancePreferences.content.connectionSettings.networkInterface.reannounce")}
                description={t("instancePreferences.content.connectionSettings.networkInterface.reannounceDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* Connection Limits Section */}
      <div className="space-y-4">
        <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.limits.title")}</h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field
            name="max_connec"
            validators={{
              onChange: ({ value }) => {
                if (value !== -1 && value !== 0 && value <= 0) {
                  return t("instancePreferences.content.connectionSettings.limits.validation.globalMax")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.connectionSettings.limits.globalMax")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  allowUnlimited={true}
                  description={t("instancePreferences.content.connectionSettings.limits.globalMaxDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="max_connec_per_torrent"
            validators={{
              onChange: ({ value }) => {
                if (value !== -1 && value !== 0 && value <= 0) {
                  return t("instancePreferences.content.connectionSettings.limits.validation.perTorrentMax")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.connectionSettings.limits.perTorrentMax")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  allowUnlimited={true}
                  description={t("instancePreferences.content.connectionSettings.limits.perTorrentMaxDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="max_uploads"
            validators={{
              onChange: ({ value }) => {
                if (value !== -1 && value !== 0 && value <= 0) {
                  return t("instancePreferences.content.connectionSettings.limits.validation.globalUploadSlots")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.connectionSettings.limits.globalUploadSlots")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  allowUnlimited={true}
                  description={t("instancePreferences.content.connectionSettings.limits.globalUploadSlotsDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="max_uploads_per_torrent"
            validators={{
              onChange: ({ value }) => {
                if (value !== -1 && value !== 0 && value <= 0) {
                  return t("instancePreferences.content.connectionSettings.limits.validation.perTorrentUploadSlots")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInputWithUnlimited
                  label={t("instancePreferences.content.connectionSettings.limits.perTorrentUploadSlots")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  allowUnlimited={true}
                  description={t("instancePreferences.content.connectionSettings.limits.perTorrentUploadSlotsDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>
        </div>

        <form.Field name="enable_multi_connections_from_same_ip">
          {(field) => (
            <SwitchSetting
              label={t("instancePreferences.content.connectionSettings.limits.allowMultipleConnections")}
              description={t("instancePreferences.content.connectionSettings.limits.allowMultipleConnectionsDescription")}
              checked={field.state.value}
              onChange={(checked) => field.handleChange(checked)}
            />
          )}
        </form.Field>
      </div>

      {/* Outgoing Ports Section */}
      <div className="space-y-4">
        <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.outgoingPorts.title")}</h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field
            name="outgoing_ports_min"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 65535) {
                  return t("instancePreferences.content.connectionSettings.outgoingPorts.validation.min")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={t("instancePreferences.content.connectionSettings.outgoingPorts.min")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={65535}
                  description={t("instancePreferences.content.connectionSettings.outgoingPorts.minDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="outgoing_ports_max"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 65535) {
                  return t("instancePreferences.content.connectionSettings.outgoingPorts.validation.max")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={t("instancePreferences.content.connectionSettings.outgoingPorts.max")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={65535}
                  description={t("instancePreferences.content.connectionSettings.outgoingPorts.maxDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>
        </div>
      </div>

      {/* IP Filtering Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4" />
          <h3 className="text-lg font-medium">{t("instancePreferences.content.connectionSettings.ipFiltering.title")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="ip_filter_enabled">
            {(field) => (
              <SwitchSetting
                label={t("instancePreferences.content.connectionSettings.ipFiltering.enable")}
                description={t("instancePreferences.content.connectionSettings.ipFiltering.enableDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="ip_filter_path">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="ip_filter_path">{t("instancePreferences.content.connectionSettings.ipFiltering.path")}</Label>
                <Input
                  id="ip_filter_path"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("instancePreferences.content.connectionSettings.ipFiltering.pathPlaceholder")}
                  disabled={!form.state.values.ip_filter_enabled}
                />
                <p className="text-xs text-muted-foreground">
                  {t("instancePreferences.content.connectionSettings.ipFiltering.pathDescription")}
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="ip_filter_trackers">
            {(field) => (
              <SwitchSetting
                label={t("instancePreferences.content.connectionSettings.ipFiltering.applyToTrackers")}
                description={t("instancePreferences.content.connectionSettings.ipFiltering.applyToTrackersDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="banned_IPs">
            {(field) => (
              <div className="space-y-2">
                <Label>{t("instancePreferences.content.connectionSettings.ipFiltering.bannedIPs")}</Label>
                <Textarea
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("instancePreferences.content.connectionSettings.ipFiltering.bannedIPsPlaceholder")}
                  className="min-h-[100px] font-mono text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  {t("instancePreferences.content.connectionSettings.ipFiltering.bannedIPsDescription")}
                </p>
              </div>
            )}
          </form.Field>
        </div>
      </div>

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || isUpdating}
            className="w-full"
          >
            {isSubmitting || isUpdating ? t("instancePreferences.content.connectionSettings.savingButton") : t("instancePreferences.content.connectionSettings.saveButton")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}