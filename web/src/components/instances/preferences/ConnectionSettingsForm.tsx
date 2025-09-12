/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { useForm } from "@tanstack/react-form"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { Wifi, Server, Globe, Shield } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { NumberInputWithUnlimited } from "@/components/forms/NumberInputWithUnlimited"
import { toast } from "sonner"

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
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)

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
        toast.success("Connection settings updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update connection settings")
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
      form.setFieldValue("bittorrent_protocol", preferences.bittorrent_protocol)
      form.setFieldValue("utp_tcp_mixed_mode", preferences.utp_tcp_mixed_mode)
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
    return <div className="flex items-center justify-center py-8">Loading connection settings...</div>
  }

  const getBittorrentProtocolLabel = (value: number) => {
    switch (value) {
      case 0: return "TCP and μTP"
      case 1: return "TCP"
      case 2: return "μTP"
      default: return "TCP and μTP"
    }
  }

  const getUtpTcpMixedModeLabel = (value: number) => {
    switch (value) {
      case 0: return "Prefer TCP"
      case 1: return "Peer proportional"
      default: return "Prefer TCP"
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
      {/* Listening Port Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4" />
          <h3 className="text-lg font-medium">Listening Port</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="listen_port">
            {(field) => (
              <NumberInput
                label="Port for incoming connections"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                min={1024}
                max={65535}
                description="Port used for incoming BitTorrent connections"
              />
            )}
          </form.Field>

          <form.Field name="random_port">
            {(field) => (
              <SwitchSetting
                label="Use random port on each startup"
                description="Randomly select a port when qBittorrent starts"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="upnp">
            {(field) => (
              <SwitchSetting
                label="Enable UPnP/NAT-PMP port forwarding"
                description="Automatically forward port through your router"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="upnp_lease_duration">
            {(field) => (
              <NumberInput
                label="UPnP lease duration (0 = permanent)"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                min={0}
                description="Duration in minutes for UPnP lease (0 for permanent)"
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* Protocol Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Wifi className="h-4 w-4" />
          <h3 className="text-lg font-medium">Protocol Settings</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="bittorrent_protocol">
            {(field) => (
              <div className="space-y-2">
                <Label className="text-sm font-medium">BitTorrent Protocol</Label>
                <Select
                  value={field.state.value.toString()}
                  onValueChange={(value) => field.handleChange(parseInt(value))}
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
                  Protocol to use for peer connections
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="utp_tcp_mixed_mode">
            {(field) => (
              <div className="space-y-2">
                <Label className="text-sm font-medium">μTP-TCP Mixed Mode</Label>
                <Select
                  value={field.state.value.toString()}
                  onValueChange={(value) => field.handleChange(parseInt(value))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">{getUtpTcpMixedModeLabel(0)}</SelectItem>
                    <SelectItem value="1">{getUtpTcpMixedModeLabel(1)}</SelectItem>
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">
                  How to handle mixed μTP/TCP connections
                </p>
              </div>
            )}
          </form.Field>
        </div>

      </div>

      {/* Network Interface Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4" />
          <h3 className="text-lg font-medium">Network Interface</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="current_network_interface">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="network_interface">Network Interface (Read-Only)</Label>
                <Input
                  id="network_interface"
                  value={field.state.value || "Auto-detect"}
                  readOnly
                  className="bg-muted"
                  disabled
                />
                <p className="text-xs text-muted-foreground">
                  Currently active network interface. Configuration requires missing API endpoints.
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="current_interface_address">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="interface_address">Interface IP Address (Read-Only)</Label>
                <Input
                  id="interface_address"
                  value={field.state.value || "Auto-detect"}
                  readOnly
                  disabled
                  className="bg-muted"
                />
                <p className="text-xs text-muted-foreground">
                  IP address of the current interface. Configuration requires missing API endpoints.
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="reannounce_when_address_changed">
            {(field) => (
              <SwitchSetting
                label="Re-announce to trackers when IP address changes"
                description="Automatically re-announce when your IP address changes"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* Connection Limits Section */}
      <div className="space-y-4">
        <h3 className="text-lg font-medium">Connection Limits</h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="max_connec">
            {(field) => (
              <NumberInputWithUnlimited
                label="Global maximum connections"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                allowUnlimited={true}
                description="Maximum connections across all torrents"
              />
            )}
          </form.Field>

          <form.Field name="max_connec_per_torrent">
            {(field) => (
              <NumberInputWithUnlimited
                label="Maximum connections per torrent"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                allowUnlimited={true}
                description="Maximum connections per individual torrent"
              />
            )}
          </form.Field>

          <form.Field name="max_uploads">
            {(field) => (
              <NumberInputWithUnlimited
                label="Global maximum upload slots"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                allowUnlimited={true}
                description="Maximum upload slots across all torrents"
              />
            )}
          </form.Field>

          <form.Field name="max_uploads_per_torrent">
            {(field) => (
              <NumberInputWithUnlimited
                label="Maximum upload slots per torrent"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                allowUnlimited={true}
                description="Maximum upload slots per individual torrent"
              />
            )}
          </form.Field>
        </div>

        <form.Field name="enable_multi_connections_from_same_ip">
          {(field) => (
            <SwitchSetting
              label="Allow multiple connections from the same IP address"
              description="Enable connections from multiple peers behind the same NAT"
              checked={field.state.value}
              onChange={(checked) => field.handleChange(checked)}
            />
          )}
        </form.Field>
      </div>

      {/* Outgoing Ports Section */}
      <div className="space-y-4">
        <h3 className="text-lg font-medium">Outgoing Ports</h3>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <form.Field name="outgoing_ports_min">
            {(field) => (
              <NumberInput
                label="Outgoing ports (Min)"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                min={0}
                max={65535}
                description="Minimum port for outgoing connections (0 = no limit)"
              />
            )}
          </form.Field>

          <form.Field name="outgoing_ports_max">
            {(field) => (
              <NumberInput
                label="Outgoing ports (Max)"
                value={field.state.value}
                onChange={(value) => field.handleChange(value)}
                min={0}
                max={65535}
                description="Maximum port for outgoing connections (0 = no limit)"
              />
            )}
          </form.Field>
        </div>
      </div>

      {/* IP Filtering Section */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4" />
          <h3 className="text-lg font-medium">IP Filtering</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="ip_filter_enabled">
            {(field) => (
              <SwitchSetting
                label="Enable IP filtering"
                description="Filter specific IP addresses from connecting"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="ip_filter_path">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="ip_filter_path">IP filter file path</Label>
                <Input
                  id="ip_filter_path"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder="/path/to/filter.dat"
                  disabled={!form.state.values.ip_filter_enabled}
                />
                <p className="text-xs text-muted-foreground">
                  Path to IP filter file (.dat, .p2p, .p2b formats)
                </p>
              </div>
            )}
          </form.Field>

          <form.Field name="ip_filter_trackers">
            {(field) => (
              <SwitchSetting
                label="Apply IP filter to trackers"
                description="Also filter tracker connections based on IP filter rules"
                checked={field.state.value}
                onChange={(checked) => field.handleChange(checked)}
              />
            )}
          </form.Field>

          <form.Field name="banned_IPs">
            {(field) => (
              <div className="space-y-2">
                <Label>Manually banned IP addresses</Label>
                <Textarea
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={`Enter IP addresses to ban (one per line):
192.168.1.100
10.0.0.50
2001:db8::1`}
                  className="min-h-[100px] font-mono text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  Add IP addresses to permanently ban from connecting (one per line)
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
            {isSubmitting || isUpdating ? "Updating..." : "Update Connection Settings"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}