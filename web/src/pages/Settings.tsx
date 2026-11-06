/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { IndexersPage } from "@/components/indexers/IndexersPage"
import { InstanceCard } from "@/components/instances/InstanceCard"
import { InstanceForm } from "@/components/instances/InstanceForm"
import { MigrationHint } from "@/components/instances/MigrationHint"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { InstancePreferencesDialog } from "@/components/instances/preferences/InstancePreferencesDialog"
import { ArrInstancesManager } from "@/components/settings/ArrInstancesManager"
import { ApiKeysManager } from "@/components/settings/ApiKeysManager"
import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { DateTimePreferencesForm } from "@/components/settings/DateTimePreferencesForm"
import { GazelleSettingsCard } from "@/components/settings/GazelleSettingsCard"
import { DEFAULT_SETTINGS_TAB, SETTINGS_NAV_GROUPS, type SettingsTab } from "@/components/settings/tabs"
import { PageHeader } from "@/components/layout/PageHeader"
import { SectionedPage, type SectionNavGroup } from "@/components/layout/SectionedPage"
import { supportedLanguages, languageNames, changeLanguage, type AppLanguage } from "@/i18n"
import { ExternalProgramsManager } from "@/components/settings/ExternalProgramsManager"
import { LogSettingsPanel } from "@/components/settings/LogSettingsPanel"
import { NotificationsManager } from "@/components/settings/NotificationsManager"
import { InstallUpdateButton } from "@/components/settings/InstallUpdateButton"
import { RestartButton } from "@/components/settings/RestartButton"
import { RestartOverlay } from "@/components/settings/RestartOverlay"
import { LicenseManager } from "@/components/themes/LicenseManager.tsx"
import { ThemeSelector } from "@/components/themes/ThemeSelector"
import { CopyButton } from "@/components/ui/copy-button"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useAuth } from "@/hooks/useAuth"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { useSelfUpdate } from "@/hooks/useSelfUpdate"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { api } from "@/lib/api"

import { withBasePath } from "@/lib/base-url"
import { canRegisterProtocolHandler, getMagnetHandlerRegistrationGuidance, registerMagnetHandler } from "@/lib/protocol-handler"
import { formatBytes, formatDuration } from "@/lib/utils"
import type { SettingsSearch } from "@/routes/_authenticated/settings"
import type { Instance, TorznabSearchCacheStats, User } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ExternalLink, Loader2, Plus, RefreshCw } from "lucide-react"
import type { FormEvent, ReactNode } from "react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const TORZNAB_CACHE_MIN_TTL_MINUTES = 1440

function LanguageSelector() {
  const { i18n } = useTranslation("settings")

  return (
    <div className="space-y-2">
      <Select
        value={i18n.language}
        onValueChange={(value) => changeLanguage(value as AppLanguage)}
      >
        <SelectTrigger className="w-[200px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {supportedLanguages.map((lng) => (
            <SelectItem key={lng} value={lng}>
              {languageNames[lng]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

function ChangePasswordForm() {
  const { t } = useTranslation("settings")
  const { setIsAuthenticated } = useAuth()
  const mutation = useMutation({
    mutationFn: async (data: { currentPassword: string; newPassword: string }) => {
      return api.changePassword(data.currentPassword, data.newPassword)
    },
    onSuccess: () => {
      toast.success(t("changePassword.toasts.success"))
      // The server ended every session, this one included. Dropping the
      // cached user sends the authenticated layout back to the login page.
      setIsAuthenticated(false)
    },
    onError: () => {
      toast.error(t("changePassword.toasts.error"))
    },
  })

  const form = useForm({
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        currentPassword: value.currentPassword,
        newPassword: value.newPassword,
      })
    },
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-4"
    >
      <form.Field
        name="currentPassword"
        validators={{
          onChange: ({ value }) => !value ? t("changePassword.validation.currentRequired") : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="currentPassword">{t("changePassword.currentPassword")}</Label>
            <Input
              id="currentPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="newPassword"
        validators={{
          onChange: ({ value }) => {
            if (!value) return t("changePassword.validation.newRequired")
            if (value.length < 8) return t("changePassword.validation.minLength")
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="newPassword">{t("changePassword.newPassword")}</Label>
            <Input
              id="newPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="confirmPassword"
        validators={{
          onChange: ({ value, fieldApi }) => {
            const newPassword = fieldApi.form.getFieldValue("newPassword")
            if (!value) return t("changePassword.validation.confirmRequired")
            if (value !== newPassword) return t("changePassword.validation.mismatch")
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="confirmPassword">{t("changePassword.confirmPassword")}</Label>
            <Input
              id="confirmPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || mutation.isPending}
          >
            {isSubmitting || mutation.isPending ? t("changePassword.submitting") : t("changePassword.submit")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}

interface InstancesManagerProps {
  search: SettingsSearch
  onSearchChange: (search: SettingsSearch) => void
}

const INSTANCE_FORM_ID = "instance-form"

function InstancesManager({ search, onSearchChange }: InstancesManagerProps) {
  const { t } = useTranslation("settings")
  const { instances, isLoading, reorderInstances, isReordering, isCreating } = useInstances()
  const [titleBarSpeedsEnabled, setTitleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)
  const isDialogOpen = search.tab === "instances" && search.modal === "add-instance"
  const [editingInstanceId, setEditingInstanceId] = useState<number | null>(null)
  const editingInstance = instances?.find(instance => instance.id === editingInstanceId)

  // Close edit dialog if instance was deleted
  useEffect(() => {
    if (editingInstanceId !== null && !editingInstance && !isLoading) {
      setEditingInstanceId(null)
    }
  }, [editingInstanceId, editingInstance, isLoading])

  const handleOpenAddDialog = () => {
    onSearchChange({ ...search, tab: "instances", modal: "add-instance" })
  }

  const handleCloseDialog = () => {
    onSearchChange({ tab: "instances" })
  }

  const handleEditInstance = (instance: Instance) => {
    setEditingInstanceId(instance.id)
  }

  const handleReorder = (instanceId: number, direction: -1 | 1) => {
    if (!instances || isReordering) return

    const currentIndex = instances.findIndex(instance => instance.id === instanceId)
    if (currentIndex === -1) return

    const targetIndex = currentIndex + direction
    if (targetIndex < 0 || targetIndex >= instances.length) return

    const orderedIds = instances.map(instance => instance.id)
    const [moved] = orderedIds.splice(currentIndex, 1)
    orderedIds.splice(targetIndex, 0, moved)

    reorderInstances(orderedIds, {
      onError: (error) => {
        toast.error(t("instances.toasts.reorderFailed"), {
          description: error instanceof Error ? error.message : undefined,
        })
      },
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-stretch gap-2 sm:flex-row sm:justify-end">
        <Button onClick={handleOpenAddDialog} size="sm" className="w-full sm:w-auto">
          <Plus className="mr-2 h-4 w-4" />
          {t("instances.addButton")}
        </Button>
      </div>

      <PasswordIssuesBanner instances={instances || []} />

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-center text-sm text-muted-foreground py-8">
            {t("instances.loading")}
          </p>
        ) : (
          <>
            {instances && instances.length > 0 ? (
              <div className="grid gap-4 lg:grid-cols-2">
                {instances.map((instance, index) => (
                  <InstanceCard
                    key={instance.id}
                    instance={instance}
                    onEdit={() => handleEditInstance(instance)}
                    onMoveUp={index > 0 ? () => handleReorder(instance.id, -1) : undefined}
                    onMoveDown={index < instances.length - 1 ? () => handleReorder(instance.id, 1) : undefined}
                    disableMoveUp={isReordering}
                    disableMoveDown={isReordering}
                  />
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed p-12 text-center">
                <p className="text-muted-foreground">{t("instances.empty")}</p>
                <Button
                  onClick={handleOpenAddDialog}
                  className="mt-4"
                  variant="outline"
                >
                  <Plus className="mr-2 h-4 w-4" />
                  {t("instances.addFirst")}
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      <div className="rounded-lg border p-4">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-1">
            <Label className="text-sm font-medium">{t("instances.titleBarSpeeds.label")}</Label>
            <p className="text-xs text-muted-foreground">
              {t("instances.titleBarSpeeds.description")}
            </p>
          </div>
          <Switch
            checked={titleBarSpeedsEnabled}
            onCheckedChange={(checked) => setTitleBarSpeedsEnabled(Boolean(checked))}
          />
        </div>
      </div>

      <Dialog open={isDialogOpen} onOpenChange={(open) => open ? handleOpenAddDialog() : handleCloseDialog()}>
        <DialogContent className="sm:max-w-[425px] max-h-[90dvh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>{t("instances.addDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("instances.addDialog.description")}
            </DialogDescription>
            <MigrationHint />
          </DialogHeader>
          <div className="flex-1 overflow-y-auto min-h-0">
            <InstanceForm
              onSuccess={handleCloseDialog}
              onCancel={handleCloseDialog}
              formId={INSTANCE_FORM_ID}
            />
          </div>
          <DialogFooter className="flex-shrink-0">
            <Button type="button" variant="outline" onClick={handleCloseDialog}>
              {t("instances.addDialog.cancel")}
            </Button>
            <Button type="submit" form={INSTANCE_FORM_ID} disabled={isCreating}>
              {isCreating ? t("instances.addDialog.adding") : t("instances.addDialog.submit")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Instance Preferences Dialog */}
      {editingInstanceId && editingInstance && (
        <InstancePreferencesDialog
          open={true}
          onOpenChange={(open) => !open && setEditingInstanceId(null)}
          instanceId={editingInstance.id}
          instanceName={editingInstance.name}
          instance={editingInstance}
        />
      )}
    </div>
  )
}

function TorznabSearchCachePanel() {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const statsQuery = useQuery({
    queryKey: ["torznab", "search-cache", "stats"],
    queryFn: () => api.getTorznabSearchCacheStats(),
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const { formatDate } = useDateTimeFormatters()

  const stats: TorznabSearchCacheStats | undefined = statsQuery.data
  const [ttlInput, setTtlInput] = useState("")

  const formatCacheTimestamp = useCallback((value?: string | null) => {
    if (!value) {
      return "—"
    }
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) {
      return "—"
    }
    return formatDate(parsed)
  }, [formatDate])

  useEffect(() => {
    if (stats?.ttlMinutes !== undefined) {
      setTtlInput(String(stats.ttlMinutes))
    }
  }, [stats?.ttlMinutes])

  const updateTTLMutation = useMutation({
    mutationFn: async (nextTTL: number) => {
      return api.updateTorznabSearchCacheSettings(nextTTL)
    },
    onSuccess: (updatedStats) => {
      toast.success(t("searchCache.toasts.ttlUpdated", { minutes: updatedStats.ttlMinutes }))
      setTtlInput(String(updatedStats.ttlMinutes))
      queryClient.setQueryData(["torznab", "search-cache", "stats"], updatedStats)
      queryClient.invalidateQueries({
        queryKey: ["torznab", "search-cache"],
        exact: false,
      })
    },
    onError: (error: unknown) => {
      const message = error instanceof Error ? error.message : t("searchCache.toasts.ttlUpdateFailed")
      toast.error(message)
    },
  })

  const handleUpdateTTL = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const parsed = Number(ttlInput)
    if (!Number.isFinite(parsed)) {
      toast.error(t("searchCache.toasts.invalidNumber"))
      return
    }
    const normalized = Math.floor(parsed)
    if (normalized < TORZNAB_CACHE_MIN_TTL_MINUTES) {
      toast.error(t("searchCache.toasts.ttlMinimum", { min: TORZNAB_CACHE_MIN_TTL_MINUTES }))
      return
    }
    updateTTLMutation.mutate(normalized)
  }

  const ttlMinutes = stats?.ttlMinutes ?? 0
  const approxSize = stats?.approxSizeBytes ?? 0

  const cacheStatusText = stats?.enabled ? t("searchCache.enabled") : t("searchCache.disabled")

  const rows = useMemo(
    () => [
      { label: t("searchCache.entries"), value: stats?.entries?.toLocaleString() ?? "0" },
      { label: t("searchCache.hitCount"), value: stats?.totalHits?.toLocaleString() ?? "0" },
      { label: t("searchCache.approxSize"), value: approxSize > 0 ? formatBytes(approxSize) : "—" },
      { label: t("searchCache.ttl"), value: ttlMinutes > 0 ? t("searchCache.ttlValue", { minutes: ttlMinutes }) : "—" },
      { label: t("searchCache.newestEntry"), value: formatCacheTimestamp(stats?.newestCachedAt) },
      { label: t("searchCache.lastUsed"), value: formatCacheTimestamp(stats?.lastUsedAt) },
    ],
    [t, approxSize, formatCacheTimestamp, stats?.entries, stats?.lastUsedAt, stats?.newestCachedAt, stats?.totalHits, ttlMinutes]
  )

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <CardTitle>{t("searchCache.title")}</CardTitle>
            <CardDescription>{t("searchCache.description")}</CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={stats?.enabled ? "default" : "secondary"}>{cacheStatusText}</Badge>
            <Button
              variant="outline"
              size="sm"
              onClick={() => statsQuery.refetch()}
              disabled={statsQuery.isFetching}
            >
              <RefreshCw className={`mr-2 h-4 w-4 ${statsQuery.isFetching ? "animate-spin" : ""}`} />
              {t("searchCache.refreshStats")}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="grid gap-4 sm:grid-cols-2">
          {rows.map(row => (
            <div key={row.label} className="space-y-1 rounded-lg border p-3 bg-muted/40">
              <p className="text-xs uppercase text-muted-foreground">{row.label}</p>
              <p className="text-lg font-semibold">{row.value}</p>
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t("searchCache.configTitle")}</CardTitle>
          <CardDescription>{t("searchCache.configDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleUpdateTTL} className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="torznab-cache-ttl">{t("searchCache.cacheTTL")}</Label>
              <div className="flex flex-col gap-2 sm:flex-row">
                <Input
                  id="torznab-cache-ttl"
                  type="number"
                  min={TORZNAB_CACHE_MIN_TTL_MINUTES}
                  value={ttlInput}
                  onChange={(event) => setTtlInput(event.target.value)}
                  disabled={updateTTLMutation.isPending}
                />
                <Button type="submit" disabled={updateTTLMutation.isPending}>
                  {updateTTLMutation.isPending ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      {t("searchCache.saving")}
                    </>
                  ) : (
                    t("searchCache.saveTTL")
                  )}
                </Button>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("searchCache.ttlHelper", { min: TORZNAB_CACHE_MIN_TTL_MINUTES })}
            </p>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

function formatRelativeDate(value: string | undefined, t: (key: string, options?: Record<string, string>) => string): string {
  if (!value || value.trim() === "") {
    return "—"
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return "—"
  }

  const secondsDiff = Math.floor((Date.now() - date.getTime()) / 1000)
  if (Math.abs(secondsDiff) < 1) {
    return t("searchCache.timestamps.justNow")
  }

  const duration = formatDuration(Math.abs(secondsDiff))
  if (secondsDiff >= 0) {
    return t("searchCache.timestamps.ago", { duration })
  }

  return t("searchCache.timestamps.inFuture", { duration })
}

function formatCurrentSessionAuth(user: User | undefined, t: (key: string) => string): string {
  if (!user) {
    return t("application.auth.unknown")
  }

  const methodRaw = user.auth_method?.trim() || ""
  const method = methodRaw !== "" ? methodRaw : "builtin"
  const username = user.username?.trim() || ""

  if (username !== "") {
    return `${method} (${username})`
  }

  return method
}

function isDevVersion(version?: string): boolean {
  const value = version?.trim().toLowerCase() || ""
  return value === "0.0.0-dev" || value.includes("dev") || value === "main"
}

function getLiveUptimeSeconds(baseUptime: number, startedAtMs: number): number {
  const elapsed = Math.floor((Date.now() - startedAtMs) / 1000)
  return Math.max(0, baseUptime + elapsed)
}

type ApplicationField = {
  label: string
  value: string
  secondary?: string
  copyValue?: string
  monospace?: boolean
  action?: ReactNode
}

interface ApplicationSectionProps {
  title: string
  description: string
  fields: ApplicationField[]
  headerAction?: ReactNode
}

function ApplicationSection({ title, description, fields, headerAction }: ApplicationSectionProps) {
  return (
    <Card>
      <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </div>
        {headerAction}
      </CardHeader>
      <CardContent className="p-0">
        <dl className="divide-y">
          {fields.map((field) => (
            <div key={field.label} className="group px-4 py-3 sm:px-6">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                <dt className="text-xs uppercase text-muted-foreground sm:w-44 sm:shrink-0">{field.label}</dt>
                <dd className="min-w-0 flex-1">
                  <div className="flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <p
                        className={`${field.monospace ? "font-mono text-xs sm:text-sm" : "text-sm font-medium"} break-all`}
                        title={field.value}
                      >
                        {field.value}
                      </p>
                      {field.secondary && (
                        <p className="mt-1 text-xs text-muted-foreground">{field.secondary}</p>
                      )}
                    </div>
                    {field.action}
                    {field.copyValue && (
                      <CopyButton
                        value={field.copyValue}
                        label={field.label}
                        className="h-7 w-7 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                      />
                    )}
                  </div>
                </dd>
              </div>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}

function ApplicationInfoPanel({ openUpdate, onUpdateOpened }: { openUpdate: boolean; onUpdateOpened: () => void }) {
  const { t } = useTranslation("settings")
  const { formatISOTimestamp, formatTimestamp } = useDateTimeFormatters()
  const appInfoQuery = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
  })

  const currentUserQuery = useQuery({
    queryKey: ["auth-me", "application-tab"],
    queryFn: () => api.checkAuth(),
    staleTime: 60 * 1000,
  })

  const latestVersionQuery = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    staleTime: 5 * 60 * 1000,
  })

  const selfUpdate = useSelfUpdate()
  const { setConfirmOpen } = selfUpdate

  useEffect(() => {
    if (openUpdate && latestVersionQuery.data) {
      setConfirmOpen(true)
      onUpdateOpened()
    }
  }, [openUpdate, latestVersionQuery.data, setConfirmOpen, onUpdateOpened])

  const info = appInfoQuery.data
  const user = currentUserQuery.data

  const [liveUptimeSeconds, setLiveUptimeSeconds] = useState(0)

  useEffect(() => {
    if (!info) {
      setLiveUptimeSeconds(0)
      return
    }

    const baseUptime = Math.max(0, info.uptimeSeconds)
    const startedAtMs = Date.now()
    setLiveUptimeSeconds(baseUptime)

    const timer = window.setInterval(() => {
      setLiveUptimeSeconds(getLiveUptimeSeconds(baseUptime, startedAtMs))
    }, 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [info])

  let currentSessionAuth = formatCurrentSessionAuth(user, t)
  if (currentUserQuery.isLoading) {
    currentSessionAuth = t("application.auth.loading")
  } else if (currentUserQuery.isError) {
    currentSessionAuth = t("application.auth.unavailable")
  }

  const updateStatus = useMemo(() => {
    if (!info) {
      return { label: t("application.build.statuses.unknown"), detail: t("application.build.statuses.unknownDetail") }
    }
    if (!info.checkForUpdates) {
      return { label: t("application.build.statuses.disabled"), detail: t("application.build.statuses.disabledDetail") }
    }
    if (isDevVersion(info.version)) {
      return { label: t("application.build.statuses.devBuild"), detail: "" }
    }
    if (latestVersionQuery.isLoading || latestVersionQuery.isFetching) {
      return { label: t("application.build.statuses.checking"), detail: t("application.build.statuses.checkingDetail") }
    }
    if (latestVersionQuery.data) {
      return { label: t("application.build.statuses.updateAvailable"), detail: latestVersionQuery.data.tag_name }
    }
    return { label: t("application.build.statuses.upToDate"), detail: t("application.build.statuses.upToDateDetail") }
  }, [t, info, latestVersionQuery.data, latestVersionQuery.isFetching, latestVersionQuery.isLoading])

  const updateCheckedAt = latestVersionQuery.dataUpdatedAt > 0 ? formatTimestamp(latestVersionQuery.dataUpdatedAt / 1000) : t("application.build.statuses.notCheckedYet")

  const updateControl = latestVersionQuery.data && (
    <InstallUpdateButton selfUpdate={selfUpdate} release={latestVersionQuery.data} className="shrink-0" />
  )

  const buildFields: ApplicationField[] = info ? [
    { label: t("application.build.version"), value: info.version || "—", monospace: true },
    { label: t("application.build.commit"), value: info.commitShort || info.commit || "—", copyValue: info.commit || "", monospace: true },
    {
      label: t("application.build.buildDate"),
      value: info.buildDate?.trim() ? formatISOTimestamp(info.buildDate) : "—",
      secondary: formatRelativeDate(info.buildDate, t),
    },
    {
      label: t("application.build.updateStatus"),
      value: updateStatus.label,
      secondary: [updateStatus.detail, t("application.build.statuses.lastChecked", { date: updateCheckedAt })].filter(Boolean).join(" • "),
      action: updateControl,
    },
  ] : []

  const runtimeFields: ApplicationField[] = info ? [
    { label: t("application.runtime.uptime"), value: formatDuration(liveUptimeSeconds) },
    { label: t("application.runtime.runtime"), value: `${info.goVersion} • ${info.goOS}/${info.goArch}`, monospace: true },
  ] : []

  const authFields: ApplicationField[] = info ? [
    { label: t("application.auth.currentSessionAuth"), value: currentSessionAuth, monospace: true },
    { label: t("application.auth.oidcEnabled"), value: info.oidcEnabled ? t("application.auth.yes") : t("application.auth.no") },
    { label: t("application.auth.builtInLoginEnabled"), value: info.builtInLoginEnabled ? t("application.auth.yes") : t("application.auth.no") },
    { label: t("application.auth.oidcIssuerHost"), value: info.oidcIssuerHost || "—", monospace: true },
  ] : []

  const storageFields: ApplicationField[] = info ? [
    {
      label: t("application.storage.database"),
      value: `${info.database.engine}${info.database.target ? ` (${info.database.target})` : ""}`,
      monospace: true,
    },
    { label: t("application.storage.bind"), value: `${info.host}:${info.port}${info.baseUrl}`, monospace: true },
    { label: t("application.storage.configDir"), value: info.configDir || "—", copyValue: info.configDir || "", monospace: true },
    { label: t("application.storage.dataDir"), value: info.dataDir || "—", copyValue: info.dataDir || "", monospace: true },
  ] : []


  return (
    <div className="space-y-4">
      {appInfoQuery.isLoading && (
        <Card>
          <CardContent className="py-8">
            <div className="flex items-center justify-center text-sm text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t("application.loading")}
            </div>
          </CardContent>
        </Card>
      )}

      {appInfoQuery.isError && (
        <Card>
          <CardContent className="py-6">
            <p className="text-sm text-destructive">
              {appInfoQuery.error instanceof Error ? appInfoQuery.error.message : t("application.loadFailed")}
            </p>
          </CardContent>
        </Card>
      )}

      {appInfoQuery.isError && !info && updateControl}

      {info && (
        <>
          <ApplicationSection
            title={t("application.build.title")}
            description={t("application.build.description")}
            fields={buildFields}
            headerAction={(
              <div className="flex gap-2">
                <RestartButton />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    void appInfoQuery.refetch()
                    void latestVersionQuery.refetch()
                    void currentUserQuery.refetch()
                  }}
                  disabled={appInfoQuery.isFetching || latestVersionQuery.isFetching || currentUserQuery.isFetching}
                >
                  <RefreshCw className={`mr-2 h-4 w-4 ${(appInfoQuery.isFetching || latestVersionQuery.isFetching || currentUserQuery.isFetching) ? "animate-spin" : ""}`} />
                  {t("application.build.refresh")}
                </Button>
              </div>
            )}
          />
          <ApplicationSection
            title={t("application.runtime.title")}
            description={t("application.runtime.description")}
            fields={runtimeFields}
          />
          <ApplicationSection
            title={t("application.auth.title")}
            description={t("application.auth.description")}
            fields={authFields}
          />
          <ApplicationSection
            title={t("application.storage.title")}
            description={t("application.storage.description")}
            fields={storageFields}
          />
        </>
      )}
      <RestartOverlay state={selfUpdate.overlay} update={selfUpdate.update} />
    </div>
  )
}

interface SettingsProps {
  search: SettingsSearch
  onSearchChange: (search: SettingsSearch) => void
}

export function Settings({ search, onSearchChange }: SettingsProps) {
  const { t } = useTranslation("settings")
  const activeTab: SettingsTab = search.tab ?? DEFAULT_SETTINGS_TAB
  const navGroups: SectionNavGroup<SettingsTab>[] = SETTINGS_NAV_GROUPS.map(group => ({
    label: "labelKey" in group ? t(group.labelKey) : undefined,
    sections: group.tabs.map(entry => ({ value: entry.tab, label: t(entry.labelKey) })),
  }))
  const appInfoQuery = useQuery({
    queryKey: ["application-info"],
    queryFn: () => api.getApplicationInfo(),
    staleTime: 30 * 1000,
  })

  const handleTabChange = (tab: SettingsTab) => {
    onSearchChange({ tab })
  }

  return (
    <div className="space-y-6 p-4 lg:p-6 pb-16">
      <PageHeader title={t("title")} description={t("description")} />

      <SectionedPage navLabel={t("title")} groups={navGroups} activeSection={activeTab} onSectionChange={handleTabChange}>
        {activeTab === "application" && (
          <ApplicationInfoPanel openUpdate={search.modal === "install-update"} onUpdateOpened={() => onSearchChange({ ...search, modal: undefined })} />
        )}

        {activeTab === "instances" && (
          <Card>
            <CardHeader>
              <CardTitle>{t("instances.title")}</CardTitle>
              <CardDescription>
                {t("instances.description")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <InstancesManager search={search} onSearchChange={onSearchChange} />
            </CardContent>
          </Card>
        )}

        {activeTab === "indexers" && (
          <>
            <IndexersPage withContainer={false} />
            <GazelleSettingsCard />
          </>
        )}

        {activeTab === "search-cache" && (
          <TorznabSearchCachePanel />
        )}

        {activeTab === "integrations" && (
          <Card>
            <CardHeader>
              <CardTitle>{t("integrations.title")}</CardTitle>
              <CardDescription>
                {t("integrations.description")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ArrInstancesManager />
            </CardContent>
          </Card>
        )}

        {activeTab === "client-api" && (
          <Card>
            <CardHeader>
              <CardTitle>{t("clientApiKeys.cardTitle")}</CardTitle>
              <CardDescription>
                {t("clientApiKeys.cardDescription")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ClientApiKeysManager />
            </CardContent>
          </Card>
        )}

        {activeTab === "api" && (
          <Card>
            <CardHeader>
              <div className="flex items-start justify-between">
                <div className="space-y-1.5">
                  <CardTitle>{t("apiKeys.cardTitle")}</CardTitle>
                  <CardDescription>
                    {t("apiKeys.cardDescription")}
                  </CardDescription>
                </div>
                <a
                  href={withBasePath("api/docs")}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
                  title={t("apiKeys.apiDocs")}
                >
                  <span className="hidden sm:inline">{t("apiKeys.apiDocs")}</span>
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
              </div>
            </CardHeader>
            <CardContent>
              <ApiKeysManager authMode={appInfoQuery.data?.authMode} authModeLoading={appInfoQuery.isLoading} />
            </CardContent>
          </Card>
        )}

        {activeTab === "external-programs" && (
          <Card>
            <CardHeader>
              <CardTitle>{t("externalPrograms.cardTitle")}</CardTitle>
              <CardDescription>
                {t("externalPrograms.cardDescription")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <ExternalProgramsManager />
            </CardContent>
          </Card>
        )}

        {activeTab === "notifications" && (
          <Card>
            <CardHeader>
              <CardTitle>{t("notifications.cardTitle")}</CardTitle>
              <CardDescription>
                {t("notifications.cardDescription")}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <NotificationsManager />
            </CardContent>
          </Card>
        )}

        {activeTab === "datetime" && (
          <>
            <Card>
              <CardHeader>
                <CardTitle>{t("language.cardTitle")}</CardTitle>
                <CardDescription>
                  {t("language.cardDescription")}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <LanguageSelector />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>{t("dateTime.cardTitle")}</CardTitle>
                <CardDescription>
                  {t("dateTime.cardDescription")}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <DateTimePreferencesForm />
              </CardContent>
            </Card>
          </>
        )}

        {activeTab === "themes" && (
          <>
            <ThemeSelector />
            <div id="license-manager">
              <LicenseManager
                checkoutStatus={search.checkout}
                checkoutPaymentStatus={search.status}
                onCheckoutConsumed={() => onSearchChange({ tab: "themes" })}
              />
            </div>
          </>
        )}

        {activeTab === "security" && (
          <>
            <Card>
              <CardHeader>
                <CardTitle>{t("security.changePassword.title")}</CardTitle>
                <CardDescription>
                  {t("security.changePassword.description")}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <ChangePasswordForm />
              </CardContent>
            </Card>

            {canRegisterProtocolHandler() && (
              <Card>
                <CardHeader>
                  <CardTitle>{t("security.browserIntegration.title")}</CardTitle>
                  <CardDescription>
                    {t("security.browserIntegration.description")}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <p className="text-sm text-muted-foreground">
                      {t("security.browserIntegration.registerDescription")}
                    </p>
                    <Button
                      variant="secondary"
                      onClick={() => {
                        const success = registerMagnetHandler()
                        if (success) {
                          toast.success(t("security.browserIntegration.toasts.requested"), {
                            description: getMagnetHandlerRegistrationGuidance(),
                          })
                        } else {
                          toast.error(t("security.browserIntegration.toasts.failed"))
                        }
                      }}
                      className="w-fit"
                    >
                      {t("security.browserIntegration.registerButton")}
                    </Button>
                  </div>
                </CardContent>
              </Card>
            )}
          </>
        )}

        {activeTab === "logs" && (
          <LogSettingsPanel />
        )}
      </SectionedPage>
    </div>
  )
}
