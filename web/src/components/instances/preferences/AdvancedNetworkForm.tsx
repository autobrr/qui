/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React from "react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { FieldHelp } from "@/components/ui/field-help"
import { Settings, HardDrive, Zap, Ban, Radio, AlertTriangle } from "lucide-react"
import { usePreferencesForm } from "@/hooks/usePreferencesForm"
import { useQBittorrentFieldVisibility } from "@/hooks/useQBittorrentAppInfo"
import { unitLabel } from "@/lib/unit-format"
import type { AppPreferences } from "@/types"
import { useTranslation } from "react-i18next"

import { PreferencesFormShell } from "./PreferencesFormShell"
import { PreferencesSection } from "./PreferencesSection"

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

  return (
    <label
      htmlFor={switchId}
      className="flex items-center gap-3 cursor-pointer"
    >
      <Switch
        id={switchId}
        checked={checked}
        onCheckedChange={onChange}
      />
      <span className="text-sm font-medium">{label}</span>
      {description && <FieldHelp>{description}</FieldHelp>}
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

  return (
    <div className="space-y-2">
      <Label htmlFor={inputId} className="flex items-center gap-2 text-sm font-medium">
        {label}
        {unit && <span className="text-muted-foreground">({unit})</span>}
        {description && <FieldHelp>{description}</FieldHelp>}
      </Label>
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
      />
    </div>
  )
}

export function AdvancedNetworkForm({ instanceId, onSuccess }: AdvancedNetworkFormProps) {
  return (
    <PreferencesSection instanceId={instanceId} i18nPrefix="preferences.advancedNetwork">
      {(preferences) => <AdvancedNetworkFields instanceId={instanceId} preferences={preferences} onSuccess={onSuccess} />}
    </PreferencesSection>
  )
}

function AdvancedNetworkFields({ instanceId, preferences, onSuccess }: AdvancedNetworkFormProps & { preferences: AppPreferences }) {
  const { t } = useTranslation("instances")
  const fieldVisibility = useQBittorrentFieldVisibility(instanceId)
  const { form, isUpdating } = usePreferencesForm({
    instanceId,
    preferences,
    toForm: (p) => ({
      // Tracker settings
      announce_ip: p.announce_ip,

      // Performance settings
      limit_lan_peers: p.limit_lan_peers,
      limit_tcp_overhead: p.limit_tcp_overhead,
      limit_utp_rate: p.limit_utp_rate,
      peer_tos: p.peer_tos,
      socket_backlog_size: p.socket_backlog_size,
      send_buffer_watermark: p.send_buffer_watermark,
      send_buffer_low_watermark: p.send_buffer_low_watermark,
      send_buffer_watermark_factor: p.send_buffer_watermark_factor,
      max_concurrent_http_announces: p.max_concurrent_http_announces,
      request_queue_size: p.request_queue_size,
      stop_tracker_timeout: p.stop_tracker_timeout,

      // Disk I/O settings
      async_io_threads: p.async_io_threads,
      hashing_threads: p.hashing_threads,
      file_pool_size: p.file_pool_size,
      disk_cache: p.disk_cache,
      disk_cache_ttl: p.disk_cache_ttl,
      disk_queue_size: p.disk_queue_size,
      disk_io_type: p.disk_io_type,
      disk_io_read_mode: p.disk_io_read_mode,
      disk_io_write_mode: p.disk_io_write_mode,
      checking_memory_use: p.checking_memory_use,
      memory_working_set_limit: p.memory_working_set_limit,
      enable_coalesce_read_write: p.enable_coalesce_read_write,

      // Peer behavior
      peer_turnover: p.peer_turnover,
      peer_turnover_cutoff: p.peer_turnover_cutoff,
      peer_turnover_interval: p.peer_turnover_interval,

      // Security & filtering
      block_peers_on_privileged_ports: p.block_peers_on_privileged_ports,
    }),
    i18nPrefix: "preferences.advancedNetwork",
    onSaved: onSuccess,
  })

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
            <AlertTitle>{t("preferences.advancedNetwork.versionWarningTitle")}</AlertTitle>
            <AlertDescription>
              {t("preferences.advancedNetwork.versionWarningDescription")}
            </AlertDescription>
          </Alert>
        )}

        {/* Tracker Settings */}
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Radio className="h-4 w-4" />
            <h3 className="text-lg font-medium">{t("preferences.advancedNetwork.trackerSettings")}</h3>
          </div>

          <div className="space-y-4">
            <form.Field name="announce_ip">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="announce_ip" className="flex items-center gap-2">
                    {t("preferences.advancedNetwork.announceIp")}
                    <FieldHelp>{t("preferences.advancedNetwork.announceIpDescription")}</FieldHelp>
                  </Label>
                  <Input
                    id="announce_ip"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("preferences.connectionSettings.autoDetect")}
                  />
                </div>
              )}
            </form.Field>
          </div>
        </div>

        {/* Performance Optimization */}
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Zap className="h-4 w-4" />
            <h3 className="text-lg font-medium">{t("preferences.advancedNetwork.performanceOptimization")}</h3>
          </div>

          <div className="space-y-4">
            {/* Switch Settings */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <form.Field name="limit_lan_peers">
                {(field) => (
                  <SwitchSetting
                    label={t("preferences.advancedNetwork.limitUtpProtocol")}
                    description={t("preferences.advancedNetwork.limitUtpProtocolDescription")}
                    checked={field.state.value}
                    onChange={(checked) => field.handleChange(checked)}
                  />
                )}
              </form.Field>

              <form.Field name="limit_tcp_overhead">
                {(field) => (
                  <SwitchSetting
                    label={t("preferences.advancedNetwork.limitTcpOverhead")}
                    description={t("preferences.advancedNetwork.limitTcpOverheadDescription")}
                    checked={field.state.value}
                    onChange={(checked) => field.handleChange(checked)}
                  />
                )}
              </form.Field>

              <form.Field name="limit_utp_rate">
                {(field) => (
                  <SwitchSetting
                    label={t("preferences.advancedNetwork.limitUtpConnections")}
                    description={t("preferences.advancedNetwork.limitUtpConnectionsDescription")}
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
                    label={t("preferences.advancedNetwork.peerTosByte")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={255}
                    description={t("preferences.advancedNetwork.peerTosDescription")}
                  />
                )}
              </form.Field>

              <form.Field name="max_concurrent_http_announces">
                {(field) => (
                  <NumberInput
                    label={t("preferences.advancedNetwork.maxHttpAnnounces")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("preferences.advancedNetwork.maxHttpAnnouncesDescription")}
                  />
                )}
              </form.Field>

              <form.Field name="stop_tracker_timeout">
                {(field) => (
                  <NumberInput
                    label={t("preferences.advancedNetwork.stopTrackerTimeout")}
                    unit={t("preferences.advancedNetwork.unitSeconds")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("preferences.advancedNetwork.stopTrackerTimeoutDescription")}
                  />
                )}
              </form.Field>

              {fieldVisibility.showSocketBacklogField && (
                <form.Field name="socket_backlog_size">
                  {(field) => (
                    <NumberInput
                      label={t("preferences.advancedNetwork.socketBacklogSize")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("preferences.advancedNetwork.socketBacklogSizeDescription")}
                    />
                  )}
                </form.Field>
              )}

              {fieldVisibility.showRequestQueueField && (
                <form.Field name="request_queue_size">
                  {(field) => (
                    <NumberInput
                      label={t("preferences.advancedNetwork.requestQueueSize")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("preferences.advancedNetwork.requestQueueSizeDescription")}
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
                        label={t("preferences.advancedNetwork.sendBufferWatermark")}
                        unit={unitLabel("KiB")}
                        value={field.state.value}
                        onChange={(value) => field.handleChange(value)}
                        min={1}
                        description={t("preferences.advancedNetwork.sendBufferWatermarkDescription")}
                      />
                    )}
                  </form.Field>

                  <form.Field name="send_buffer_low_watermark">
                    {(field) => (
                      <NumberInput
                        label={t("preferences.advancedNetwork.sendBufferLowWatermark")}
                        unit={unitLabel("KiB")}
                        value={field.state.value}
                        onChange={(value) => field.handleChange(value)}
                        min={1}
                        description={t("preferences.advancedNetwork.sendBufferLowWatermarkDescription")}
                      />
                    )}
                  </form.Field>

                  <form.Field name="send_buffer_watermark_factor">
                    {(field) => (
                      <NumberInput
                        label={t("preferences.advancedNetwork.watermarkFactor")}
                        unit="%"
                        value={field.state.value}
                        onChange={(value) => field.handleChange(value)}
                        min={1}
                        description={t("preferences.advancedNetwork.watermarkFactorDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.advancedNetwork.diskIoMemory")}</h3>
          </div>

          <div className="space-y-4">
            {/* Coalesce switch at top */}
            {fieldVisibility.showCoalesceReadsWritesField && (
              <div className="space-y-3">
                <form.Field name="enable_coalesce_read_write">
                  {(field) => (
                    <SwitchSetting
                      label={t("preferences.advancedNetwork.coalesceReadsWrites")}
                      description={t("preferences.advancedNetwork.coalesceReadsWritesDescription")}
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
                    label={t("preferences.advancedNetwork.asyncIoThreads")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("preferences.advancedNetwork.asyncIoThreadsDescription")}
                  />
                )}
              </form.Field>

              <form.Field name="file_pool_size">
                {(field) => (
                  <NumberInput
                    label={t("preferences.advancedNetwork.filePoolSize")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1}
                    description={t("preferences.advancedNetwork.filePoolSizeDescription")}
                  />
                )}
              </form.Field>

              <form.Field name="disk_queue_size">
                {(field) => (
                  <NumberInput
                    label={t("preferences.advancedNetwork.diskQueueSize")}
                    unit={unitLabel("B")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={1024}
                    description={t("preferences.advancedNetwork.diskQueueSizeDescription")}
                  />
                )}
              </form.Field>

              <form.Field
                name="checking_memory_use"
                validators={{
                  onChange: ({ value }) => {
                    if (value <= 0 || value > 1024) {
                      return t("preferences.advancedNetwork.validation.checkingMemoryUse")
                    }
                    return undefined
                  },
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <NumberInput
                      label={t("preferences.advancedNetwork.checkingMemoryUse")}
                      unit={unitLabel("MiB")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      max={1024}
                      description={t("preferences.advancedNetwork.checkingMemoryUseDescription")}
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
                      label={t("preferences.advancedNetwork.hashingThreads")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("preferences.advancedNetwork.hashingThreadsDescription")}
                    />
                  )}
                </form.Field>
              )}

              {fieldVisibility.showDiskCacheFields && (
                <>
                  <form.Field name="disk_cache">
                    {(field) => (
                      <NumberInput
                        label={t("preferences.advancedNetwork.diskCacheSize")}
                        unit={unitLabel("MiB")}
                        value={field.state.value}
                        onChange={(value) => field.handleChange(value)}
                        min={-1}
                        description={t("preferences.advancedNetwork.diskCacheSizeDescription")}
                      />
                    )}
                  </form.Field>

                  <form.Field name="disk_cache_ttl">
                    {(field) => (
                      <NumberInput
                        label={t("preferences.advancedNetwork.diskCacheTtl")}
                        unit={t("preferences.advancedNetwork.unitSeconds")}
                        value={field.state.value}
                        onChange={(value) => field.handleChange(value)}
                        min={1}
                        description={t("preferences.advancedNetwork.diskCacheTtlDescription")}
                      />
                    )}
                  </form.Field>
                </>
              )}

              {fieldVisibility.showMemoryWorkingSetLimit && (
                <form.Field name="memory_working_set_limit">
                  {(field) => (
                    <NumberInput
                      label={t("preferences.advancedNetwork.workingSetLimit")}
                      unit={unitLabel("MiB")}
                      value={field.state.value}
                      onChange={(value) => field.handleChange(value)}
                      min={1}
                      description={t("preferences.advancedNetwork.workingSetLimitDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.advancedNetwork.peerManagement")}</h3>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <form.Field
              name="peer_turnover"
              validators={{
                onChange: ({ value }) => {
                  if (value < 0 || value > 100) {
                    return t("preferences.advancedNetwork.validation.peerTurnover")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("preferences.advancedNetwork.peerTurnover")}
                    unit="%"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={100}
                    description={t("preferences.advancedNetwork.peerTurnoverDescription")}
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
                    return t("preferences.advancedNetwork.validation.peerTurnoverCutoff")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("preferences.advancedNetwork.turnoverCutoff")}
                    unit="%"
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={100}
                    description={t("preferences.advancedNetwork.turnoverCutoffDescription")}
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
                    return t("preferences.advancedNetwork.validation.peerTurnoverInterval")
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <NumberInput
                    label={t("preferences.advancedNetwork.turnoverInterval")}
                    unit={t("preferences.advancedNetwork.unitSeconds")}
                    value={field.state.value}
                    onChange={(value) => field.handleChange(value)}
                    min={0}
                    max={3600}
                    description={t("preferences.advancedNetwork.turnoverIntervalDescription")}
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
            <h3 className="text-lg font-medium">{t("preferences.advancedNetwork.securityIpFiltering")}</h3>
          </div>

          <div className="space-y-4">
            <div className="space-y-3">
              <form.Field name="block_peers_on_privileged_ports">
                {(field) => (
                  <SwitchSetting
                    label={t("preferences.advancedNetwork.blockPrivilegedPorts")}
                    description={t("preferences.advancedNetwork.blockPrivilegedPortsDescription")}
                    checked={field.state.value}
                    onChange={(checked) => field.handleChange(checked)}
                  />
                )}
              </form.Field>
            </div>
          </div>
        </div>
      </div>
    </PreferencesFormShell>
  )
}
