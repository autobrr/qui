/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { buildCategoryTree, type CategoryNode } from "@/components/torrents/CategoryTree"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { buildTrackerCustomizationMaps, useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { pickTrackerIconDomain } from "@/lib/tracker-icons"
import { cn, copyTextToClipboard, formatErrorReason, normalizeTrackerDomains } from "@/lib/utils"
import { REANNOUNCE_CONSTRAINTS, type InstanceFormData, type InstanceReannounceActivity, type InstanceReannounceSettings } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, HardDrive, Info, RefreshCcw } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface TrackerReannounceFormProps {
  instanceId: number
  onInstanceChange?: (instanceId: number) => void
  onSuccess?: () => void
  /** Render variant: "card" wraps in Card component, "embedded" renders without card wrapper */
  variant?: "card" | "embedded"
  /** Form ID for external submit button. When provided, the internal submit button is hidden. */
  formId?: string
}

const DEFAULT_SETTINGS: InstanceReannounceSettings = {
  enabled: false,
  initialWaitSeconds: 15,
  reannounceIntervalSeconds: 7,
  maxAgeSeconds: 600,
  maxRetries: 50,
  aggressive: false,
  monitorAll: false,
  excludeCategories: false,
  categories: [],
  excludeTags: false,
  tags: [],
  excludeTrackers: false,
  trackers: [],
}

const GLOBAL_SCAN_INTERVAL_SECONDS = 7

type MonitorScopeField = keyof Pick<InstanceReannounceSettings, "categories" | "tags" | "trackers">

export function TrackerReannounceForm({ instanceId, onInstanceChange, onSuccess, variant = "card", formId }: TrackerReannounceFormProps) {
  const { instances, updateInstance, isUpdating } = useInstances()
  const { formatISOTimestamp } = useDateTimeFormatters()
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const instance = useMemo(() => instances?.find((item) => item.id === instanceId), [instances, instanceId])
  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )
  const [settings, setSettings] = useState<InstanceReannounceSettings>(() => cloneSettings(instance?.reannounceSettings))
  const [hideSkipped, setHideSkipped] = useState(true)
  const [activeTab, setActiveTab] = useState("settings")

  // Reset settings when instance changes
  useEffect(() => {
    setSettings(cloneSettings(instance?.reannounceSettings))
  }, [instanceId, instance?.reannounceSettings])

  const trackersQuery = useInstanceTrackers(instanceId, { enabled: !!instance })
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()

  const categoriesQuery = useQuery({
    queryKey: ["instance-categories", instanceId],
    queryFn: () => api.getCategories(instanceId),
    enabled: !!instance,
    staleTime: 1000 * 60 * 5,
  })

  const tagsQuery = useQuery({
    queryKey: ["instance-tags", instanceId],
    queryFn: () => api.getTags(instanceId),
    enabled: !!instance,
    staleTime: 1000 * 60 * 5,
  })

  const trackerCustomizationMaps = useMemo(
    () => buildTrackerCustomizationMaps(trackerCustomizations),
    [trackerCustomizations]
  )

  // Process trackers to apply customizations (nicknames and merged domains)
  const trackerOptions: Option[] = useMemo(() => {
    if (!trackersQuery.data) return []

    const { domainToCustomization } = trackerCustomizationMaps
    const trackers = Object.keys(trackersQuery.data)
    const processed: Option[] = []
    const seenDisplayNames = new Set<string>()

    for (const tracker of trackers) {
      const lowerTracker = tracker.toLowerCase()

      const customization = domainToCustomization.get(lowerTracker)

      if (customization) {
        const displayKey = customization.displayName.toLowerCase()
        if (seenDisplayNames.has(displayKey)) continue
        seenDisplayNames.add(displayKey)

        const iconDomain = pickTrackerIconDomain(trackerIcons, customization.domains)
        processed.push({
          label: customization.displayName,
          value: customization.domains.join(","),
          icon: <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />,
        })
      } else {
        if (seenDisplayNames.has(lowerTracker)) continue
        seenDisplayNames.add(lowerTracker)

        processed.push({
          label: tracker,
          value: tracker,
          icon: <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} />,
        })
      }
    }

    processed.sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))

    return processed
  }, [trackersQuery.data, trackerCustomizationMaps, trackerIcons])

  const selectedTrackerValues = useMemo(() => {
    const { domainToCustomization } = trackerCustomizationMaps
    const result: string[] = []
    const seen = new Set<string>()

    for (const domain of normalizeTrackerDomains(settings.trackers)) {
      const customization = domainToCustomization.get(domain)
      const value = customization ? customization.domains.join(",") : domain
      if (seen.has(value)) continue
      seen.add(value)
      result.push(value)
    }

    return result
  }, [settings.trackers, trackerCustomizationMaps])

  const categoryOptions: Option[] = useMemo(() => {
    if (!categoriesQuery.data) return []

    // Build tree and flatten with level info for indentation
    const tree = buildCategoryTree(categoriesQuery.data, {})
    const flattened: Option[] = []

    const visitNodes = (nodes: CategoryNode[]) => {
      for (const node of nodes) {
        flattened.push({
          label: node.name,
          value: node.name,
        })
        visitNodes(node.children)
      }
    }

    visitNodes(tree)
    return flattened
  }, [categoriesQuery.data])

  const tagOptions: Option[] = useMemo(() => {
    if (!tagsQuery.data) return []
    return tagsQuery.data
      .map((tag) => ({
        label: tag,
        value: tag,
      }))
      .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: "base" }))
  }, [tagsQuery.data])

  const appendUniqueValue = (field: MonitorScopeField, rawValue: string) => {
    const trimmed = rawValue.trim()
    if (!trimmed) return
    const normalized = trimmed.toLowerCase()
    setSettings((prev) => {
      const values = prev[field]
      if (field === "trackers") {
        const next = normalizeTrackerDomains([...values, trimmed])
        return {
          ...prev,
          trackers: next,
        }
      }
      if (values.some((entry) => entry.toLowerCase() === normalized)) {
        return prev
      }
      return {
        ...prev,
        [field]: [...values, trimmed],
      }
    })
  }

  const persistSettings = (nextSettings: InstanceReannounceSettings, successMessage = tr("trackerReannounceForm.toasts.settingsSaved")) => {
    if (!instance) {
      toast.error(tr("trackerReannounceForm.toasts.instanceMissingTitle"), {
        description: tr("trackerReannounceForm.toasts.instanceMissingDescription"),
      })
      return
    }

    const sanitized = sanitizeSettings(nextSettings)
    const payload: Partial<InstanceFormData> = {
      name: instance.name,
      host: instance.host,
      username: instance.username,
      tlsSkipVerify: instance.tlsSkipVerify,
      reannounceSettings: sanitized,
    }

    if (instance.basicUsername !== undefined) {
      payload.basicUsername = instance.basicUsername
    }

    updateInstance(
      { id: instanceId, data: payload },
      {
        onSuccess: () => {
          toast.success(tr("trackerReannounceForm.toasts.monitoringUpdated"), { description: successMessage })
          onSuccess?.()
        },
        onError: (error) => {
          toast.error(tr("trackerReannounceForm.toasts.updateFailed"), {
            description: error instanceof Error ? error.message : tr("trackerReannounceForm.toasts.unableUpdateSettings"),
          })
        },
      }
    )
  }

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    persistSettings(settings)
  }

  const handleToggleEnabled = (enabled: boolean) => {
    const nextSettings = { ...settings, enabled }
    setSettings(nextSettings)

    if (!enabled) {
      persistSettings(nextSettings, tr("trackerReannounceForm.toasts.monitoringDisabled"))
    }
  }

  // Only fetch activity in card mode (embedded mode shows activity in overview)
  const activityQuery = useQuery({
    queryKey: ["instance-reannounce-activity", instanceId],
    queryFn: () => api.getInstanceReannounceActivity(instanceId, 100),
    enabled: variant !== "embedded" && Boolean(instance && settings.enabled),
    refetchInterval: activeTab === "activity" ? 5000 : false,
  })

  if (!instance) {
    return <p className="text-sm text-muted-foreground">{tr("trackerReannounceForm.states.instanceNotFound")}</p>
  }

  // Filter and limit to 50 events for display
  const allActivityEvents: InstanceReannounceActivity[] = (activityQuery.data ?? []).slice(-50).reverse()
  const activityEvents = hideSkipped? (activityQuery.data ?? []).filter((event) => event.outcome !== "skipped").slice(-50).reverse(): allActivityEvents
  const activityEnabled = Boolean(instance && settings.enabled)

  const outcomeClasses: Record<InstanceReannounceActivity["outcome"], string> = {
    succeeded: "bg-emerald-500/10 text-emerald-500 border-emerald-500/20",
    failed: "bg-destructive/10 text-destructive border-destructive/30",
    skipped: "bg-muted text-muted-foreground border-border/60",
  }

  const outcomeLabels: Record<InstanceReannounceActivity["outcome"], string> = {
    succeeded: tr("trackerReannounceForm.activity.outcomes.succeeded"),
    failed: tr("trackerReannounceForm.activity.outcomes.failed"),
    skipped: tr("trackerReannounceForm.activity.outcomes.skipped"),
  }

  const headerContent = (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <h3 className={cn(variant === "card" ? "text-lg font-semibold" : "text-base font-medium")}>
              {variant === "card"
                ? tr("trackerReannounceForm.header.title")
                : tr("trackerReannounceForm.tabs.settings")}
            </h3>
            {variant === "card" && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-4 w-4 text-muted-foreground cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-[300px]">
                  <p>{tr("trackerReannounceForm.header.tooltip")}</p>
                </TooltipContent>
              </Tooltip>
            )}
          </div>
          {variant === "card" && (
            <p className="text-sm text-muted-foreground">
              {tr("trackerReannounceForm.header.descriptionPrefix")} <strong>{tr("trackerReannounceForm.header.stalled")}</strong>{" "}
              {tr("trackerReannounceForm.header.descriptionSuffix")}{" "}
              {tr("trackerReannounceForm.header.scanInterval", { seconds: GLOBAL_SCAN_INTERVAL_SECONDS })}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2 bg-muted/50 p-2 rounded-lg border shrink-0">
          <Label htmlFor="tracker-monitoring" className="font-medium text-sm cursor-pointer">
            {settings.enabled
              ? tr("trackerReannounceForm.values.enabled")
              : tr("trackerReannounceForm.values.disabled")}
          </Label>
          <Switch
            id="tracker-monitoring"
            checked={settings.enabled}
            onCheckedChange={handleToggleEnabled}
            disabled={isUpdating}
          />
        </div>
      </div>

      {variant === "card" && activeInstances.length > 1 && onInstanceChange && (
        <div className="flex items-center gap-3 pt-2 border-t border-border/40">
          <Label className="text-sm text-muted-foreground shrink-0">{tr("trackerReannounceForm.values.instance")}</Label>
          <Select
            value={String(instanceId)}
            onValueChange={(value) => onInstanceChange?.(Number(value))}
            disabled={!onInstanceChange}
          >
            <SelectTrigger className="w-[200px]">
              <div className="flex items-center gap-2 min-w-0 overflow-hidden">
                <HardDrive className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                <span className="truncate">
                  <SelectValue placeholder={tr("trackerReannounceForm.placeholders.selectInstance")} />
                </span>
              </div>
            </SelectTrigger>
            <SelectContent>
              {activeInstances.map((inst) => (
                <SelectItem key={inst.id} value={String(inst.id)}>
                  <span className="truncate">{inst.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}
    </div>
  )

  const settingsContent = (
    <div className="space-y-6">
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
            {tr("trackerReannounceForm.sections.timingAndBehavior")}
          </h3>
          <Separator className="flex-1" />
        </div>

        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
          <NumberField
            id="initial-wait"
            label={tr("trackerReannounceForm.fields.initialWait.label")}
            description={tr("trackerReannounceForm.fields.initialWait.description")}
            tooltip={tr("trackerReannounceForm.fields.initialWait.tooltip")}
            min={REANNOUNCE_CONSTRAINTS.MIN_INITIAL_WAIT}
            value={settings.initialWaitSeconds}
            onChange={(value) => setSettings((prev) => ({ ...prev, initialWaitSeconds: value }))}
          />
          <NumberField
            id="reannounce-interval"
            label={tr("trackerReannounceForm.fields.retryInterval.label")}
            description={tr("trackerReannounceForm.fields.retryInterval.description")}
            tooltip={tr("trackerReannounceForm.fields.retryInterval.tooltip")}
            min={REANNOUNCE_CONSTRAINTS.MIN_INTERVAL}
            value={settings.reannounceIntervalSeconds}
            onChange={(value) => setSettings((prev) => ({ ...prev, reannounceIntervalSeconds: value }))}
          />
          <NumberField
            id="max-age"
            label={tr("trackerReannounceForm.fields.maxTorrentAge.label")}
            description={tr("trackerReannounceForm.fields.maxTorrentAge.description")}
            tooltip={tr("trackerReannounceForm.fields.maxTorrentAge.tooltip")}
            min={REANNOUNCE_CONSTRAINTS.MIN_MAX_AGE}
            value={settings.maxAgeSeconds}
            onChange={(value) => setSettings((prev) => ({ ...prev, maxAgeSeconds: value }))}
          />
          <NumberField
            id="max-retries"
            label={tr("trackerReannounceForm.fields.maxRetries.label")}
            description={tr("trackerReannounceForm.fields.maxRetries.description")}
            tooltip={tr("trackerReannounceForm.fields.maxRetries.tooltip")}
            min={REANNOUNCE_CONSTRAINTS.MIN_MAX_RETRIES}
            max={REANNOUNCE_CONSTRAINTS.MAX_MAX_RETRIES}
            value={settings.maxRetries}
            onChange={(value) => setSettings((prev) => ({ ...prev, maxRetries: value }))}
          />
        </div>

        <div className="flex items-center justify-between rounded-lg border p-3 bg-muted/40">
          <div className="space-y-0.5">
            <div className="flex items-center gap-2">
              <Label htmlFor="quick-retry" className="text-base">{tr("trackerReannounceForm.fields.quickRetry.label")}</Label>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-4 w-4 text-muted-foreground cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-[300px]">
                  <p>{tr("trackerReannounceForm.fields.quickRetry.tooltip")}</p>
                </TooltipContent>
              </Tooltip>
            </div>
            <p className="text-sm text-muted-foreground">
              {tr("trackerReannounceForm.fields.quickRetry.description")}
            </p>
          </div>
          <Switch
            id="quick-retry"
            checked={settings.aggressive}
            onCheckedChange={(aggressive) => setSettings((prev) => ({ ...prev, aggressive }))}
          />
        </div>
      </div>

      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
            {tr("trackerReannounceForm.sections.scopeAndFiltering")}
          </h3>
          <Separator className="flex-1" />
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between rounded-lg border p-3 bg-muted/40">
            <div className="space-y-0.5">
              <Label htmlFor="monitor-all" className="text-base">{tr("trackerReannounceForm.fields.monitorAll.label")}</Label>
              <p className="text-sm text-muted-foreground">
                {tr("trackerReannounceForm.fields.monitorAll.descriptionLine1")}<br />
                {tr("trackerReannounceForm.fields.monitorAll.descriptionLine2")}
              </p>
            </div>
            <Switch
              id="monitor-all"
              checked={settings.monitorAll}
              onCheckedChange={(v) => {
                setSettings((prev) => {
                  const next = { ...prev, monitorAll: v }
                  // Automatically switch to exclude mode if monitoring all
                  if (v) {
                    next.excludeCategories = true
                    next.excludeTags = true
                    next.excludeTrackers = true
                  }
                  return next
                })
              }}
            />
          </div>


          <div className="grid gap-6 pt-2 animate-in fade-in slide-in-from-top-2 duration-200">
            {/* Categories */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label htmlFor="scope-categories">{tr("trackerReannounceForm.fields.categories.label")}</Label>
                <Tabs
                  value={settings.excludeCategories ? "exclude" : "include"}
                  onValueChange={(v) => setSettings((prev) => ({ ...prev, excludeCategories: v === "exclude" }))}
                  className="h-7"
                >
                  <TabsList className="h-7">
                    <TabsTrigger
                      value="include"
                      className="text-xs h-5 px-2"
                      disabled={settings.monitorAll}
                    >
                      {tr("trackerReannounceForm.values.include")}
                    </TabsTrigger>
                    <TabsTrigger value="exclude" className="text-xs h-5 px-2">
                      {tr("trackerReannounceForm.values.exclude")}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
              </div>
              <MultiSelect
                options={categoryOptions}
                selected={settings.categories}
                onChange={(values) => setSettings((prev) => ({ ...prev, categories: values }))}
                placeholder={tr("trackerReannounceForm.placeholders.selectCategories")}
                creatable
                onCreateOption={(value) => appendUniqueValue("categories", value)}
              />
            </div>

            {/* Tags */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label htmlFor="scope-tags">{tr("trackerReannounceForm.fields.tags.label")}</Label>
                <Tabs
                  value={settings.excludeTags ? "exclude" : "include"}
                  onValueChange={(v) => setSettings((prev) => ({ ...prev, excludeTags: v === "exclude" }))}
                  className="h-7"
                >
                  <TabsList className="h-7">
                    <TabsTrigger
                      value="include"
                      className="text-xs h-5 px-2"
                      disabled={settings.monitorAll}
                    >
                      {tr("trackerReannounceForm.values.include")}
                    </TabsTrigger>
                    <TabsTrigger value="exclude" className="text-xs h-5 px-2">
                      {tr("trackerReannounceForm.values.exclude")}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
              </div>
              <MultiSelect
                options={tagOptions}
                selected={settings.tags}
                onChange={(values) => setSettings((prev) => ({ ...prev, tags: values }))}
                placeholder={tr("trackerReannounceForm.placeholders.selectTags")}
                creatable
                onCreateOption={(value) => appendUniqueValue("tags", value)}
              />
            </div>

            {/* Trackers */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label htmlFor="scope-trackers">{tr("trackerReannounceForm.fields.trackerDomains.label")}</Label>
                <Tabs
                  value={settings.excludeTrackers ? "exclude" : "include"}
                  onValueChange={(v) => setSettings((prev) => ({ ...prev, excludeTrackers: v === "exclude" }))}
                  className="h-7"
                >
                  <TabsList className="h-7">
                    <TabsTrigger
                      value="include"
                      className="text-xs h-5 px-2"
                      disabled={settings.monitorAll}
                    >
                      {tr("trackerReannounceForm.values.include")}
                    </TabsTrigger>
                    <TabsTrigger value="exclude" className="text-xs h-5 px-2">
                      {tr("trackerReannounceForm.values.exclude")}
                    </TabsTrigger>
                  </TabsList>
                </Tabs>
              </div>
              <MultiSelect
                options={trackerOptions}
                selected={selectedTrackerValues}
                onChange={(values) => setSettings((prev) => ({ ...prev, trackers: normalizeTrackerDomains(values) }))}
                placeholder={tr("trackerReannounceForm.placeholders.selectTrackerDomains")}
                creatable
                onCreateOption={(value) => appendUniqueValue("trackers", value)}
                hideCheckIcon
              />
            </div>
          </div>
        </div>
      </div>

      {!formId && (
        <div className="flex justify-end pt-4">
          <Button type="submit" disabled={isUpdating}>
            {isUpdating ? tr("trackerReannounceForm.actions.saving") : tr("trackerReannounceForm.actions.saveChanges")}
          </Button>
        </div>
      )}
    </div>
  )

  const activityContent = (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <h3 className="text-sm font-medium leading-none">{tr("trackerReannounceForm.activity.title")}</h3>
          <p className="text-sm text-muted-foreground">
            {activityEnabled
              ? tr("trackerReannounceForm.activity.enabledDescription")
              : tr("trackerReannounceForm.activity.disabledDescription")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2 mr-2">
            <Switch
              id="hide-skipped"
              checked={hideSkipped}
              onCheckedChange={setHideSkipped}
              className="scale-75"
            />
            <Label htmlFor="hide-skipped" className="text-sm font-normal cursor-pointer">
              {tr("trackerReannounceForm.activity.hideSkipped")}
            </Label>
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={activityQuery.isFetching}
            onClick={() => activityQuery.refetch()}
            className="h-8 px-2 lg:px-3"
          >
            <RefreshCcw className={cn("h-3.5 w-3.5 mr-2", activityQuery.isFetching && "animate-spin")} />
            {tr("trackerReannounceForm.actions.refresh")}
          </Button>
        </div>
      </div>

      {activityQuery.isError ? (
        <div className="h-[150px] flex flex-col items-center justify-center border border-destructive/30 rounded-lg bg-destructive/10 text-center p-4">
          <p className="text-sm text-destructive">{tr("trackerReannounceForm.states.failedLoadActivity")}</p>
          <p className="text-xs text-destructive/70 mt-1">
            {tr("trackerReannounceForm.states.checkConnection")}
          </p>
        </div>
      ) : activityQuery.isLoading ? (
        <div className="h-[300px] flex items-center justify-center border rounded-lg bg-muted/40">
          <p className="text-sm text-muted-foreground">{tr("trackerReannounceForm.states.loadingActivity")}</p>
        </div>
      ) : activityEvents.length === 0 ? (
        <div className="h-[300px] flex flex-col items-center justify-center border border-dashed rounded-lg bg-muted/40 text-center p-6">
          <p className="text-sm text-muted-foreground">{tr("trackerReannounceForm.states.noActivityYet")}</p>
          {activityEnabled && (
            <p className="text-xs text-muted-foreground/60 mt-1">
              {tr("trackerReannounceForm.states.activityWillAppear")}
            </p>
          )}
        </div>
      ) : (
        <ScrollArea className="h-[400px] rounded-md border bg-muted/20">
          <div className="divide-y divide-border">
            {activityEvents.map((event, index) => (
              <div key={`${event.hash}-${index}-${event.timestamp}`} className="p-4 hover:bg-muted/30 transition-colors">
                <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
                  <div className="space-y-1.5 flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="font-medium text-sm truncate max-w-[300px] sm:max-w-[400px] cursor-help">
                            {event.torrentName || event.hash}
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p className="font-semibold">{event.torrentName || tr("trackerReannounceForm.values.notAvailable")}</p>
                        </TooltipContent>
                      </Tooltip>
                      <Badge variant="outline" className={cn("capitalize text-[10px] px-1.5 py-0 h-5", outcomeClasses[event.outcome])}>
                        {outcomeLabels[event.outcome]}
                      </Badge>
                    </div>

                    <div className="flex items-center gap-3 text-xs text-muted-foreground">
                      <div className="flex items-center gap-1 bg-muted/60 px-1.5 py-0.5 rounded">
                        <span className="font-mono">{event.hash.substring(0, 7)}</span>
                        <button
                          type="button"
                          className="hover:text-foreground transition-colors"
                          onClick={() => {
                            copyTextToClipboard(event.hash)
                            toast.success(tr("trackerReannounceForm.toasts.hashCopied"))
                          }}
                          title={tr("trackerReannounceForm.activity.copyHash")}
                        >
                          <Copy className="h-3 w-3" />
                        </button>
                      </div>
                      <span className="text-muted-foreground/40">•</span>
                      <span>{formatISOTimestamp(event.timestamp)}</span>
                    </div>

                    {(event.trackers || event.reason) && (
                      <div className="mt-2 space-y-1 bg-muted/40 p-2 rounded text-xs">
                        {event.trackers && (
                          <div className="flex items-start gap-2">
                            <span className="font-medium text-muted-foreground shrink-0">{tr("trackerReannounceForm.activity.labels.trackers")}:</span>
                            <span className="text-foreground break-all">{event.trackers}</span>
                          </div>
                        )}
                        {event.reason && (
                          <div className="flex items-start gap-2">
                            <span className="font-medium text-muted-foreground shrink-0">{tr("trackerReannounceForm.activity.labels.reason")}:</span>
                            {formatErrorReason(event.reason) !== event.reason ? (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="text-foreground break-all cursor-help">{formatErrorReason(event.reason)}</span>
                                </TooltipTrigger>
                                <TooltipContent className="max-w-md">
                                  <p className="break-all">{event.reason}</p>
                                </TooltipContent>
                              </Tooltip>
                            ) : (
                              <span className="text-foreground break-all">{event.reason}</span>
                            )}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </ScrollArea>
      )}
    </div>
  )

  if (variant === "embedded") {
    // Embedded mode: only show settings, no tabs (activity is shown in overview)
    return (
      <form id={formId} onSubmit={handleSubmit} className="space-y-6">
        {headerContent}
        {settingsContent}
      </form>
    )
  }

  // Card mode: show tabs with settings and activity
  return (
    <form onSubmit={handleSubmit}>
      <Card className="w-full">
        <CardHeader className="space-y-4">
          {headerContent}
        </CardHeader>
        <CardContent>
          <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
            <div className="flex items-center justify-between mb-4">
              <TabsList className="grid w-full grid-cols-2 lg:w-[400px]">
                <TabsTrigger value="settings">{tr("trackerReannounceForm.tabs.settings")}</TabsTrigger>
                <TabsTrigger value="activity">{tr("trackerReannounceForm.tabs.activityLog")}</TabsTrigger>
              </TabsList>
            </div>
            <TabsContent value="settings" className="mt-0">
              {settingsContent}
            </TabsContent>
            <TabsContent value="activity" className="mt-0">
              {activityContent}
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </form>
  )
}

interface NumberFieldProps {
  id: string
  label: string
  description?: string
  tooltip?: string
  value: number
  min: number
  max?: number
  onChange: (value: number) => void
}

function NumberField({ id, label, description, tooltip, value, min, max, onChange }: NumberFieldProps) {
  const [inputValue, setInputValue] = useState<string>(() => String(value))

  useEffect(() => {
    setInputValue(String(value))
  }, [value])

  const sanitizeAndCommit = (rawValue: string) => {
    const parsed = Math.floor(Number(rawValue))
    let sanitized = !rawValue.trim() || !Number.isFinite(parsed) ? Math.max(min, value) : Math.max(min, parsed)
    if (max !== undefined) {
      sanitized = Math.min(max, sanitized)
    }
    if (sanitized !== value) {
      onChange(sanitized)
    }
    setInputValue(String(sanitized))
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Label htmlFor={id} className="text-sm font-medium">{label}</Label>
        {tooltip && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-[250px]">
              <p>{tooltip}</p>
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        value={inputValue}
        onChange={(event) => {
          const nextValue = event.target.value
          setInputValue(nextValue)

          const parsed = Math.floor(Number(nextValue))
          if (!nextValue.trim() || !Number.isFinite(parsed)) {
            return
          }
          onChange(parsed)
        }}
        onBlur={(event) => sanitizeAndCommit(event.target.value)}
        className="h-9"
      />
      {description && <p className="text-[11px] text-muted-foreground">{description}</p>}
    </div>
  )
}

function cloneSettings(settings?: InstanceReannounceSettings): InstanceReannounceSettings {
  if (!settings) {
    return { ...DEFAULT_SETTINGS }
  }
  return {
    enabled: settings.enabled,
    initialWaitSeconds: settings.initialWaitSeconds,
    reannounceIntervalSeconds: settings.reannounceIntervalSeconds,
    maxAgeSeconds: settings.maxAgeSeconds,
    maxRetries: settings.maxRetries,
    monitorAll: settings.monitorAll,
    excludeCategories: settings.excludeCategories,
    categories: [...settings.categories],
    excludeTags: settings.excludeTags,
    tags: [...settings.tags],
    excludeTrackers: settings.excludeTrackers,
    trackers: normalizeTrackerDomains(settings.trackers),
    aggressive: settings.aggressive,
  }
}

function sanitizeSettings(settings: InstanceReannounceSettings): InstanceReannounceSettings {
  const clamp = (value: number, fallback: number, min: number, max?: number) => {
    const parsed = Number.isFinite(value) ? Math.floor(value) : fallback
    const clamped = Math.max(min, parsed)
    return max !== undefined ? Math.min(max, clamped) : clamped
  }
  const normalizeList = (values: string[]) => values.map((value) => value.trim()).filter(Boolean)

  return {
    enabled: settings.enabled,
    initialWaitSeconds: clamp(settings.initialWaitSeconds, DEFAULT_SETTINGS.initialWaitSeconds, REANNOUNCE_CONSTRAINTS.MIN_INITIAL_WAIT),
    reannounceIntervalSeconds: clamp(settings.reannounceIntervalSeconds, DEFAULT_SETTINGS.reannounceIntervalSeconds, REANNOUNCE_CONSTRAINTS.MIN_INTERVAL),
    maxAgeSeconds: clamp(settings.maxAgeSeconds, DEFAULT_SETTINGS.maxAgeSeconds, REANNOUNCE_CONSTRAINTS.MIN_MAX_AGE),
    maxRetries: clamp(settings.maxRetries, DEFAULT_SETTINGS.maxRetries, REANNOUNCE_CONSTRAINTS.MIN_MAX_RETRIES, REANNOUNCE_CONSTRAINTS.MAX_MAX_RETRIES),
    monitorAll: settings.monitorAll,
    excludeCategories: settings.excludeCategories,
    categories: normalizeList(settings.categories),
    excludeTags: settings.excludeTags,
    tags: normalizeList(settings.tags),
    excludeTrackers: settings.excludeTrackers,
    trackers: normalizeTrackerDomains(settings.trackers),
    aggressive: settings.aggressive,
  }
}
