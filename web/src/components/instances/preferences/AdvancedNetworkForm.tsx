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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Settings, HardDrive, Zap, Ban, Radio, AlertTriangle } from "lucide-react"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useQBittorrentFieldVisibility } from "@/hooks/useQBittorrentAppInfo"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"

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
  const { t } = useTranslation()
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
        toast.success(t("instancePreferences.content.advancedSettings.notifications.saveSuccess"))
        onSuccess?.()
      } catch (error) {
        toast.error(t("instancePreferences.content.advancedSettings.notifications.saveError"))
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
    return <div className="flex items-center justify-center py-8">{t("instancePreferences.content.advancedSettings.loading")}</div>
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
          <AlertTitle>{t("instancePreferences.content.advancedSettings.warnings.showAll.title")}</AlertTitle>
          <AlertDescription>
            {t("instancePreferences.content.advancedSettings.warnings.showAll.description")}
          </AlertDescription>
        </Alert>
      )}

      {/* Tracker Settings */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Radio className="h-4 w-4" />
          <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.tracker.title")}</h3>
        </div>

        <div className="space-y-4">
          <form.Field name="announce_ip">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="announce_ip">{t("instancePreferences.content.advancedSettings.tracker.announceIP")}</Label>
                <Input
                  id="announce_ip"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={t("instancePreferences.content.advancedSettings.tracker.autoDetect")}
                />
                <p className="text-xs text-muted-foreground">
                  {t("instancePreferences.content.advancedSettings.tracker.announceIPDescription")}
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
          <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.performance.title")}</h3>
        </div>

        <div className="space-y-4">
          {/* Switch Settings */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <form.Field name="limit_lan_peers">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.advancedSettings.performance.limitUTP")}
                  description={t("instancePreferences.content.advancedSettings.performance.limitUTPDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_tcp_overhead">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.advancedSettings.performance.limitTCPOverhead")}
                  description={t("instancePreferences.content.advancedSettings.performance.limitTCPOverheadDescription")}
                  checked={field.state.value}
                  onChange={(checked) => field.handleChange(checked)}
                />
              )}
            </form.Field>

            <form.Field name="limit_utp_rate">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.advancedSettings.performance.limitUTPRate")}
                  description={t("instancePreferences.content.advancedSettings.performance.limitUTPRateDescription")}
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
                  label={t("instancePreferences.content.advancedSettings.performance.peerTOS")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={255}
                  description={t("instancePreferences.content.advancedSettings.performance.peerTOSDescription")}
                />
              )}
            </form.Field>

            <form.Field name="max_concurrent_http_announces">
              {(field) => (
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.performance.maxHTTPAnnounces")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={t("instancePreferences.content.advancedSettings.performance.maxHTTPAnnouncesDescription")}
                />
              )}
            </form.Field>

            <form.Field name="stop_tracker_timeout">
              {(field) => (
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.performance.stopTrackerTimeout")}
                  unit={t("instancePreferences.content.advancedSettings.performance.stopTrackerTimeoutUnit")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={t("instancePreferences.content.advancedSettings.performance.stopTrackerTimeoutDescription")}
                />
              )}
            </form.Field>

            {fieldVisibility.showSocketBacklogField && (
              <form.Field name="socket_backlog_size">
                {(field) => (
                  <NumberInput
                    label={t("instancePreferences.content.advancedSettings.performance.socketBacklog")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("instancePreferences.content.advancedSettings.performance.socketBacklogDescription")}
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showRequestQueueField && (
              <form.Field name="request_queue_size">
                {(field) => (
                  <NumberInput
                    label={t("instancePreferences.content.advancedSettings.performance.requestQueue")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("instancePreferences.content.advancedSettings.performance.requestQueueDescription")}
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
                      label={t("instancePreferences.content.advancedSettings.performance.sendBufferWatermark")}
                      unit={t("instancePreferences.content.advancedSettings.performance.sendBufferWatermarkUnit")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("instancePreferences.content.advancedSettings.performance.sendBufferWatermarkDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_low_watermark">
                  {(field) => (
                    <NumberInput
                      label={t("instancePreferences.content.advancedSettings.performance.sendBufferLowWatermark")}
                      unit={t("instancePreferences.content.advancedSettings.performance.sendBufferLowWatermarkUnit")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("instancePreferences.content.advancedSettings.performance.sendBufferLowWatermarkDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="send_buffer_watermark_factor">
                  {(field) => (
                    <NumberInput
                      label={t("instancePreferences.content.advancedSettings.performance.watermarkFactor")}
                      unit={t("instancePreferences.content.advancedSettings.performance.watermarkFactorUnit")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("instancePreferences.content.advancedSettings.performance.watermarkFactorDescription")}
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
          <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.diskIO.title")}</h3>
        </div>

        <div className="space-y-4">
          {/* Coalesce switch at top */}
          {fieldVisibility.showCoalesceReadsWritesField && (
            <div className="space-y-3">
              <form.Field name="enable_coalesce_read_write">
                {(field) => (
                  <SwitchSetting
                    label={t("instancePreferences.content.advancedSettings.diskIO.coalesceReadWrite")}
                    description={t("instancePreferences.content.advancedSettings.diskIO.coalesceReadWriteDescription")}
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
                  label={t("instancePreferences.content.advancedSettings.diskIO.asyncThreads")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={t("instancePreferences.content.advancedSettings.diskIO.asyncThreadsDescription")}
                />
              )}
            </form.Field>

            <form.Field name="file_pool_size">
              {(field) => (
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.diskIO.filePool")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1}
                  description={t("instancePreferences.content.advancedSettings.diskIO.filePoolDescription")}
                />
              )}
            </form.Field>

            <form.Field name="disk_queue_size">
              {(field) => (
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.diskIO.diskQueue")}
                  unit={t("instancePreferences.content.advancedSettings.diskIO.diskQueueUnit")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={1024}
                  description={t("instancePreferences.content.advancedSettings.diskIO.diskQueueDescription")}
                />
              )}
            </form.Field>

            <form.Field
              name="checking_memory_use"
              validators={{
                onChange: ({ value }) => {
                  if (value <= 0 || value > 1024) {
                    return t("instancePreferences.content.advancedSettings.diskIO.validation.checkingMemory")
                  }
                  return undefined
                }
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("instancePreferences.content.advancedSettings.diskIO.checkingMemory")}
                    unit={t("instancePreferences.content.advancedSettings.diskIO.checkingMemoryUnit")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    max={1024}
                    description={t("instancePreferences.content.advancedSettings.diskIO.checkingMemoryDescription")}
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
                    label={t("instancePreferences.content.advancedSettings.diskIO.hashingThreads")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("instancePreferences.content.advancedSettings.diskIO.hashingThreadsDescription")}
                  />
                )}
              </form.Field>
            )}

            {fieldVisibility.showDiskCacheFields && (
              <>
                <form.Field name="disk_cache">
                  {(field) => (
                    <NumberInput
                      label={t("instancePreferences.content.advancedSettings.diskIO.diskCache")}
                      unit={t("instancePreferences.content.advancedSettings.diskIO.diskCacheUnit")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={-1}
                      description={t("instancePreferences.content.advancedSettings.diskIO.diskCacheDescription")}
                    />
                  )}
                </form.Field>

                <form.Field name="disk_cache_ttl">
                  {(field) => (
                    <NumberInput
                      label={t("instancePreferences.content.advancedSettings.diskIO.diskCacheTTL")}
                      unit={t("instancePreferences.content.advancedSettings.diskIO.diskCacheTTLUnit")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("instancePreferences.content.advancedSettings.diskIO.diskCacheTTLDescription")}
                    />
                  )}
                </form.Field>
              </>
            )}

            {fieldVisibility.showMemoryWorkingSetLimit && (
              <form.Field name="memory_working_set_limit">
                {(field) => (
                  <NumberInput
                    label={t("instancePreferences.content.advancedSettings.diskIO.workingSetLimit")}
                    unit={t("instancePreferences.content.advancedSettings.diskIO.workingSetLimitUnit")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("instancePreferences.content.advancedSettings.diskIO.workingSetLimitDescription")}
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
          <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.peerManagement.title")}</h3>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <form.Field
            name="peer_turnover"
            validators={{
              onChange: ({ value }) => {
                if (value < 0 || value > 100) {
                  return t("instancePreferences.content.advancedSettings.peerManagement.validation.turnover")
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.peerManagement.turnover")}
                  unit={t("instancePreferences.content.advancedSettings.peerManagement.turnoverUnit")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description={t("instancePreferences.content.advancedSettings.peerManagement.turnoverDescription")}
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
                  return t("instancePreferences.content.advancedSettings.peerManagement.validation.turnoverCutoff")
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.peerManagement.turnoverCutoff")}
                  unit={t("instancePreferences.content.advancedSettings.peerManagement.turnoverCutoffUnit")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={100}
                  description={t("instancePreferences.content.advancedSettings.peerManagement.turnoverCutoffDescription")}
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
                  return t("instancePreferences.content.advancedSettings.peerManagement.validation.turnoverInterval")
                }
                return undefined
              }
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <NumberInput
                  label={t("instancePreferences.content.advancedSettings.peerManagement.turnoverInterval")}
                  unit={t("instancePreferences.content.advancedSettings.peerManagement.turnoverIntervalUnit")}
                  value={field.state.value}
                  onChange={(value) => field.handleChange(value)}
                  min={0}
                  max={3600}
                  description={t("instancePreferences.content.advancedSettings.peerManagement.turnoverIntervalDescription")}
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
          <h3 className="text-lg font-medium">{t("instancePreferences.content.advancedSettings.security.title")}</h3>
        </div>

        <div className="space-y-4">
          <div className="space-y-3">
            <form.Field name="block_peers_on_privileged_ports">
              {(field) => (
                <SwitchSetting
                  label={t("instancePreferences.content.advancedSettings.security.blockPrivilegedPorts")}
                  description={t("instancePreferences.content.advancedSettings.security.blockPrivilegedPortsDescription")}
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
            {isSubmitting || isUpdating ? t("instancePreferences.content.advancedSettings.savingButton") : t("instancePreferences.content.advancedSettings.saveButton")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}
