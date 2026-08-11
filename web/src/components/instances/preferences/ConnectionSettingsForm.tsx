/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { useIncognitoMode } from "@/lib/incognito"
import { useForm } from "@tanstack/react-form"
import { AlertTriangle, Globe, Server, Shield, Wifi } from "lucide-react"
import React from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PreferencesFormShell } from "./PreferencesFormShell"

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
  const switchId = React.useId()
  const descriptionId = description ? `${switchId}-desc` : undefined

  return (
    <label
      htmlFor={switchId}
      className="flex items-center gap-3 cursor-pointer"
    >
      <Switch
        id={switchId}
        checked={checked}
        onCheckedChange={onChange}
        aria-describedby={descriptionId}
      />
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{label}</span>
        {description && (
          <p id={descriptionId} className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </label>
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
  const inputId = React.useId()
  const descriptionId = description ? `${inputId}-desc` : undefined

  return (
    <div className="space-y-2">
      <Label htmlFor={inputId} className="text-sm font-medium">{label}</Label>
      {description && (
        <p id={descriptionId} className="text-xs text-muted-foreground">{description}</p>
      )}
      <Input
        id={inputId}
        type="number"
        min={min}
        max={max}
        value={value || ""}
        onChange={(e) => {
          const val = parseInt(e.target.value)
          onChange(isNaN(val) ? 0 : val)
        }}
        placeholder={placeholder}
        aria-describedby={descriptionId}
      />
    </div>
  )
}

export function ConnectionSettingsForm({ instanceId, onSuccess }: ConnectionSettingsFormProps) {
  const { t } = useTranslation("instances")
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const fieldVisibility = useQBittorrentFieldVisibility(instanceId)
  const [incognitoMode] = useIncognitoMode()

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
        await updatePreferences(value)
        toast.success(t("preferences.connectionSettings.toast.success"))
        onSuccess?.()
      } catch (error) {
        toast.error(t("preferences.connectionSettings.toast.error"))
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
  // eslint-disable-next-line react-hooks/exhaustive-deps -- form reference is stable, only sync on preferences change
  }, [preferences])

  if (isLoading || !preferences) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{t("preferences.connectionSettings.loading")}</p>
      </div>
    )
  }

  const getBittorrentProtocolLabel = (value: number) => {
    switch (value) {
      case 1: return t("preferences.connectionSettings.protocolTcp")
      case 2: return t("preferences.connectionSettings.protocolUtp")
      default: return t("preferences.connectionSettings.protocolTcpUtp")
    }
  }

  const getUtpTcpMixedModeLabel = (value: number) => {
    switch (value) {
      case 1: return t("preferences.connectionSettings.peerProportional")
      default: return t("preferences.connectionSettings.preferTcp")
    }
  }


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
              {isSubmitting || isUpdating ? t("preferences.common.saving") : t("preferences.common.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      )}
    >
      <div className="space-y-6">
        {fieldVisibility.isUnknown && (
          <Alert className="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-400/70 dark:bg-amber-950/50">
            <AlertTriangle className="h-4 w-4 text-amber-600" />
            <AlertTitle>{t("preferences.connectionSettings.versionWarningTitle")}</AlertTitle>
            <AlertDescription>
              {t("preferences.connectionSettings.versionWarningDescription")}
            </AlertDescription>
          </Alert>
        )}

        {/* Listening Port Section */}
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4" />
            <h3 className="text-lg font-medium">{t("preferences.connectionSettings.listeningPort")}</h3>
          </div>

          <div className="space-y-4">
            {/* Input boxes row */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <form.Field
                name="listen_port"
                validators={{
                  onChange: ({ value }) => {
                    if (value < 0 || value > 65535) {
                      return t("preferences.connectionSettings.validation.listenPort")
                    }
                    return undefined
                  },
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <NumberInput
                      label={t("preferences.connectionSettings.portForIncoming")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={0}
                      max={65535}
                      description={t("preferences.connectionSettings.portDescription")}
                    />
                    {field.state.meta.errors.length > 0 && (
                      <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                    )}
                  </div>
                )}
              </form.Field>

              {fieldVisibility.showUpnpLeaseField && (
                <form.Field name="upnp_lease_duration">
                  {(field) => (
                    <NumberInput
                      label={t("preferences.connectionSettings.upnpLeaseDuration")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={0}
                      description={t("preferences.connectionSettings.upnpLeaseDurationDescription")}
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
                    label={t("preferences.connectionSettings.useRandomPort")}
                    description={t("preferences.connectionSettings.useRandomPortDescription")}
                    checked={field.state.value}
                    onChange={(checked) => field.handleChange(checked)}
                  />
                )}
              </form.Field>

              <form.Field name="upnp">
                {(field) => (
                  <SwitchSetting
                    label={t("preferences.connectionSettings.enableUpnp")}
                    description={t("preferences.connectionSettings.enableUpnpDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.connectionSettings.protocolSettings")}</h3>
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
                    <Label className="text-sm font-medium">{t("preferences.connectionSettings.bittorrentProtocol")}</Label>
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
                      {t("preferences.connectionSettings.protocolDescription")}
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
                    <Label className="text-sm font-medium">{t("preferences.connectionSettings.utpTcpMixedMode")}</Label>
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
                        <SelectValue placeholder={t("preferences.connectionSettings.selectMode")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="0">{getUtpTcpMixedModeLabel(0)}</SelectItem>
                        <SelectItem value="1">{getUtpTcpMixedModeLabel(1)}</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      {t("preferences.connectionSettings.utpTcpMixedModeDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.connectionSettings.networkInterface")}</h3>
          </div>

          <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <form.Field name="current_network_interface">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="network_interface">{t("preferences.connectionSettings.networkInterfaceReadOnly")}</Label>
                    <Input
                      id="network_interface"
                      value={field.state.value || t("preferences.connectionSettings.autoDetect")}
                      readOnly
                      className={incognitoMode ? "bg-muted blur-sm select-none" : "bg-muted"}
                      disabled
                    />
                    <p className="text-xs text-muted-foreground">
                      {t("preferences.connectionSettings.networkInterfaceDescription")}
                    </p>
                  </div>
                )}
              </form.Field>

              <form.Field name="current_interface_address">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="interface_address">{t("preferences.connectionSettings.interfaceIpAddress")}</Label>
                    <Input
                      id="interface_address"
                      value={field.state.value || t("preferences.connectionSettings.autoDetect")}
                      readOnly
                      disabled
                      className={incognitoMode ? "bg-muted blur-sm select-none" : "bg-muted"}
                    />
                    <p className="text-xs text-muted-foreground">
                      {t("preferences.connectionSettings.interfaceIpAddressDescription")}
                    </p>
                  </div>
                )}
              </form.Field>
            </div>

            <form.Field name="reannounce_when_address_changed">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.connectionSettings.reannounceOnIpChange")}
                  description={t("preferences.connectionSettings.reannounceOnIpChangeDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
        </div>

        {/* Connection Limits Section */}
        <div className="space-y-4">
          <h3 className="text-lg font-medium">{t("preferences.connectionSettings.connectionLimits")}</h3>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field
              name="max_connec"
              validators={{
                onChange: ({ value }) => {
                  if (value !== -1 && value !== 0 && value <= 0) {
                    return t("preferences.connectionSettings.validation.maxConnections")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.connectionSettings.globalMaxConnections")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    allowUnlimited={true}
                    description={t("preferences.connectionSettings.globalMaxConnectionsDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="max_connec_per_torrent"
              validators={{
                onChange: ({ value }) => {
                  if (value !== -1 && value !== 0 && value <= 0) {
                    return t("preferences.connectionSettings.validation.maxConnectionsPerTorrent")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.connectionSettings.maxConnectionsPerTorrent")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    allowUnlimited={true}
                    description={t("preferences.connectionSettings.maxConnectionsPerTorrentDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="max_uploads"
              validators={{
                onChange: ({ value }) => {
                  if (value !== -1 && value !== 0 && value <= 0) {
                    return t("preferences.connectionSettings.validation.maxUploadSlots")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.connectionSettings.globalMaxUploadSlots")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    allowUnlimited={true}
                    description={t("preferences.connectionSettings.globalMaxUploadSlotsDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="max_uploads_per_torrent"
              validators={{
                onChange: ({ value }) => {
                  if (value !== -1 && value !== 0 && value <= 0) {
                    return t("preferences.connectionSettings.validation.maxUploadSlotsPerTorrent")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInputWithUnlimited
                    label={t("preferences.connectionSettings.maxUploadSlotsPerTorrent")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    allowUnlimited={true}
                    description={t("preferences.connectionSettings.maxUploadSlotsPerTorrentDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>
          </div>

          <form.Field name="enable_multi_connections_from_same_ip">
            {(field) => (
              <SwitchSetting
                label={t("preferences.connectionSettings.allowMultiConnections")}
                description={t("preferences.connectionSettings.allowMultiConnectionsDescription")}
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>

        {/* Outgoing Ports Section */}
        <div className="space-y-4">
          <h3 className="text-lg font-medium">{t("preferences.connectionSettings.outgoingPorts")}</h3>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <form.Field
              name="outgoing_ports_min"
              validators={{
                onChange: ({ value }) => {
                  if (value < 0 || value > 65535) {
                    return t("preferences.connectionSettings.validation.outgoingPortsMin")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("preferences.connectionSettings.outgoingPortsMin")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={65535}
                    description={t("preferences.connectionSettings.outgoingPortsMinDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="outgoing_ports_max"
              validators={{
                onChange: ({ value }) => {
                  if (value < 0 || value > 65535) {
                    return t("preferences.connectionSettings.validation.outgoingPortsMax")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("preferences.connectionSettings.outgoingPortsMax")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={65535}
                    description={t("preferences.connectionSettings.outgoingPortsMaxDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
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
            <h3 className="text-lg font-medium">{t("preferences.connectionSettings.ipFiltering")}</h3>
          </div>

          <div className="space-y-4">
            <form.Field name="ip_filter_enabled">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.connectionSettings.enableIpFiltering")}
                  description={t("preferences.connectionSettings.enableIpFilteringDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="ip_filter_path">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="ip_filter_path">{t("preferences.connectionSettings.ipFilterPath")}</Label>
                  <Input
                    id="ip_filter_path"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("preferences.connectionSettings.ipFilterPathPlaceholder")}
                    disabled={!form.state.values.ip_filter_enabled}
                    className={incognitoMode ? "blur-sm select-none" : ""}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("preferences.connectionSettings.ipFilterPathDescription")}
                  </p>
                </div>
              )}
            </form.Field>

            <form.Field name="ip_filter_trackers">
              {(field) => (
                <SwitchSetting
                  label={t("preferences.connectionSettings.applyIpFilterToTrackers")}
                  description={t("preferences.connectionSettings.applyIpFilterToTrackersDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="banned_IPs">
              {(field) => (
                <div className="space-y-2">
                  <Label>{t("preferences.connectionSettings.bannedIps")}</Label>
                  <Textarea
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("preferences.connectionSettings.bannedIpsPlaceholder")}
                    className={incognitoMode ? "min-h-[100px] font-mono text-sm blur-sm select-none" : "min-h-[100px] font-mono text-sm"}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("preferences.connectionSettings.bannedIpsDescription")}
                  </p>
                </div>
              )}
            </form.Field>
          </div>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
