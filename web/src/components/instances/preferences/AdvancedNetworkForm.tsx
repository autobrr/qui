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
import { useTranslation } from "react-i18next"
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
  const inputId = React.useId()
  const descriptionId = description ? `${inputId}-desc` : undefined

  return (
    <div className="space-y-2">
      <Label htmlFor={inputId} className="text-sm font-medium">
        {label}
        {unit && <span className="text-muted-foreground ml-1">({unit})</span>}
      </Label>
      {description && (
        <p id={descriptionId} className="text-xs text-muted-foreground">{description}</p>
      )}
      <Input
        id={inputId}
        type="number"
        min={min}
        max={max}
        value={value ?? ""}
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

export function AdvancedNetworkForm({ instanceId, onSuccess }: AdvancedNetworkFormProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
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
    onSubmit: ({ value }) => {
      updatePreferences(value, {
        onSuccess: () => {
          toast.success(tr("advancedNetworkForm.toasts.updated"))
          onSuccess?.()
        },
        onError: (error) => {
          toast.error(tr("advancedNetworkForm.toasts.failedUpdate"))
          console.error("Failed to update advanced network settings:", error)
        },
      })
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
  // eslint-disable-next-line react-hooks/exhaustive-deps -- form reference is stable, only sync on preferences change
  }, [preferences])

  if (isLoading || !preferences) {
    return (
      <div className="flex items-center justify-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">{tr("advancedNetworkForm.loading")}</p>
      </div>
    )
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
          <AlertTitle>{tr("advancedNetworkForm.alerts.unknownVersionTitle")}</AlertTitle>
          <AlertDescription>
            {tr("advancedNetworkForm.alerts.unknownVersionDescription")}
          </AlertDescription>
        </Alert>
      )}

      {/* Tracker Settings */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Radio className="h-4 w-4" />
          <h3 className="text-lg font-medium">{tr("advancedNetworkForm.sections.trackerSettings")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="announce_ip">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="announce_ip">{tr("advancedNetworkForm.fields.announceIpLabel")}</Label>
                <Input
                  id="announce_ip"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={tr("advancedNetworkForm.fields.announceIpPlaceholder")}
                />
                <p className="text-xs text-muted-foreground">
                  {tr("advancedNetworkForm.fields.announceIpDescription")}
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
          <h3 className="text-lg font-medium">{tr("advancedNetworkForm.sections.performanceOptimization")}</h3>
        </div>

        <div className="space-y-4">
          {/* Switch Settings */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <form.Field name="limit_lan_peers">
              {(field) => (
                <SwitchSetting
                  label={tr("advancedNetworkForm.fields.limitLanPeersLabel")}
                  description={tr("advancedNetworkForm.fields.limitLanPeersDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_tcp_overhead">
              {(field) => (
                <SwitchSetting
                  label={tr("advancedNetworkForm.fields.limitTcpOverheadLabel")}
                  description={tr("advancedNetworkForm.fields.limitTcpOverheadDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_utp_rate">
              {(field) => (
                <SwitchSetting
                  label={tr("advancedNetworkForm.fields.limitUtpRateLabel")}
                  description={tr("advancedNetworkForm.fields.limitUtpRateDescription")}
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
                  label={tr("advancedNetworkForm.fields.peerTosLabel")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={255}
                  description={tr("advancedNetworkForm.fields.peerTosDescription")}
                />
              )}
            </form.Field>

            <form.Field name="max_concurrent_http_announces">
              {(field) => (
                <NumberInput
                  label={tr("advancedNetworkForm.fields.maxConcurrentHttpAnnouncesLabel")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={tr("advancedNetworkForm.fields.maxConcurrentHttpAnnouncesDescription")}
                />
              )}
            </form.Field>

            <form.Field name="stop_tracker_timeout">
              {(field) => (
                <NumberInput
                  label={tr("advancedNetworkForm.fields.stopTrackerTimeoutLabel")}
                  unit={tr("advancedNetworkForm.units.seconds")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={tr("advancedNetworkForm.fields.stopTrackerTimeoutDescription")}
                />
              )}
            </form.Field>

            {fieldVisibility.showSocketBacklogField && (
              <form.Field name="socket_backlog_size">
                {(field) => (
                  <NumberInput
                    label={tr("advancedNetworkForm.fields.socketBacklogSizeLabel")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={tr("advancedNetworkForm.fields.socketBacklogSizeDescription")}
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showRequestQueueField && (
              <form.Field name="request_queue_size">
                {(field) => (
                  <NumberInput
                    label={tr("advancedNetworkForm.fields.requestQueueSizeLabel")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={tr("advancedNetworkForm.fields.requestQueueSizeDescription")}
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
                      label={tr("advancedNetworkForm.fields.sendBufferWatermarkLabel")}
                      unit={tr("advancedNetworkForm.units.kib")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={tr("advancedNetworkForm.fields.sendBufferWatermarkDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_low_watermark">
                  {(field) => (
                    <NumberInput
                      label={tr("advancedNetworkForm.fields.sendBufferLowWatermarkLabel")}
                      unit={tr("advancedNetworkForm.units.kib")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={tr("advancedNetworkForm.fields.sendBufferLowWatermarkDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_watermark_factor">
                  {(field) => (
                    <NumberInput
                      label={tr("advancedNetworkForm.fields.watermarkFactorLabel")}
                      unit={tr("advancedNetworkForm.units.percent")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={tr("advancedNetworkForm.fields.watermarkFactorDescription")}
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
          <h3 className="text-lg font-medium">{tr("advancedNetworkForm.sections.diskIoMemory")}</h3>
        </div>

        <div className="space-y-4">
          {/* Coalesce switch at top */}
          {fieldVisibility.showCoalesceReadsWritesField && (
            <div className="space-y-3">
              <form.Field name="enable_coalesce_read_write">
                {(field) => (
                  <SwitchSetting
                    label={tr("advancedNetworkForm.fields.enableCoalesceReadWriteLabel")}
                    description={tr("advancedNetworkForm.fields.enableCoalesceReadWriteDescription")}
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
                  label={tr("advancedNetworkForm.fields.asyncIoThreadsLabel")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={tr("advancedNetworkForm.fields.asyncIoThreadsDescription")}
                />
              )}
            </form.Field>

            <form.Field name="file_pool_size">
              {(field) => (
                <NumberInput
                  label={tr("advancedNetworkForm.fields.filePoolSizeLabel")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={tr("advancedNetworkForm.fields.filePoolSizeDescription")}
                />
              )}
            </form.Field>

            <form.Field name="disk_queue_size">
              {(field) => (
                <NumberInput
                  label={tr("advancedNetworkForm.fields.diskQueueSizeLabel")}
                  unit={tr("advancedNetworkForm.units.bytes")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1024}
                  description={tr("advancedNetworkForm.fields.diskQueueSizeDescription")}
                />
              )}
            </form.Field>

            <form.Field
              name="checking_memory_use"
              validators={{
                onChange: ({ value }) => {
                  if (value <= 0 || value > 1024) {
                    return tr("advancedNetworkForm.errors.checkingMemoryUseRange")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={tr("advancedNetworkForm.fields.checkingMemoryUseLabel")}
                    unit={tr("advancedNetworkForm.units.mib")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    max={1024}
                    description={tr("advancedNetworkForm.fields.checkingMemoryUseDescription")}
                  />
                  {field.state.meta.errors.length > 0 && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            {/* Version-dependent fields - flow with always-visible fields */}
            {fieldVisibility.showHashingThreadsField && (
              <form.Field name="hashing_threads">
                {(field) => (
                  <NumberInput
                    label={tr("advancedNetworkForm.fields.hashingThreadsLabel")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={tr("advancedNetworkForm.fields.hashingThreadsDescription")}
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showDiskCacheFields && (
              <>
                <form.Field name="disk_cache">
                  {(field) => (
                    <NumberInput
                      label={tr("advancedNetworkForm.fields.diskCacheSizeLabel")}
                      unit={tr("advancedNetworkForm.units.mib")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={-1}
                      description={tr("advancedNetworkForm.fields.diskCacheSizeDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="disk_cache_ttl">
                  {(field) => (
                    <NumberInput
                      label={tr("advancedNetworkForm.fields.diskCacheTtlLabel")}
                      unit={tr("advancedNetworkForm.units.seconds")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={tr("advancedNetworkForm.fields.diskCacheTtlDescription")}
                    />
                  )}
                </form.Field>
              </>
            )}

            {fieldVisibility.showMemoryWorkingSetLimit && (
              <form.Field name="memory_working_set_limit">
                {(field) => (
                  <NumberInput
                    label={tr("advancedNetworkForm.fields.workingSetLimitLabel")}
                    unit={tr("advancedNetworkForm.units.mib")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={tr("advancedNetworkForm.fields.workingSetLimitDescription")}
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
          <h3 className="text-lg font-medium">{tr("advancedNetworkForm.sections.peerManagement")}</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <form.Field
            name="peer_turnover"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 100) {
                  return tr("advancedNetworkForm.errors.peerTurnoverRange")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={tr("advancedNetworkForm.fields.peerTurnoverLabel")}
                  unit={tr("advancedNetworkForm.units.percent")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description={tr("advancedNetworkForm.fields.peerTurnoverDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="peer_turnover_cutoff"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 100) {
                  return tr("advancedNetworkForm.errors.peerTurnoverCutoffRange")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={tr("advancedNetworkForm.fields.turnoverCutoffLabel")}
                  unit={tr("advancedNetworkForm.units.percent")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description={tr("advancedNetworkForm.fields.turnoverCutoffDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          <form.Field
            name="peer_turnover_interval"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 3600) {
                  return tr("advancedNetworkForm.errors.peerTurnoverIntervalRange")
                }
                return undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={tr("advancedNetworkForm.fields.turnoverIntervalLabel")}
                  unit={tr("advancedNetworkForm.units.seconds")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={3600}
                  description={tr("advancedNetworkForm.fields.turnoverIntervalDescription")}
                />
                {field.state.meta.errors.length > 0 && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
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
          <h3 className="text-lg font-medium">{tr("advancedNetworkForm.sections.securityIpFiltering")}</h3>
        </div>

        <div className="space-y-4">
          <div className="space-y-3">
            <form.Field name="block_peers_on_privileged_ports">
              {(field) => (
                <SwitchSetting
                  label={tr("advancedNetworkForm.fields.blockPeersPrivilegedPortsLabel")}
                  description={tr("advancedNetworkForm.fields.blockPeersPrivilegedPortsDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>
          </div>
        </div>
      </div>

      <div className="flex justify-end pt-4">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {isSubmitting || isUpdating ? tr("advancedNetworkForm.actions.saving") : tr("advancedNetworkForm.actions.saveChanges")}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
