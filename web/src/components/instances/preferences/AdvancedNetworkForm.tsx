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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Settings, HardDrive, Zap, Ban, Radio, AlertTriangle } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useQBittorrentFieldVisibility } from "@/hooks/useQBittorrentAppInfo"
import { toast } from "sonner"

interface AdvancedNetworkFormProps {
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
  unit,
}: {
  label: string
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  description?: string
  placeholder?: string
  unit?: string
}) {
  return (
    <div className="space-y-2">
      <Label className="text-sm font-medium">
        {label}
        {unit && <span className="text-muted-foreground ml-1">({unit})</span>}
      </Label>
      {description && (
        <p className="text-xs text-muted-foreground">{description}</p>
      )}
      <Input
        type="number"
        min={min}
        max={max}
        value={value ?? ""}
        onChange={(e) => {
          const val = parseInt(e.target.value)
          onChange(isNaN(val) ? 0 : val)
        }}
        placeholder={placeholder}
      />
    </div>
  )
}

export function AdvancedNetworkForm({ instanceId, onSuccess }: AdvancedNetworkFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const fieldVisibility = useQBittorrentFieldVisibility(instanceId)

  const form = useForm({
    defaultValues: {
      // Tracker settings
      announce_ip: "",

      // Performance settings
      limit_lan_peers: false,
      limit_tcp_overhead: false,
      limit_utp_rate: false,
      peer_tos: 0,
      socket_backlog_size: 0,
      send_buffer_watermark: 0,
      send_buffer_low_watermark: 0,
      send_buffer_watermark_factor: 0,
      max_concurrent_http_announces: 0,
      request_queue_size: 0,
      stop_tracker_timeout: 0,

      // Disk I/O settings
      async_io_threads: 0,
      hashing_threads: 0,
      file_pool_size: 0,
      disk_cache: 0,
      disk_cache_ttl: 0,
      disk_queue_size: 0,
      disk_io_type: 0,
      disk_io_read_mode: 0,
      disk_io_write_mode: 0,
      checking_memory_use: 0,
      memory_working_set_limit: 0,
      enable_coalesce_read_write: false,

      // Peer behavior
      peer_turnover: 0,
      peer_turnover_cutoff: 0,
      peer_turnover_interval: 0,

      // Security & filtering
      block_peers_on_privileged_ports: false,
    },
    onSubmit: async ({ value }) => {
      try {
        updatePreferences(value)
        toast.success("Advanced network settings updated successfully")
        onSuccess?.()
      } catch (error) {
        toast.error("Failed to update advanced network settings")
        console.error("Failed to update advanced network settings:", error)
      }
    },
  })

  React.useEffect(() => {
    if (preferences) {
      // Tracker settings
      form.setFieldValue("announce_ip", preferences.announce_ip)

      // Performance settings
      form.setFieldValue("limit_lan_peers", preferences.limit_lan_peers)
      form.setFieldValue("limit_tcp_overhead", preferences.limit_tcp_overhead)
      form.setFieldValue("limit_utp_rate", preferences.limit_utp_rate)
      form.setFieldValue("peer_tos", preferences.peer_tos)
      form.setFieldValue("socket_backlog_size", preferences.socket_backlog_size)
      form.setFieldValue("send_buffer_watermark", preferences.send_buffer_watermark)
      form.setFieldValue("send_buffer_low_watermark", preferences.send_buffer_low_watermark)
      form.setFieldValue("send_buffer_watermark_factor", preferences.send_buffer_watermark_factor)
      form.setFieldValue("max_concurrent_http_announces", preferences.max_concurrent_http_announces)
      form.setFieldValue("request_queue_size", preferences.request_queue_size)
      form.setFieldValue("stop_tracker_timeout", preferences.stop_tracker_timeout)

      // Disk I/O settings
      form.setFieldValue("async_io_threads", preferences.async_io_threads)
      form.setFieldValue("hashing_threads", preferences.hashing_threads)
      form.setFieldValue("file_pool_size", preferences.file_pool_size)
      form.setFieldValue("disk_cache", preferences.disk_cache)
      form.setFieldValue("disk_cache_ttl", preferences.disk_cache_ttl)
      form.setFieldValue("disk_queue_size", preferences.disk_queue_size)
      form.setFieldValue("disk_io_type", preferences.disk_io_type)
      form.setFieldValue("disk_io_read_mode", preferences.disk_io_read_mode)
      form.setFieldValue("disk_io_write_mode", preferences.disk_io_write_mode)
      form.setFieldValue("checking_memory_use", preferences.checking_memory_use)
      form.setFieldValue("memory_working_set_limit", preferences.memory_working_set_limit)
      form.setFieldValue("enable_coalesce_read_write", preferences.enable_coalesce_read_write)

      // Peer behavior
      form.setFieldValue("peer_turnover", preferences.peer_turnover)
      form.setFieldValue("peer_turnover_cutoff", preferences.peer_turnover_cutoff)
      form.setFieldValue("peer_turnover_interval", preferences.peer_turnover_interval)

      // Security & filtering
      form.setFieldValue("block_peers_on_privileged_ports", preferences.block_peers_on_privileged_ports)
    }
  }, [preferences, form])

  if (isLoading || !preferences) {
    return <div className="flex items-center justify-center py-8">Loading advanced network settings...</div>
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
          <AlertTitle>Showing all advanced options</AlertTitle>
          <AlertDescription>
            We couldn&apos;t determine this instance&apos;s qBittorrent version, so every advanced
            setting is displayed. Some options might not apply to your environment.
          </AlertDescription>
        </Alert>
      )}

      {/* Tracker Settings */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Radio className="h-4 w-4" />
          <h3 className="text-lg font-medium">Tracker Settings</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="announce_ip">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="announce_ip">IP address reported to trackers (requires restart)</Label>
                <Input
                  id="announce_ip"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder="Auto-detect"
                />
                <p className="text-xs text-muted-foreground">
                  IP address to announce to trackers (leave empty for auto-detect)
                </p>
              </div>
            )}
          </form.Field>
        </div>
      </div>

      {/* Performance Optimization */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Zap className="h-4 w-4" />
          <h3 className="text-lg font-medium">Performance Optimization</h3>
        </div>

        <div className="space-y-4">
          {/* Switch Settings */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <form.Field name="limit_lan_peers">
              {(field) => (
                <SwitchSetting
                  label="Apply rate limit to μTP protocol"
                  description="Limit μTP connections to prevent flooding LAN peers"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_tcp_overhead">
              {(field) => (
                <SwitchSetting
                  label="Apply rate limit to transport overhead"
                  description="Include protocol overhead in rate limiting calculations"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_utp_rate">
              {(field) => (
                <SwitchSetting
                  label="Apply rate limit to μTP connections"
                  description="Apply upload/download limits to μTP connections"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>

          {/* Number input fields - combined for proper flow */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <form.Field name="peer_tos">
              {(field) => (
                <NumberInput
                  label="Peer ToS Byte"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={255}
                  description="Type of Service byte for peer connections"
                />
              )}
            </form.Field>

            <form.Field name="max_concurrent_http_announces">
              {(field) => (
                <NumberInput
                  label="Max HTTP Announces"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description="Maximum concurrent HTTP tracker announces"
                />
              )}
            </form.Field>

            <form.Field name="stop_tracker_timeout">
              {(field) => (
                <NumberInput
                  label="Stop Tracker Timeout"
                  unit="seconds"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description="Timeout for tracker stop announcements"
                />
              )}
            </form.Field>

            {fieldVisibility.showSocketBacklogField && (
              <form.Field name="socket_backlog_size">
                {(field) => (
                  <NumberInput
                    label="Socket Backlog Size"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description="Number of pending connections in socket backlog"
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showRequestQueueField && (
              <form.Field name="request_queue_size">
                {(field) => (
                  <NumberInput
                    label="Request Queue Size"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description="Maximum number of queued piece requests"
                  />
                )}
              </form.Field>
            )}

            {/* Send Buffer Fields - moved into main grid for proper flow */}
            {fieldVisibility.showSendBufferFields && (
              <>
                <form.Field name="send_buffer_watermark">
                  {(field) => (
                    <NumberInput
                      label="Send Buffer Watermark"
                      unit="KiB"
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description="Upper watermark for socket send buffer"
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_low_watermark">
                  {(field) => (
                    <NumberInput
                      label="Send Buffer Low Watermark"
                      unit="KiB"
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description="Lower watermark for socket send buffer"
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_watermark_factor">
                  {(field) => (
                    <NumberInput
                      label="Watermark Factor"
                      unit="%"
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description="Send buffer watermark factor percentage"
                    />
                  )}
                </form.Field>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Disk I/O Settings */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <HardDrive className="h-4 w-4" />
          <h3 className="text-lg font-medium">Disk I/O & Memory</h3>
        </div>

        <div className="space-y-4">
          {/* Coalesce switch at top */}
          {fieldVisibility.showCoalesceReadsWritesField && (
            <div className="space-y-3">
              <form.Field name="enable_coalesce_read_write">
                {(field) => (
                  <SwitchSetting
                    label="Coalesce reads & writes"
                    description="Combine adjacent disk reads and writes for better performance"
                    checked={field.state.value}
                    onChange={(checked) => field.handleChange(checked)}
                  />
                )}
              </form.Field>
            </div>
          )}

          {/* All fields combined in single grid for proper flow */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {/* Always visible fields */}
            <form.Field name="async_io_threads">
              {(field) => (
                <NumberInput
                  label="Async I/O Threads"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description="Number of threads for asynchronous I/O operations"
                />
              )}
            </form.Field>

            <form.Field name="file_pool_size">
              {(field) => (
                <NumberInput
                  label="File Pool Size"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description="Maximum number of open file handles in pool"
                />
              )}
            </form.Field>

            <form.Field name="disk_queue_size">
              {(field) => (
                <NumberInput
                  label="Disk Queue Size"
                  unit="bytes"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1024}
                  description="Maximum bytes queued for disk I/O"
                />
              )}
            </form.Field>

            <form.Field 
              name="checking_memory_use"
              validators={{
                onChange: ({ value }) => {
                  if (value <= 0 || value > 1024) {
                    return 'Outstanding memory when checking torrents must be greater than 0 and less than 1024'
                  }
                  return undefined
                }
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label="Checking Memory Use"
                    unit="MiB"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    max={1024}
                    description="Maximum memory used for piece checking"
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            {/* Version-dependent fields - flow with always-visible fields */}
            {fieldVisibility.showHashingThreadsField && (
              <form.Field name="hashing_threads">
                {(field) => (
                  <NumberInput
                    label="Hashing Threads"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description="Number of threads for piece hash checking"
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showDiskCacheFields && (
              <>
                <form.Field name="disk_cache">
                  {(field) => (
                    <NumberInput
                      label="Disk Cache Size"
                      unit="MiB"
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={-1}
                      description="Disk cache size (-1 = auto, 0 = disabled)"
                    />
                  )}
                </form.Field>

                <form.Field name="disk_cache_ttl">
                  {(field) => (
                    <NumberInput
                      label="Disk Cache TTL"
                      unit="seconds"
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description="How long to keep cached data in memory"
                    />
                  )}
                </form.Field>
              </>
            )}

            {fieldVisibility.showMemoryWorkingSetLimit && (
              <form.Field name="memory_working_set_limit">
                {(field) => (
                  <NumberInput
                    label="Working Set Limit"
                    unit="MiB"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description="Physical memory working set size limit"
                  />
                )}
              </form.Field>
            )}
          </div>
        </div>
      </div>

      {/* Peer Management */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Settings className="h-4 w-4" />
          <h3 className="text-lg font-medium">Peer Management</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <form.Field 
            name="peer_turnover"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 100) {
                  return 'Peer turnover must be between 0 and 100'
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label="Peer Turnover"
                  unit="%"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description="Percentage of peers to disconnect/reconnect"
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field 
            name="peer_turnover_cutoff"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 100) {
                  return 'Peer turnover cutoff must be between 0 and 100'
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label="Turnover Cutoff"
                  unit="%"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description="Peer turnover threshold percentage"
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field 
            name="peer_turnover_interval"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 3600) {
                  return 'Peer turnover interval must be greater than or equal to 0 and less than 3600 seconds'
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label="Turnover Interval"
                  unit="seconds"
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={3600}
                  description="How often to perform peer turnover"
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-red-500">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>
        </div>
      </div>

      {/* Security & IP Filtering */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Ban className="h-4 w-4" />
          <h3 className="text-lg font-medium">Security & IP Filtering</h3>
        </div>

        <div className="space-y-4">
          <div className="space-y-3">
            <form.Field name="block_peers_on_privileged_ports">
              {(field) => (
                <SwitchSetting
                  label="Block peers on privileged ports"
                  description="Block connections from peers using ports below 1024"
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
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
            {isSubmitting || isUpdating ? "Updating..." : "Update Advanced Network Settings"}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}
