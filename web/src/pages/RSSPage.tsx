/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertCircle,
  Check,
  ChevronRight,
  Download,
  ExternalLink,
  FileText,
  Folder,
  HardDrive,
  Link,
  Loader2,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  Regex,
  Rss,
  Search,
  Settings,
  Tag,
  Trash2
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

import { MultiSelect } from "@/components/ui/multi-select"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedInstanceSelection } from "@/hooks/usePersistedInstanceSelection"
import {
  rssKeys,
  useAddRSSFeed,
  useAddRSSFolder,
  useMarkRSSAsRead,
  useMoveRSSItem,
  useRefreshRSSFeed,
  useRemoveRSSItem,
  useRemoveRSSRule,
  useReprocessRSSRules,
  useRSSFeeds,
  useRSSMatchingArticles,
  useRSSRules,
  useSetRSSFeedURL,
  useSetRSSRule
} from "@/hooks/useRSS"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { renderTextWithLinks } from "@/lib/linkUtils"
import type {
  AppPreferences,
  Category,
  RSSArticle,
  RSSAutoDownloadRule,
  RSSFeed,
  RSSItems
} from "@/types"
import { isRSSFeed } from "@/types"
import { useQueryClient } from "@tanstack/react-query"
import { useTranslation } from "react-i18next"

import { AddTorrentDialog, type AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"

interface RSSPageProps {
  activeTab: "feeds" | "rules"
  selectedFeedPath?: string
  selectedRuleName?: string
  onTabChange: (tab: "feeds" | "rules") => void
  onFeedSelect: (feedPath: string | undefined) => void
  onRuleSelect: (ruleName: string | undefined) => void
}

function useCommonTr() {
  const { t } = useTranslation("common")
  return (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
}

export function RSSPage({
  activeTab,
  selectedFeedPath,
  selectedRuleName,
  onTabChange,
  onFeedSelect,
  onRuleSelect,
}: RSSPageProps) {
  const tr = useCommonTr()
  const { instances } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = usePersistedInstanceSelection("rss")

  // Auto-select/validate instance selection
  useEffect(() => {
    if (!instances || instances.length === 0) {
      if (selectedInstanceId !== undefined) {
        setSelectedInstanceId(undefined)
      }
      return
    }

    if (selectedInstanceId !== undefined) {
      const exists = instances.some((i) => i.id === selectedInstanceId)
      if (exists) {
        return
      }
      const fallbackInstance = instances.find((i) => i.connected) ?? instances[0]
      setSelectedInstanceId(fallbackInstance?.id)
      return
    }

    const firstConnected = instances.find((i) => i.connected)
    if (firstConnected) {
      setSelectedInstanceId(firstConnected.id)
    } else if (instances[0]) {
      setSelectedInstanceId(instances[0].id)
    }
  }, [selectedInstanceId, setSelectedInstanceId, instances])

  const instanceId = selectedInstanceId ?? 0

  const handleInstanceSelection = (value: string) => {
    if (value === "") {
      setSelectedInstanceId(undefined)
      return
    }
    const parsed = parseInt(value, 10)
    if (Number.isNaN(parsed)) {
      setSelectedInstanceId(undefined)
      return
    }
    setSelectedInstanceId(parsed)
  }

  const hasInstances = (instances?.length ?? 0) > 0

  // Queries
  const {
    data: feedsData,
    isLoading: feedsLoading,
    isError: feedsIsError,
    error: feedsError,
    refetch: refetchFeeds,
    sseStatus,
    sseReconnectAttempt,
  } = useRSSFeeds(instanceId, {
    enabled: instanceId > 0,
  })
  const {
    data: rulesData,
    isLoading: rulesLoading,
    isError: rulesIsError,
    error: rulesError,
    refetch: refetchRules,
  } = useRSSRules(instanceId, {
    enabled: instanceId > 0,
  })
  const { preferences, updatePreferences, isUpdating: isUpdatingPreferences } = useInstancePreferences(instanceId, {
    enabled: instanceId > 0,
  })
  const { data: metadata } = useInstanceMetadata(instanceId)

  // Derived state
  const isRSSProcessingEnabled = preferences?.rss_processing_enabled ?? true
  const isRSSAutoDownloadingEnabled = preferences?.rss_auto_downloading_enabled ?? true

  // SSE status notifications (only on permanent disconnect)
  const sseDisconnectToastShownRef = useRef(false)
  useEffect(() => {
    if (sseStatus === "live") {
      sseDisconnectToastShownRef.current = false
      return
    }
    if (sseStatus === "disconnected" && sseReconnectAttempt > 0 && !sseDisconnectToastShownRef.current) {
      toast.error(tr("rssPage.toasts.liveUpdatesDisconnected"))
      sseDisconnectToastShownRef.current = true
    }
  }, [sseStatus, sseReconnectAttempt, tr])

  // Mutations
  const reprocessRules = useReprocessRSSRules(instanceId)
  const refreshAllFeeds = useRefreshRSSFeed(instanceId)

  // Dialog states
  const [addFeedOpen, setAddFeedOpen] = useState(false)
  const [addFolderOpen, setAddFolderOpen] = useState(false)
  const [addRuleOpen, setAddRuleOpen] = useState(false)

  // Instance selector component (reused in different places)
  const renderInstanceSelector = () => (
    <Select value={instanceId > 0 ? instanceId.toString() : ""} onValueChange={handleInstanceSelection}>
      <SelectTrigger className="!w-[240px] !max-w-[240px]">
        <div className="flex items-center gap-2 min-w-0 overflow-hidden">
          <HardDrive className="h-4 w-4 flex-shrink-0" />
          <span className="truncate">
            <SelectValue placeholder={tr("rssPage.instanceSelector.placeholder")} />
          </span>
        </div>
      </SelectTrigger>
      <SelectContent>
        {instances?.map((inst) => (
          <SelectItem key={inst.id} value={inst.id.toString()}>
            <div className="flex items-center max-w-40 gap-2">
              <span className="truncate">{inst.name}</span>
              <span
                className={`ml-auto h-2 w-2 rounded-full flex-shrink-0 ${inst.connected? "bg-green-500": "bg-red-500"
                }`}
              />
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )

  // No instances view
  if (!hasInstances) {
    return (
      <div className="flex flex-1 items-center justify-center p-8">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center">
            <Rss className="mx-auto h-12 w-12 text-muted-foreground mb-2" />
            <CardTitle>{tr("rssPage.empty.noInstancesTitle")}</CardTitle>
            <CardDescription>
              {tr("rssPage.empty.noInstancesDescription")}
            </CardDescription>
          </CardHeader>
        </Card>
      </div>
    )
  }

  // No instance selected view
  if (!instanceId) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-center gap-3">
            <Rss className="h-6 w-6" />
            <h1 className="text-2xl font-semibold">{tr("rssPage.header.title")}</h1>
          </div>
          {renderInstanceSelector()}
        </div>
        <div className="flex flex-1 items-center justify-center">
          <Card className="w-full max-w-md">
            <CardHeader className="text-center">
              <HardDrive className="mx-auto h-12 w-12 text-muted-foreground mb-2" />
              <CardTitle>{tr("rssPage.empty.selectInstanceTitle")}</CardTitle>
              <CardDescription>
                {tr("rssPage.empty.selectInstanceDescription")}
              </CardDescription>
            </CardHeader>
          </Card>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)] p-6 overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 shrink-0">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold">{tr("rssPage.header.title")}</h1>
          <p className="text-sm text-muted-foreground">
            {tr("rssPage.header.description")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {renderInstanceSelector()}
          <RssSettingsPopover
            preferences={preferences}
            updatePreferences={updatePreferences}
            isUpdating={isUpdatingPreferences}
          />
        </div>
      </div>

      {/* Warning Banners */}
      {!isRSSProcessingEnabled && (
        <Alert variant="destructive" className="mb-4 flex items-center gap-3 [&>svg]:static [&>svg]:shrink-0 [&>svg~*]:pl-0">
          <AlertCircle className="h-4 w-4" />
          <span className="flex-1 text-sm">
            {tr("rssPage.warnings.processingDisabled")}
          </span>
          <Button
            size="sm"
            variant="outline"
            className="shrink-0 !px-3"
            onClick={() => {
              updatePreferences(
                { rss_processing_enabled: true },
                {
                  onSuccess: () => toast.success(tr("rssPage.toasts.rssProcessingEnabled")),
                  onError: () => toast.error(tr("rssPage.toasts.enableRssProcessingFailed")),
                }
              )
            }}
            disabled={isUpdatingPreferences}
          >
            {isUpdatingPreferences ? <Loader2 className="h-4 w-4 animate-spin" /> : tr("rssPage.actions.enableRss")}
          </Button>
        </Alert>
      )}

      {isRSSProcessingEnabled && !isRSSAutoDownloadingEnabled && (
        <Alert variant="warning" className="mb-4 flex items-center gap-3 [&>svg]:static [&>svg]:shrink-0 [&>svg~*]:pl-0">
          <AlertCircle className="h-4 w-4" />
          <span className="flex-1 text-sm">
            {tr("rssPage.warnings.autoDownloadDisabled")}
          </span>
          <Button
            size="sm"
            variant="outline"
            className="shrink-0 !px-3"
            onClick={() => {
              updatePreferences(
                { rss_auto_downloading_enabled: true },
                {
                  onSuccess: () => toast.success(tr("rssPage.toasts.autoDownloadingEnabled")),
                  onError: () => toast.error(tr("rssPage.toasts.enableAutoDownloadingFailed")),
                }
              )
            }}
            disabled={isUpdatingPreferences}
          >
            {isUpdatingPreferences ? <Loader2 className="h-4 w-4 animate-spin" /> : tr("rssPage.actions.enableAutoDownload")}
          </Button>
        </Alert>
      )}

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={(v) => onTabChange(v as "feeds" | "rules")} className="flex-1 min-h-0 flex flex-col">
        <div className="flex items-center justify-between mb-4 shrink-0">
          <TabsList>
            <TabsTrigger value="feeds" className="gap-2">
              <Rss className="h-4 w-4" />
              {tr("rssPage.tabs.feeds")}
            </TabsTrigger>
            <TabsTrigger value="rules" className="gap-2">
              <FileText className="h-4 w-4" />
              {tr("rssPage.tabs.rules")}
            </TabsTrigger>
          </TabsList>

          {activeTab === "feeds" && (
            <div className="flex items-center gap-2">
              {sseStatus !== "disabled" && isRSSProcessingEnabled && feedsData && Object.keys(feedsData).length > 0 && (
                <Badge
                  variant="outline"
                  className={`gap-2 ${sseStatus === "live"? "border-green-500/50 bg-green-500/10 text-green-600 dark:text-green-400": sseStatus === "reconnecting" || sseStatus === "connecting"? "border-yellow-500/50 bg-yellow-500/10 text-yellow-700 dark:text-yellow-400": "border-red-500/50 bg-red-500/10 text-red-600 dark:text-red-400"
                  }`}
                >
                  {sseStatus === "connecting" && <Loader2 className="h-3 w-3 animate-spin" />}
                  {sseStatus === "reconnecting" && <Loader2 className="h-3 w-3 animate-spin" />}
                  {sseStatus === "live" && <span className="h-2 w-2 rounded-full bg-green-500" />}
                  {sseStatus === "disconnected" && <span className="h-2 w-2 rounded-full bg-red-500" />}
                  <span className="text-xs">
                    {sseStatus === "live"
                      ? tr("rssPage.sse.live")
                      : sseStatus === "connecting"
                        ? tr("rssPage.sse.connecting")
                        : sseStatus === "reconnecting"
                          ? sseReconnectAttempt > 0
                            ? tr("rssPage.sse.reconnectingWithAttempt", { attempt: sseReconnectAttempt, max: 5 })
                            : tr("rssPage.sse.reconnecting")
                          : tr("rssPage.sse.disconnected")}
                  </span>
                </Badge>
              )}
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  refreshAllFeeds.mutate(
                    { itemPath: "" },
                    {
                      onSuccess: () => toast.success(tr("rssPage.toasts.refreshingAllFeeds")),
                      onError: () => toast.error(tr("rssPage.toasts.refreshFeedsFailed")),
                    }
                  )
                }}
                disabled={refreshAllFeeds.isPending}
              >
                {refreshAllFeeds.isPending ? (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                ) : (
                  <RefreshCw className="h-4 w-4 mr-2" />
                )}
                {tr("rssPage.actions.refreshAll")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setAddFolderOpen(true)}>
                <Folder className="h-4 w-4 mr-2" />
                {tr("rssPage.actions.addFolder")}
              </Button>
              <Button size="sm" onClick={() => setAddFeedOpen(true)}>
                <Plus className="h-4 w-4 mr-2" />
                {tr("rssPage.actions.addFeed")}
              </Button>
            </div>
          )}

          {activeTab === "rules" && (
            <div className="flex items-center gap-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      reprocessRules.mutate(undefined, {
                        onSuccess: () => {
                          toast.success(tr("rssPage.toasts.rulesReprocessed"))
                        },
                        onError: () => {
                          toast.error(tr("rssPage.toasts.reprocessRulesFailed"))
                        },
                      })
                    }}
                    disabled={reprocessRules.isPending}
                  >
                    {reprocessRules.isPending ? (
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <RefreshCw className="h-4 w-4 mr-2" />
                    )}
                    {tr("rssPage.actions.reprocess")}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{tr("rssPage.tooltips.reprocess")}</p>
                </TooltipContent>
              </Tooltip>
              <Button size="sm" onClick={() => setAddRuleOpen(true)}>
                <Plus className="h-4 w-4 mr-2" />
                {tr("rssPage.actions.addRule")}
              </Button>
            </div>
          )}
        </div>

        <TabsContent value="feeds" className="mt-0 flex-1 min-h-0">
          <FeedsTab
            instanceId={instanceId}
            feedsData={feedsData}
            feedsLoading={feedsLoading}
            feedsIsError={feedsIsError}
            feedsError={feedsError}
            onRetry={() => {
              refetchFeeds()
            }}
            selectedFeedPath={selectedFeedPath}
            onFeedSelect={onFeedSelect}
          />
        </TabsContent>

        <TabsContent value="rules" className="mt-0 flex-1 min-h-0">
          <RulesTab
            instanceId={instanceId}
            rulesData={rulesData}
            rulesLoading={rulesLoading}
            rulesIsError={rulesIsError}
            rulesError={rulesError}
            onRetry={() => {
              refetchRules()
            }}
            selectedRuleName={selectedRuleName}
            onRuleSelect={onRuleSelect}
            feedsData={feedsData}
            categories={metadata?.categories ?? {}}
            tags={metadata?.tags ?? []}
          />
        </TabsContent>
      </Tabs>

      {/* Dialogs */}
      <AddFeedDialog
        instanceId={instanceId}
        open={addFeedOpen}
        onOpenChange={setAddFeedOpen}
        feedsData={feedsData}
      />
      <AddFolderDialog instanceId={instanceId} open={addFolderOpen} onOpenChange={setAddFolderOpen} />
      <AddRuleDialog
        instanceId={instanceId}
        open={addRuleOpen}
        onOpenChange={setAddRuleOpen}
        feedsData={feedsData}
        categories={metadata?.categories ?? {}}
        tags={metadata?.tags ?? []}
      />
    </div>
  )
}

// ============================================================================
// Feeds Tab
// ============================================================================

interface FeedsTabProps {
  instanceId: number
  feedsData: RSSItems | undefined
  feedsLoading: boolean
  feedsIsError: boolean
  feedsError: unknown
  onRetry: () => void
  selectedFeedPath?: string
  onFeedSelect: (feedPath: string | undefined) => void
}

function FeedsTab({
  instanceId,
  feedsData,
  feedsLoading,
  feedsIsError,
  feedsError,
  onRetry,
  selectedFeedPath,
  onFeedSelect,
}: FeedsTabProps) {
  const tr = useCommonTr()
  const queryClient = useQueryClient()
  const removeFeed = useRemoveRSSItem(instanceId)
  const refreshFeed = useRefreshRSSFeed(instanceId)
  const markAsRead = useMarkRSSAsRead(instanceId)
  const moveFeed = useMoveRSSItem(instanceId)
  const setFeedURL = useSetRSSFeedURL(instanceId)

  // Capabilities for version-gated features
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsSetRSSFeedURL = capabilities?.supportsSetRSSFeedURL ?? false

  // Rename dialog state
  const [renameDialog, setRenameDialog] = useState<{ open: boolean; path: string; currentName: string }>({
    open: false,
    path: "",
    currentName: "",
  })
  const [newName, setNewName] = useState("")

  // Edit URL dialog state
  const [editURLDialog, setEditURLDialog] = useState<{ open: boolean; path: string; currentURL: string }>({
    open: false,
    path: "",
    currentURL: "",
  })
  const [newURL, setNewURL] = useState("")

  // AddTorrentDialog state
  const [addTorrentOpen, setAddTorrentOpen] = useState(false)
  const [addTorrentPayload, setAddTorrentPayload] = useState<AddTorrentDropPayload | null>(null)

  // Find selected feed
  const selectedFeed = useMemo(() => {
    if (!selectedFeedPath || !feedsData) return null
    return findFeedByPath(feedsData, selectedFeedPath)
  }, [selectedFeedPath, feedsData])

  if (feedsLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (feedsIsError) {
    const message = feedsError instanceof Error ? feedsError.message : tr("rssPage.errors.failedLoadFeeds")
    return (
      <Card>
        <CardContent className="py-6">
          <Alert
            variant="destructive"
            className="flex items-center gap-3 [&>svg]:static [&>svg]:shrink-0 [&>svg~*]:pl-0"
          >
            <AlertCircle className="h-4 w-4" />
            <div className="flex-1 min-w-0">
              <AlertTitle>{tr("rssPage.errors.failedLoadFeeds")}</AlertTitle>
              <AlertDescription className="break-words">{message}</AlertDescription>
            </div>
            <Button
              size="sm"
              variant="outline"
              className="shrink-0 !px-3"
              onClick={() => {
                onRetry()
                queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
              }}
            >
              {tr("rssPage.actions.retry")}
            </Button>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  if (!feedsData || Object.keys(feedsData).length === 0) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12">
          <Rss className="h-12 w-12 text-muted-foreground mb-4" />
          <p className="text-muted-foreground text-center">
            {tr("rssPage.empty.noFeedsConfigured")}
          </p>
        </CardContent>
      </Card>
    )
  }

  const handleRemoveFeed = async (path: string) => {
    try {
      await removeFeed.mutateAsync({ path })
      toast.success(tr("rssPage.toasts.feedRemoved"))
      if (selectedFeedPath === path) {
        onFeedSelect(undefined)
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.removeFeedFailed")
      toast.error(message)
    }
  }

  const handleRefreshFeed = async (path: string) => {
    try {
      await refreshFeed.mutateAsync({ itemPath: path })
      toast.success(tr("rssPage.toasts.feedRefreshTriggered"))
      // Invalidate to pick up changes
      setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: rssKeys.feeds(instanceId) })
      }, 2000)
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.refreshFeedFailed")
      toast.error(message)
    }
  }

  const handleMarkAllAsRead = async (feedPath: string) => {
    try {
      await markAsRead.mutateAsync({ itemPath: feedPath })
      toast.success(tr("rssPage.toasts.markedAllRead"))
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.markAllReadFailed")
      toast.error(message)
    }
  }

  const openRenameDialog = (path: string) => {
    // Extract the current name from the path (last segment)
    const segments = path.split("\\")
    const currentName = segments[segments.length - 1]
    setRenameDialog({ open: true, path, currentName })
    setNewName(currentName)
  }

  const handleRenameFeed = async () => {
    if (!newName.trim() || newName === renameDialog.currentName) {
      setRenameDialog({ open: false, path: "", currentName: "" })
      return
    }

    try {
      // Build the new path by replacing the last segment
      const segments = renameDialog.path.split("\\")
      segments[segments.length - 1] = newName.trim()
      const destPath = segments.join("\\")

      await moveFeed.mutateAsync({ itemPath: renameDialog.path, destPath })
      toast.success(tr("rssPage.toasts.feedRenamed"))

      // Update selection if the renamed feed was selected
      if (selectedFeedPath === renameDialog.path) {
        onFeedSelect(destPath)
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.renameFeedFailed")
      toast.error(message)
    } finally {
      setRenameDialog({ open: false, path: "", currentName: "" })
    }
  }

  const openEditURLDialog = (path: string, currentURL: string) => {
    setEditURLDialog({ open: true, path, currentURL })
    setNewURL(currentURL)
  }

  const handleEditURL = async () => {
    if (!newURL.trim() || newURL === editURLDialog.currentURL) {
      setEditURLDialog({ open: false, path: "", currentURL: "" })
      return
    }

    try {
      await setFeedURL.mutateAsync({ path: editURLDialog.path, url: newURL.trim() })
      toast.success(tr("rssPage.toasts.feedUrlUpdated"))
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.updateFeedUrlFailed")
      toast.error(message)
    } finally {
      setEditURLDialog({ open: false, path: "", currentURL: "" })
    }
  }

  const handleDownloadArticle = (torrentURL: string) => {
    setAddTorrentPayload({ type: "url", urls: [torrentURL] })
    setAddTorrentOpen(true)
  }

  const unreadCount = countUnreadArticles(feedsData)
  const selectedFeedArticles = selectedFeed?.articles ?? []
  const selectedFeedUnread = selectedFeedArticles.filter(a => !a.isRead).length

  return (
    <div className="grid grid-cols-1 lg:grid-cols-[280px_1fr] gap-4 h-full">
      {/* Feed Tree - Narrow sidebar */}
      <Card className="flex flex-col min-h-0">
        <CardHeader className="shrink-0 flex flex-row items-center justify-between space-y-0 py-0">
          <CardTitle className="text-sm font-medium">{tr("rssPage.tabs.feeds")}</CardTitle>
          {unreadCount > 0 && (
            <span className="text-xs text-muted-foreground">{unreadCount}</span>
          )}
        </CardHeader>
        <CardContent className="pt-0 px-3 flex-1 min-h-0 overflow-y-auto">
          <FeedTree
            items={feedsData}
            path=""
            selectedPath={selectedFeedPath}
            onSelect={onFeedSelect}
            onRemove={handleRemoveFeed}
            onRefresh={handleRefreshFeed}
            onRename={openRenameDialog}
            onEditURL={openEditURLDialog}
            supportsEditURL={supportsSetRSSFeedURL}
          />
        </CardContent>
      </Card>

      {/* Rename Dialog */}
      <Dialog open={renameDialog.open} onOpenChange={(open) => !open && setRenameDialog({ open: false, path: "", currentName: "" })}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{tr("rssPage.dialogs.renameFeed.title")}</DialogTitle>
            <DialogDescription>{tr("rssPage.dialogs.renameFeed.description")}</DialogDescription>
          </DialogHeader>
          <div className="py-4">
            <Input
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={tr("rssPage.dialogs.renameFeed.placeholder")}
              onKeyDown={(e) => e.key === "Enter" && handleRenameFeed()}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRenameDialog({ open: false, path: "", currentName: "" })}>
              {tr("rssPage.actions.cancel")}
            </Button>
            <Button onClick={handleRenameFeed} disabled={!newName.trim() || newName === renameDialog.currentName}>
              {tr("rssPage.actions.rename")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit URL Dialog */}
      <Dialog open={editURLDialog.open} onOpenChange={(open) => !open && setEditURLDialog({ open: false, path: "", currentURL: "" })}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{tr("rssPage.dialogs.editFeedUrl.title")}</DialogTitle>
            <DialogDescription>{tr("rssPage.dialogs.editFeedUrl.description")}</DialogDescription>
          </DialogHeader>
          <div className="py-4">
            <Input
              value={newURL}
              onChange={(e) => setNewURL(e.target.value)}
              placeholder={tr("rssPage.dialogs.editFeedUrl.placeholder")}
              onKeyDown={(e) => e.key === "Enter" && handleEditURL()}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditURLDialog({ open: false, path: "", currentURL: "" })}>
              {tr("rssPage.actions.cancel")}
            </Button>
            <Button onClick={handleEditURL} disabled={!newURL.trim() || newURL === editURLDialog.currentURL}>
              {tr("rssPage.actions.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add Torrent Dialog */}
      <AddTorrentDialog
        instanceId={instanceId}
        open={addTorrentOpen}
        onOpenChange={setAddTorrentOpen}
        dropPayload={addTorrentPayload}
        onDropPayloadConsumed={() => setAddTorrentPayload(null)}
      />

      {/* Articles Panel - Main content */}
      <Card className="flex flex-col min-h-0">
        <CardHeader className="shrink-0 flex flex-row items-center justify-between space-y-0 py-0">
          <CardTitle className="text-sm font-medium truncate min-w-0" title={selectedFeed?.url}>
            {selectedFeed ? (selectedFeed.title || tr("rssPage.articles.title")) : tr("rssPage.articles.title")}
          </CardTitle>
          {selectedFeed && (
            <div className="flex items-center gap-3 shrink-0">
              {selectedFeedUnread > 0 && (
                <span className="text-xs text-muted-foreground">{tr("rssPage.articles.unreadCount", { count: selectedFeedUnread })}</span>
              )}
              <Button variant="outline" size="sm" onClick={() => handleMarkAllAsRead(selectedFeedPath!)}>
                {tr("rssPage.actions.markAllRead")}
              </Button>
            </div>
          )}
        </CardHeader>
        <CardContent className="pt-0 flex-1 min-h-0 flex flex-col">
          {selectedFeed ? (
            <ArticlesPanel instanceId={instanceId} feed={selectedFeed} feedPath={selectedFeedPath!} onDownload={handleDownloadArticle} />
          ) : (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
              <Rss className="h-8 w-8 mb-3 opacity-50" />
              <p className="text-sm">{tr("rssPage.empty.selectFeedToView")}</p>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// ============================================================================
// Feed Tree Component
// ============================================================================

interface FeedTreeProps {
  items: RSSItems
  path: string
  selectedPath?: string
  onSelect: (path: string | undefined) => void
  onRemove: (path: string) => void
  onRefresh: (path: string) => void
  onRename: (path: string) => void
  onEditURL: (path: string, currentURL: string) => void
  supportsEditURL: boolean
}

function FeedTree({ items, path, selectedPath, onSelect, onRemove, onRefresh, onRename, onEditURL, supportsEditURL }: FeedTreeProps) {
  const tr = useCommonTr()
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set())

  // Auto-expand parent folders when selectedPath changes (e.g., on page load)
  useEffect(() => {
    if (!selectedPath) return
    const parts = selectedPath.split("\\")
    if (parts.length <= 1) return // No parent folders

    const parentPaths: string[] = []
    for (let i = 1; i < parts.length; i++) {
      parentPaths.push(parts.slice(0, i).join("\\"))
    }

    setExpandedFolders((prev) => {
      const next = new Set(prev)
      parentPaths.forEach((p) => next.add(p))
      return next
    })
  }, [selectedPath])

  const toggleFolder = (folderPath: string) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev)
      if (next.has(folderPath)) {
        next.delete(folderPath)
      } else {
        next.add(folderPath)
      }
      return next
    })
  }

  const entries = Object.entries(items).sort(([a], [b]) => a.localeCompare(b))

  return (
    <div className="space-y-0.5">
      {entries.map(([name, item]) => {
        const itemPath = path ? `${path}\\${name}` : name

        if (isRSSFeed(item)) {
          const feed = item as RSSFeed
          const unreadCount = feed.articles?.filter((a) => !a.isRead).length ?? 0
          const isSelected = selectedPath === itemPath
          const hasUnread = unreadCount > 0

          return (
            <div
              key={itemPath}
              className={`group flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer transition-colors ${isSelected? "bg-primary/10 text-primary": "hover:bg-muted"
              }`}
              onClick={() => onSelect(itemPath)}
              role="button"
              tabIndex={0}
              aria-current={isSelected ? "page" : undefined}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault()
                  onSelect(itemPath)
                }
              }}
            >
              <Rss className={`h-3.5 w-3.5 flex-shrink-0 ${feed.hasError ? "text-destructive" : isSelected ? "text-primary" : "text-muted-foreground"
              }`} />
              <span className={`flex-1 truncate text-sm ${hasUnread ? "font-medium" : ""}`} title={feed.title && feed.title !== name ? feed.title : undefined}>
                {name}
              </span>
              {feed.isLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
              {feed.hasError && <AlertCircle className="h-3.5 w-3.5 text-destructive" />}
              {hasUnread && (
                <span className="text-xs text-muted-foreground tabular-nums">{unreadCount}</span>
              )}
              <DropdownMenu>
                <DropdownMenuTrigger asChild onClick={(e) => e.stopPropagation()}>
                  <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0 text-muted-foreground/60 hover:text-foreground hover:bg-muted">
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => onRefresh(itemPath)}>
                    <RefreshCw className="h-4 w-4 mr-2" />
                    {tr("rssPage.actions.refresh")}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => onRename(itemPath)}>
                    <Pencil className="h-4 w-4 mr-2" />
                    {tr("rssPage.actions.rename")}
                  </DropdownMenuItem>
                  {supportsEditURL && feed.url && (
                    <DropdownMenuItem onClick={() => onEditURL(itemPath, feed.url)}>
                      <Link className="h-4 w-4 mr-2" />
                      {tr("rssPage.actions.editUrl")}
                    </DropdownMenuItem>
                  )}
                  {feed.url && (
                    <DropdownMenuItem asChild>
                      <a href={feed.url} target="_blank" rel="noopener noreferrer">
                        <ExternalLink className="h-4 w-4 mr-2" />
                        {tr("rssPage.actions.openUrl")}
                      </a>
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem className="text-destructive" onClick={() => onRemove(itemPath)}>
                    <Trash2 className="h-4 w-4 mr-2" />
                    {tr("rssPage.actions.remove")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          )
        } else {
          // Folder
          const folder = item as RSSItems
          const isExpanded = expandedFolders.has(itemPath)
          const feedCount = countFeeds(folder)
          const folderUnread = countUnreadArticles(folder)

          return (
            <div key={itemPath}>
              <div
                className="group flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer transition-colors hover:bg-muted"
                onClick={() => toggleFolder(itemPath)}
                role="button"
                tabIndex={0}
                aria-expanded={isExpanded}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault()
                    toggleFolder(itemPath)
                  }
                }}
              >
                <ChevronRight className={`h-3.5 w-3.5 text-muted-foreground transition-transform duration-150 ${isExpanded ? "rotate-90" : ""}`} />
                <Folder className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="flex-1 truncate text-sm font-medium">{name}</span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {folderUnread > 0 ? folderUnread : feedCount}
                </span>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild onClick={(e) => e.stopPropagation()}>
                    <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0 text-muted-foreground/60 hover:text-foreground hover:bg-muted">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => onRename(itemPath)}>
                      <Pencil className="h-4 w-4 mr-2" />
                      {tr("rssPage.actions.rename")}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem className="text-destructive" onClick={() => onRemove(itemPath)}>
                      <Trash2 className="h-4 w-4 mr-2" />
                      {tr("rssPage.actions.removeFolder")}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
              {isExpanded && (
                <div className="ml-5">
                  <FeedTree
                    items={folder}
                    path={itemPath}
                    selectedPath={selectedPath}
                    onSelect={onSelect}
                    onRemove={onRemove}
                    onRefresh={onRefresh}
                    onRename={onRename}
                    onEditURL={onEditURL}
                    supportsEditURL={supportsEditURL}
                  />
                </div>
              )}
            </div>
          )
        }
      })}
    </div>
  )
}

// ============================================================================
// Articles Panel
// ============================================================================

interface ArticlesPanelProps {
  instanceId: number
  feed: RSSFeed
  feedPath: string
  onDownload: (torrentURL: string) => void
}

function ArticlesPanel({ instanceId, feed, feedPath, onDownload }: ArticlesPanelProps) {
  const tr = useCommonTr()
  const { formatDate } = useDateTimeFormatters()
  const markAsRead = useMarkRSSAsRead(instanceId)
  const [search, setSearch] = useState("")

  const articles = feed.articles ?? []

  const sortedArticles = useMemo(() => {
    if (articles.length <= 1) return articles

    return [...articles].sort((a, b) => {
      const aTime = Date.parse(a.date)
      const bTime = Date.parse(b.date)
      const aValid = Number.isFinite(aTime)
      const bValid = Number.isFinite(bTime)

      if (aValid && bValid) {
        const diff = bTime - aTime
        if (diff !== 0) return diff
      }

      // Put valid dates first; push invalid/missing dates to the bottom.
      if (aValid && !bValid) return -1
      if (!aValid && bValid) return 1

      // Ensure deterministic ordering when dates are equal/missing.
      return a.id.localeCompare(b.id)
    })
  }, [articles])

  const filteredArticles = useMemo(() => {
    if (!search.trim()) return sortedArticles
    const term = search.toLowerCase()
    return sortedArticles.filter((article) =>
      article.title?.toLowerCase().includes(term) ||
      article.description?.toLowerCase().includes(term)
    )
  }, [sortedArticles, search])

  const handleMarkAsRead = async (articleId: string) => {
    try {
      await markAsRead.mutateAsync({ itemPath: feedPath, articleId })
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.markAsReadFailed")
      toast.error(message)
    }
  }

  if (articles.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
        <Rss className="h-8 w-8 mb-3 opacity-50" />
        <p className="text-sm">{tr("rssPage.empty.noArticlesInFeed")}</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="relative mb-2">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder={tr("rssPage.articles.searchPlaceholder")}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-8 h-8"
        />
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto">
        {filteredArticles.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-32 text-muted-foreground">
            <p className="text-sm">{tr("rssPage.empty.noMatchingArticles")}</p>
          </div>
        ) : (
          <div className="space-y-1 pr-1">
            {filteredArticles.map((article) => (
              <ArticleRow
                key={article.id}
                article={article}
                formatDate={formatDate}
                onMarkAsRead={() => handleMarkAsRead(article.id)}
                onDownload={onDownload}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

interface ArticleRowProps {
  article: RSSArticle
  formatDate: (date: Date) => string
  onMarkAsRead: () => void
  onDownload: (torrentURL: string) => void
}

// Strip HTML tags and decode common HTML entities, preserving link URLs
function stripHtml(html: string): string {
  return html
    .replace(/<br\s*\/?>/gi, "\n")
    .replace(/<a\s+[^>]*href=["']([^"']+)["'][^>]*>([^<]*)<\/a>/gi, "$2 $1")
    .replace(/<[^>]*>/g, "")
    .replace(/&quot;/g, "\"")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&nbsp;/g, " ")
    .replace(/&apos;/g, "'")
    .replace(/&#x([0-9a-fA-F]+);/gi, (_, hex) => String.fromCharCode(parseInt(hex, 16)))
    .replace(/&#(\d+);/g, (_, num) => String.fromCharCode(parseInt(num, 10)))
}

function ArticleRow({ article, formatDate, onMarkAsRead, onDownload }: ArticleRowProps) {
  const tr = useCommonTr()
  const formattedDate = article.date ? formatDate(new Date(article.date)) : ""
  const hasDetails = article.description || article.author
  const rowClass = `group grid grid-cols-[1fr_auto] items-center gap-2 px-3 py-2 rounded-md border border-border transition-colors hover:bg-accent hover:text-accent-foreground ${
    article.isRead ? "text-muted-foreground border-transparent" : ""
  }`
  const titleClass = `text-sm leading-snug truncate ${article.isRead ? "" : "font-medium"}`

  const titleContent = (
    <>
      <p className={titleClass}>{article.title}</p>
      <span className="text-xs text-muted-foreground">{formattedDate}</span>
    </>
  )

  const actionButtons = (
    <>
      {article.torrentURL && (
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-muted-foreground hover:text-foreground"
          onClick={() => onDownload(article.torrentURL!)}
          title={tr("rssPage.articles.downloadTorrent")}
        >
          <Download className="h-4 w-4" />
        </Button>
      )}
      {article.link && article.link !== article.torrentURL && (
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-muted-foreground hover:text-foreground"
          asChild
          title={tr("rssPage.articles.openLink")}
        >
          <a href={article.link} target="_blank" rel="noopener noreferrer">
            <ExternalLink className="h-4 w-4" />
          </a>
        </Button>
      )}
      {!article.isRead && (
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-muted-foreground hover:text-foreground"
          onClick={onMarkAsRead}
          title={tr("rssPage.actions.markAsRead")}
        >
          <Check className="h-4 w-4" />
        </Button>
      )}
    </>
  )

  if (!hasDetails) {
    return (
      <div className={rowClass}>
        <div className="min-w-0 text-left">{titleContent}</div>
        <div className="flex items-center gap-0.5 shrink-0">{actionButtons}</div>
      </div>
    )
  }

  return (
    <Collapsible>
      <div className={rowClass}>
        <CollapsibleTrigger className="min-w-0 text-left">{titleContent}</CollapsibleTrigger>
        <div className="flex items-center gap-0.5 shrink-0">
          <CollapsibleTrigger
            className="h-7 w-7 inline-flex items-center justify-center text-muted-foreground hover:text-foreground"
            title={tr("rssPage.articles.toggleDetails")}
          >
            <ChevronRight className="h-4 w-4 transition-transform [data-state=open]:rotate-90" />
          </CollapsibleTrigger>
          {actionButtons}
        </div>
      </div>
      <CollapsibleContent>
        <div className="px-3 pb-3 pt-1 text-sm text-muted-foreground">
          {article.description && <p className="whitespace-pre-wrap">{renderTextWithLinks(stripHtml(article.description))}</p>}
          {article.author && <p className="text-xs mt-1">{tr("rssPage.articles.byAuthor", { author: article.author })}</p>}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

// ============================================================================
// Rules Tab
// ============================================================================

interface RulesTabProps {
  instanceId: number
  rulesData: Record<string, RSSAutoDownloadRule> | undefined
  rulesLoading: boolean
  rulesIsError: boolean
  rulesError: unknown
  onRetry: () => void
  selectedRuleName?: string
  onRuleSelect: (ruleName: string | undefined) => void
  feedsData: RSSItems | undefined
  categories: Record<string, Category>
  tags: string[]
}

function RulesTab({
  instanceId,
  rulesData,
  rulesLoading,
  rulesIsError,
  rulesError,
  onRetry,
  selectedRuleName,
  onRuleSelect,
  feedsData,
  categories,
  tags,
}: RulesTabProps) {
  const tr = useCommonTr()
  const setRule = useSetRSSRule(instanceId)
  const removeRule = useRemoveRSSRule(instanceId)
  const queryClient = useQueryClient()
  const [editRuleOpen, setEditRuleOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<{ name: string; rule: RSSAutoDownloadRule } | null>(null)

  if (rulesLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (rulesIsError) {
    const message = rulesError instanceof Error ? rulesError.message : tr("rssPage.errors.failedLoadRules")
    return (
      <Card>
        <CardContent className="py-6">
          <Alert
            variant="destructive"
            className="flex items-center gap-3 [&>svg]:static [&>svg]:shrink-0 [&>svg~*]:pl-0"
          >
            <AlertCircle className="h-4 w-4" />
            <div className="flex-1 min-w-0">
              <AlertTitle>{tr("rssPage.errors.failedLoadRules")}</AlertTitle>
              <AlertDescription className="break-words">{message}</AlertDescription>
            </div>
            <Button
              size="sm"
              variant="outline"
              className="shrink-0 !px-3"
              onClick={() => {
                onRetry()
                queryClient.invalidateQueries({ queryKey: rssKeys.rules(instanceId) })
              }}
            >
              {tr("rssPage.actions.retry")}
            </Button>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  if (!rulesData || Object.keys(rulesData).length === 0) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center py-12">
          <FileText className="h-12 w-12 text-muted-foreground mb-4" />
          <p className="text-muted-foreground text-center">
            {tr("rssPage.empty.noRulesConfigured")}
          </p>
        </CardContent>
      </Card>
    )
  }

  const handleToggleRule = async (name: string, rule: RSSAutoDownloadRule) => {
    try {
      await setRule.mutateAsync({
        name,
        rule: { ...rule, enabled: !rule.enabled },
      })
      toast.success(rule.enabled ? tr("rssPage.toasts.ruleDisabled") : tr("rssPage.toasts.ruleEnabled"))
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.updateRuleFailed")
      toast.error(message)
    }
  }

  const handleRemoveRule = async (name: string) => {
    try {
      await removeRule.mutateAsync(name)
      toast.success(tr("rssPage.toasts.ruleRemoved"))
      if (selectedRuleName === name) {
        onRuleSelect(undefined)
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.removeRuleFailed")
      toast.error(message)
    }
  }

  const handleEditRule = (name: string, rule: RSSAutoDownloadRule) => {
    setEditingRule({ name, rule })
    setEditRuleOpen(true)
  }

  const rules = Object.entries(rulesData).sort(([a], [b]) => a.localeCompare(b))

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="flex-1 min-h-0 overflow-y-auto space-y-1 pr-1">
        {rules.map(([name, rule]) => (
          <RuleCard
            key={name}
            name={name}
            rule={rule}
            isSelected={selectedRuleName === name}
            onSelect={() => onRuleSelect(selectedRuleName === name ? undefined : name)}
            onToggle={() => handleToggleRule(name, rule)}
            onEdit={() => handleEditRule(name, rule)}
            onRemove={() => handleRemoveRule(name)}
          />
        ))}
      </div>

      {/* Rule Preview Sheet */}
      <RulePreviewSheet
        instanceId={instanceId}
        ruleName={selectedRuleName}
        open={!!selectedRuleName}
        onOpenChange={(open) => !open && onRuleSelect(undefined)}
      />

      {/* Edit Rule Dialog */}
      <EditRuleDialog
        instanceId={instanceId}
        open={editRuleOpen}
        onOpenChange={setEditRuleOpen}
        ruleName={editingRule?.name}
        rule={editingRule?.rule}
        feedsData={feedsData}
        categories={categories}
        tags={tags}
      />
    </div>
  )
}

// ============================================================================
// Rule Card
// ============================================================================

interface RuleCardProps {
  name: string
  rule: RSSAutoDownloadRule
  isSelected: boolean
  onSelect: () => void
  onToggle: () => void
  onEdit: () => void
  onRemove: () => void
}

function RuleCard({ name, rule, isSelected, onSelect, onToggle, onEdit, onRemove }: RuleCardProps) {
  const tr = useCommonTr()
  const category = rule.torrentParams?.category || rule.assignedCategory

  // Build compact filter summary
  const filterParts: string[] = []
  if (rule.mustContain) {
    const count = rule.mustContain.split("|").filter(Boolean).length
    filterParts.push(tr("rssPage.ruleCard.matchCount", { count }))
  }
  if (rule.mustNotContain) {
    const count = rule.mustNotContain.split("|").filter(Boolean).length
    filterParts.push(tr("rssPage.ruleCard.excludeCount", { count }))
  }
  if (rule.episodeFilter) {
    filterParts.push(tr("rssPage.ruleCard.episodeFilter", { value: rule.episodeFilter }))
  }
  const filterSummary = filterParts.join(" · ") || tr("rssPage.ruleCard.noFilters")

  return (
    <div
      className={`flex items-center gap-3 px-3 py-2 rounded-md border transition-colors ${
        isSelected? "border-primary bg-accent": "border-border hover:bg-accent"
      }`}
    >
      <Switch checked={rule.enabled} onCheckedChange={onToggle} />

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`text-sm truncate ${rule.enabled ? "font-medium" : "text-muted-foreground"}`}>
            {name}
          </span>
          {rule.useRegex && (
            <Badge variant="outline" className="h-5 px-1 text-[10px]">
              <Regex className="h-3 w-3" />
            </Badge>
          )}
          {rule.smartFilter && (
            <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">{tr("rssPage.ruleCard.smart")}</Badge>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="truncate">{filterSummary}</span>
          {category && (
            <>
              <span>·</span>
              <Tag className="h-3 w-3 shrink-0" />
              <span className="truncate">{category}</span>
            </>
          )}
          {rule.affectedFeeds.length > 0 && (
            <>
              <span>·</span>
              <Rss className="h-3 w-3 shrink-0" />
              <span>{rule.affectedFeeds.length}</span>
            </>
          )}
        </div>
      </div>

      <div className="flex items-center gap-0.5 shrink-0">
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onSelect}>
              <Search className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{tr("rssPage.tooltips.previewMatches")}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onEdit}>
              <Pencil className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{tr("rssPage.tooltips.editRule")}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7 hover:text-destructive" onClick={onRemove}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{tr("rssPage.tooltips.removeRule")}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
}

// ============================================================================
// Rule Preview Sheet
// ============================================================================

interface RulePreviewSheetProps {
  instanceId: number
  ruleName?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function RulePreviewSheet({ instanceId, ruleName, open, onOpenChange }: RulePreviewSheetProps) {
  const tr = useCommonTr()
  const { data: matchingArticles, isLoading } = useRSSMatchingArticles(instanceId, ruleName ?? "", {
    enabled: !!ruleName,
  })

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[500px] sm:max-w-[500px]">
        <SheetHeader>
          <SheetTitle>{tr("rssPage.preview.title")}</SheetTitle>
          <SheetDescription>{tr("rssPage.preview.description", { ruleName })}</SheetDescription>
        </SheetHeader>
        <div className="mt-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : matchingArticles && Object.keys(matchingArticles).length > 0 ? (
            <ScrollArea className="h-[calc(100vh-200px)]">
              <div className="space-y-4">
                {Object.entries(matchingArticles).map(([feedUrl, articles]) => (
                  <div key={feedUrl}>
                    <p className="text-sm font-medium text-muted-foreground mb-2 truncate">
                      {feedUrl}
                    </p>
                    <div className="space-y-1">
                      {articles.map((title, idx) => (
                        <div key={idx} className="text-sm py-1 px-2 bg-muted rounded">
                          {title}
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </ScrollArea>
          ) : (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <FileText className="h-12 w-12 mb-4" />
              <p>{tr("rssPage.empty.noMatchingArticlesFound")}</p>
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

// ============================================================================
// Add Feed Dialog
// ============================================================================

interface AddFeedDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  feedsData: RSSItems | undefined
}

const ROOT_FOLDER_VALUE = "__root__"

function AddFeedDialog({ instanceId, open, onOpenChange, feedsData }: AddFeedDialogProps) {
  const tr = useCommonTr()
  const [url, setUrl] = useState("")
  const [path, setPath] = useState(ROOT_FOLDER_VALUE)
  const addFeed = useAddRSSFeed(instanceId)

  const folders = useMemo(() => getFolderPaths(feedsData), [feedsData])

  const handleSubmit = async () => {
    if (!url.trim()) {
      toast.error(tr("rssPage.errors.urlRequired"))
      return
    }

    try {
      const result = await addFeed.mutateAsync({
        url: url.trim(),
        path: path === ROOT_FOLDER_VALUE ? undefined : path,
      })
      if (result?.warning) {
        toast.warning(result.warning)
      } else {
        toast.success(tr("rssPage.toasts.feedAdded"))
      }
      setUrl("")
      setPath(ROOT_FOLDER_VALUE)
      onOpenChange(false)
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.addFeedFailed")
      toast.error(message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{tr("rssPage.dialogs.addFeed.title")}</DialogTitle>
          <DialogDescription>{tr("rssPage.dialogs.addFeed.description")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="feed-url">{tr("rssPage.dialogs.addFeed.feedUrlLabel")}</Label>
            <Input
              id="feed-url"
              placeholder={tr("rssPage.dialogs.addFeed.feedUrlPlaceholder")}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="feed-path">{tr("rssPage.dialogs.addFeed.folderOptionalLabel")}</Label>
            <Select value={path} onValueChange={setPath}>
              <SelectTrigger>
                <SelectValue placeholder={tr("rssPage.dialogs.addFeed.root")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ROOT_FOLDER_VALUE}>{tr("rssPage.dialogs.addFeed.root")}</SelectItem>
                {folders.map((folder) => (
                  <SelectItem key={folder} value={folder}>
                    {folder}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tr("rssPage.actions.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={addFeed.isPending}>
            {addFeed.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {tr("rssPage.actions.addFeed")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Add Folder Dialog
// ============================================================================

interface AddFolderDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
}

function AddFolderDialog({ instanceId, open, onOpenChange }: AddFolderDialogProps) {
  const tr = useCommonTr()
  const [path, setPath] = useState("")
  const addFolder = useAddRSSFolder(instanceId)

  const handleSubmit = async () => {
    if (!path.trim()) {
      toast.error(tr("rssPage.errors.folderNameRequired"))
      return
    }

    try {
      await addFolder.mutateAsync({ path: path.trim() })
      toast.success(tr("rssPage.toasts.folderCreated"))
      setPath("")
      onOpenChange(false)
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.createFolderFailed")
      toast.error(message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{tr("rssPage.dialogs.addFolder.title")}</DialogTitle>
          <DialogDescription>
            {tr("rssPage.dialogs.addFolder.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="folder-path">{tr("rssPage.dialogs.addFolder.folderPathLabel")}</Label>
            <Input
              id="folder-path"
              placeholder={tr("rssPage.dialogs.addFolder.folderPathPlaceholder")}
              value={path}
              onChange={(e) => setPath(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tr("rssPage.actions.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={addFolder.isPending}>
            {addFolder.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {tr("rssPage.actions.createFolder")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Rule Form State & Fields (shared between Add and Edit dialogs)
// ============================================================================

interface RuleFormState {
  mustContain: string
  mustNotContain: string
  episodeFilter: string
  useRegex: boolean
  smartFilter: boolean
  affectedFeeds: string[]
  savePath: string
  category: string
  tags: string[]
  ignoreDays: number
  contentLayout: string
  addStopped: boolean | null
}

const DEFAULT_RULE_FORM_STATE: RuleFormState = {
  mustContain: "",
  mustNotContain: "",
  episodeFilter: "",
  useRegex: false,
  smartFilter: false,
  affectedFeeds: [],
  savePath: "",
  category: "",
  tags: [],
  ignoreDays: 0,
  contentLayout: "",
  addStopped: null,
}

interface RuleFormFieldsProps {
  state: RuleFormState
  onChange: <K extends keyof RuleFormState>(field: K, value: RuleFormState[K]) => void
  feedUrls: string[]
  categories: Record<string, Category>
  availableTags: string[]
  idPrefix: string
}

function RuleFormFields({
  state,
  onChange,
  feedUrls,
  categories,
  availableTags,
  idPrefix,
}: RuleFormFieldsProps) {
  const tr = useCommonTr()
  return (
    <>
      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-must-contain`}>{tr("rssPage.ruleForm.mustContainLabel")}</Label>
          <Input
            id={`${idPrefix}-must-contain`}
            placeholder={tr("rssPage.ruleForm.mustContainPlaceholder")}
            value={state.mustContain}
            onChange={(e) => onChange("mustContain", e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-must-not-contain`}>{tr("rssPage.ruleForm.mustNotContainLabel")}</Label>
          <Input
            id={`${idPrefix}-must-not-contain`}
            placeholder={tr("rssPage.ruleForm.mustNotContainPlaceholder")}
            value={state.mustNotContain}
            onChange={(e) => onChange("mustNotContain", e.target.value)}
          />
        </div>
      </div>

      <div className="space-y-2">
        <Label htmlFor={`${idPrefix}-episode-filter`}>{tr("rssPage.ruleForm.episodeFilterLabel")}</Label>
        <Input
          id={`${idPrefix}-episode-filter`}
          placeholder={tr("rssPage.ruleForm.episodeFilterPlaceholder")}
          value={state.episodeFilter}
          onChange={(e) => onChange("episodeFilter", e.target.value)}
        />
        <p className="text-xs text-muted-foreground">
          {tr("rssPage.ruleForm.episodeFilterHelp")}
        </p>
      </div>

      <div className="flex items-center gap-6">
        <div className="flex items-center gap-2">
          <Switch
            checked={state.useRegex}
            onCheckedChange={(v) => onChange("useRegex", v)}
            id={`${idPrefix}-use-regex`}
          />
          <Label htmlFor={`${idPrefix}-use-regex`}>{tr("rssPage.ruleForm.useRegexLabel")}</Label>
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={state.smartFilter}
            onCheckedChange={(v) => onChange("smartFilter", v)}
            id={`${idPrefix}-smart-filter`}
          />
          <Label htmlFor={`${idPrefix}-smart-filter`}>{tr("rssPage.ruleForm.smartEpisodeFilterLabel")}</Label>
        </div>
      </div>

      <Separator />

      <div className="space-y-2">
        <Label>{tr("rssPage.ruleForm.affectedFeedsLabel")}</Label>
        <div className="grid grid-cols-1 gap-2 max-h-32 overflow-y-auto">
          {feedUrls.map((feedUrl) => (
            <label key={feedUrl} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={state.affectedFeeds.includes(feedUrl)}
                onChange={(e) => {
                  if (e.target.checked) {
                    onChange("affectedFeeds", [...state.affectedFeeds, feedUrl])
                  } else {
                    onChange(
                      "affectedFeeds",
                      state.affectedFeeds.filter((f) => f !== feedUrl)
                    )
                  }
                }}
                className="rounded"
              />
              <span className="truncate">{feedUrl}</span>
            </label>
          ))}
          {feedUrls.length === 0 && (
            <p className="text-sm text-muted-foreground">{tr("rssPage.ruleForm.noFeedsAvailable")}</p>
          )}
        </div>
      </div>

      <Separator />

      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-save-path`}>{tr("rssPage.ruleForm.savePathLabel")}</Label>
          <Input
            id={`${idPrefix}-save-path`}
            placeholder={tr("rssPage.ruleForm.savePathPlaceholder")}
            value={state.savePath}
            onChange={(e) => onChange("savePath", e.target.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-category`}>{tr("rssPage.ruleForm.categoryLabel")}</Label>
          <Select
            value={state.category || "__none__"}
            onValueChange={(v) => onChange("category", v === "__none__" ? "" : v)}
          >
            <SelectTrigger id={`${idPrefix}-category`}>
              <SelectValue placeholder={tr("rssPage.ruleForm.selectCategoryPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">{tr("rssPage.ruleForm.none")}</SelectItem>
              {buildCategorySelectOptions(categories).map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-2">
        <Label>{tr("rssPage.ruleForm.tagsLabel")}</Label>
        <MultiSelect
          options={buildTagSelectOptions(availableTags, state.tags)}
          selected={state.tags}
          onChange={(v) => onChange("tags", v)}
          placeholder={tr("rssPage.ruleForm.selectTagsPlaceholder")}
          creatable
        />
      </div>

      <Separator />

      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-ignore-days`}>{tr("rssPage.ruleForm.ignoreSubsequentLabel")}</Label>
          <div className="flex items-center gap-2">
            <Input
              id={`${idPrefix}-ignore-days`}
              type="number"
              className="w-20"
              min={0}
              value={state.ignoreDays}
              onChange={(e) => onChange("ignoreDays", parseInt(e.target.value) || 0)}
            />
            <span className="text-sm text-muted-foreground">{tr("rssPage.ruleForm.days")}</span>
          </div>
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${idPrefix}-content-layout`}>{tr("rssPage.ruleForm.contentLayoutLabel")}</Label>
          <Select
            value={state.contentLayout || "__global__"}
            onValueChange={(v) => onChange("contentLayout", v === "__global__" ? "" : v)}
          >
            <SelectTrigger id={`${idPrefix}-content-layout`}>
              <SelectValue placeholder={tr("rssPage.ruleForm.useGlobalSettings")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__global__">{tr("rssPage.ruleForm.useGlobalSettings")}</SelectItem>
              <SelectItem value="Original">{tr("rssPage.ruleForm.contentLayoutOriginal")}</SelectItem>
              <SelectItem value="Subfolder">{tr("rssPage.ruleForm.contentLayoutSubfolder")}</SelectItem>
              <SelectItem value="NoSubfolder">{tr("rssPage.ruleForm.contentLayoutNoSubfolder")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-2">
        <Label htmlFor={`${idPrefix}-add-stopped`}>{tr("rssPage.ruleForm.addStoppedLabel")}</Label>
        <Select
          value={state.addStopped === null ? "__global__" : state.addStopped ? "true" : "false"}
          onValueChange={(v) => onChange("addStopped", v === "__global__" ? null : v === "true")}
        >
          <SelectTrigger id={`${idPrefix}-add-stopped`}>
            <SelectValue placeholder={tr("rssPage.ruleForm.useGlobalSettings")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__global__">{tr("rssPage.ruleForm.useGlobalSettings")}</SelectItem>
            <SelectItem value="true">{tr("rssPage.ruleForm.alwaysAddStopped")}</SelectItem>
            <SelectItem value="false">{tr("rssPage.ruleForm.neverAddStopped")}</SelectItem>
          </SelectContent>
        </Select>
      </div>
    </>
  )
}

// ============================================================================
// Add Rule Dialog
// ============================================================================

interface AddRuleDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  feedsData: RSSItems | undefined
  categories: Record<string, Category>
  tags: string[]
}

function AddRuleDialog({
  instanceId,
  open,
  onOpenChange,
  feedsData,
  categories,
  tags: availableTags,
}: AddRuleDialogProps) {
  const tr = useCommonTr()
  const [name, setName] = useState("")
  const [formState, setFormState] = useState<RuleFormState>(DEFAULT_RULE_FORM_STATE)

  const setRule = useSetRSSRule(instanceId)
  const feedUrls = useMemo(() => getFeedUrls(feedsData), [feedsData])

  const handleFieldChange = <K extends keyof RuleFormState>(field: K, value: RuleFormState[K]) => {
    setFormState((prev) => ({ ...prev, [field]: value }))
  }

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error(tr("rssPage.errors.ruleNameRequired"))
      return
    }

    try {
      await setRule.mutateAsync({
        name: name.trim(),
        rule: {
          enabled: true,
          priority: 0,
          useRegex: formState.useRegex,
          mustContain: formState.mustContain,
          mustNotContain: formState.mustNotContain,
          episodeFilter: formState.episodeFilter || undefined,
          affectedFeeds: formState.affectedFeeds,
          ignoreDays: formState.ignoreDays,
          smartFilter: formState.smartFilter,
          previouslyMatchedEpisodes: [],
          torrentParams: {
            save_path: formState.savePath || undefined,
            category: formState.category || undefined,
            tags: formState.tags.length > 0 ? formState.tags : undefined,
            content_layout: formState.contentLayout || undefined,
            stopped: formState.addStopped ?? undefined,
          },
        },
      })
      toast.success(tr("rssPage.toasts.ruleCreated"))
      setName("")
      setFormState(DEFAULT_RULE_FORM_STATE)
      onOpenChange(false)
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.createRuleFailed")
      toast.error(message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{tr("rssPage.dialogs.addRule.title")}</DialogTitle>
          <DialogDescription>
            {tr("rssPage.dialogs.addRule.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="add-rule-name">{tr("rssPage.dialogs.addRule.ruleNameLabel")}</Label>
            <Input
              id="add-rule-name"
              placeholder={tr("rssPage.dialogs.addRule.ruleNamePlaceholder")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <Separator />

          <RuleFormFields
            state={formState}
            onChange={handleFieldChange}
            feedUrls={feedUrls}
            categories={categories}
            availableTags={availableTags}
            idPrefix="add"
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tr("rssPage.actions.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={setRule.isPending}>
            {setRule.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {tr("rssPage.actions.createRule")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Edit Rule Dialog
// ============================================================================

interface EditRuleDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  ruleName?: string
  rule?: RSSAutoDownloadRule
  feedsData: RSSItems | undefined
  categories: Record<string, Category>
  tags: string[]
}

function EditRuleDialog({
  instanceId,
  open,
  onOpenChange,
  ruleName,
  rule,
  feedsData,
  categories,
  tags: availableTags,
}: EditRuleDialogProps) {
  const tr = useCommonTr()
  const [formState, setFormState] = useState<RuleFormState>(DEFAULT_RULE_FORM_STATE)
  const { formatDate } = useDateTimeFormatters()

  const setRuleMutation = useSetRSSRule(instanceId)
  const feedUrls = useMemo(() => getFeedUrls(feedsData), [feedsData])
  const lastMatchDate = useMemo(() => {
    if (!rule?.lastMatch) return null
    const parsed = new Date(rule.lastMatch)
    return Number.isNaN(parsed.getTime()) ? null : parsed
  }, [rule?.lastMatch])

  // Initialize form when rule changes
  useEffect(() => {
    if (rule) {
      setFormState({
        mustContain: rule.mustContain,
        mustNotContain: rule.mustNotContain,
        episodeFilter: rule.episodeFilter ?? "",
        useRegex: rule.useRegex,
        smartFilter: rule.smartFilter,
        affectedFeeds: rule.affectedFeeds,
        savePath: rule.torrentParams?.save_path ?? rule.savePath ?? "",
        category: rule.torrentParams?.category ?? rule.assignedCategory ?? "",
        tags: rule.torrentParams?.tags ?? [],
        ignoreDays: rule.ignoreDays ?? 0,
        contentLayout: rule.torrentParams?.content_layout ?? "",
        addStopped: rule.torrentParams?.stopped ?? null,
      })
    }
  }, [rule])

  const handleFieldChange = <K extends keyof RuleFormState>(field: K, value: RuleFormState[K]) => {
    setFormState((prev) => ({ ...prev, [field]: value }))
  }

  const handleSubmit = async () => {
    if (!ruleName || !rule) return

    try {
      await setRuleMutation.mutateAsync({
        name: ruleName,
        rule: {
          ...rule,
          useRegex: formState.useRegex,
          mustContain: formState.mustContain,
          mustNotContain: formState.mustNotContain,
          episodeFilter: formState.episodeFilter || undefined,
          affectedFeeds: formState.affectedFeeds,
          smartFilter: formState.smartFilter,
          ignoreDays: formState.ignoreDays,
          torrentParams: {
            ...rule.torrentParams,
            save_path: formState.savePath || undefined,
            category: formState.category || undefined,
            tags: formState.tags.length > 0 ? formState.tags : undefined,
            content_layout: formState.contentLayout || undefined,
            stopped: formState.addStopped ?? undefined,
          },
        },
      })
      toast.success(tr("rssPage.toasts.ruleUpdated"))
      onOpenChange(false)
    } catch (err) {
      const message = err instanceof Error ? err.message : tr("rssPage.toasts.updateRuleFailed")
      toast.error(message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{tr("rssPage.dialogs.editRule.title", { name: ruleName })}</DialogTitle>
          <DialogDescription>{tr("rssPage.dialogs.editRule.description")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <RuleFormFields
            state={formState}
            onChange={handleFieldChange}
            feedUrls={feedUrls}
            categories={categories}
            availableTags={availableTags}
            idPrefix="edit"
          />

          {lastMatchDate && (
            <div className="space-y-2">
              <Label>{tr("rssPage.dialogs.editRule.lastMatchLabel")}</Label>
              <p className="text-sm text-muted-foreground">
                {formatDate(lastMatchDate)}
              </p>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tr("rssPage.actions.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={setRuleMutation.isPending}>
            {setRuleMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {tr("rssPage.actions.saveChanges")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Helper Functions
// ============================================================================

function findFeedByPath(items: RSSItems, path: string): RSSFeed | null {
  const parts = path.split("\\")

  let current: RSSItems | RSSFeed = items
  for (const part of parts) {
    if (isRSSFeed(current)) return null
    const next = (current as RSSItems)[part]
    if (!next) return null
    current = next as RSSItems | RSSFeed
  }

  return isRSSFeed(current) ? current : null
}

function countFeeds(items: RSSItems): number {
  let count = 0
  for (const item of Object.values(items)) {
    if (isRSSFeed(item)) {
      count++
    } else {
      count += countFeeds(item as RSSItems)
    }
  }
  return count
}

function countUnreadArticles(items: RSSItems): number {
  let count = 0
  for (const item of Object.values(items)) {
    if (isRSSFeed(item)) {
      count += item.articles?.filter((a) => !a.isRead).length ?? 0
    } else {
      count += countUnreadArticles(item as RSSItems)
    }
  }
  return count
}

function getFolderPaths(items: RSSItems | undefined, prefix = ""): string[] {
  if (!items) return []

  const paths: string[] = []
  for (const [name, item] of Object.entries(items)) {
    if (!isRSSFeed(item)) {
      const path = prefix ? `${prefix}\\${name}` : name
      paths.push(path)
      paths.push(...getFolderPaths(item as RSSItems, path))
    }
  }
  return paths
}

function getFeedUrls(items: RSSItems | undefined): string[] {
  if (!items) return []

  const urls: string[] = []
  for (const item of Object.values(items)) {
    if (isRSSFeed(item)) {
      urls.push(item.url)
    } else {
      urls.push(...getFeedUrls(item as RSSItems))
    }
  }
  return urls
}

// ============================================================================
// RSS Settings Popover
// ============================================================================

function RssSettingsPopover({
  preferences,
  updatePreferences,
  isUpdating,
}: {
  preferences: AppPreferences | undefined
  updatePreferences: ReturnType<typeof useInstancePreferences>["updatePreferences"]
  isUpdating: boolean
}) {
  const tr = useCommonTr()
  const [refreshInterval, setRefreshInterval] = useState(preferences?.rss_refresh_interval ?? 30)
  const [maxArticles, setMaxArticles] = useState(preferences?.rss_max_articles_per_feed ?? 50)
  const [downloadRepack, setDownloadRepack] = useState(preferences?.rss_download_repack_proper_episodes ?? false)

  // Sync local state when preferences change
  useEffect(() => {
    setRefreshInterval(preferences?.rss_refresh_interval ?? 30)
    setMaxArticles(preferences?.rss_max_articles_per_feed ?? 50)
    setDownloadRepack(preferences?.rss_download_repack_proper_episodes ?? false)
  }, [preferences])

  const handleSave = () => {
    updatePreferences({
      rss_refresh_interval: refreshInterval,
      rss_max_articles_per_feed: maxArticles,
      rss_download_repack_proper_episodes: downloadRepack,
    }, {
      onSuccess: () => toast.success(tr("rssPage.toasts.rssSettingsSaved")),
      onError: () => toast.error(tr("rssPage.toasts.saveRssSettingsFailed")),
    })
  }

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button variant="ghost" size="icon" className="h-8 w-8">
              <Settings className="h-4 w-4" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>{tr("rssPage.settings.title")}</TooltipContent>
      </Tooltip>
      <PopoverContent className="w-72" align="end">
        <div className="space-y-4">
          <h4 className="font-medium">{tr("rssPage.settings.title")}</h4>

          <div className="space-y-3">
            <div className="grid grid-cols-[1fr_auto_auto] items-center gap-2">
              <Label className="text-sm">{tr("rssPage.settings.refreshIntervalLabel")}</Label>
              <Input
                type="number"
                className="w-16 h-8 text-center"
                min={1}
                value={refreshInterval}
                onChange={(e) => setRefreshInterval(parseInt(e.target.value) || 30)}
              />
              <span className="text-xs text-muted-foreground w-6">{tr("rssPage.settings.minutesShort")}</span>
            </div>

            <div className="grid grid-cols-[1fr_auto_auto] items-center gap-2">
              <Label className="text-sm">{tr("rssPage.settings.maxArticlesLabel")}</Label>
              <Input
                type="number"
                className="w-16 h-8 text-center"
                min={1}
                value={maxArticles}
                onChange={(e) => setMaxArticles(parseInt(e.target.value) || 50)}
              />
              <span className="w-6" />
            </div>
          </div>

          <Separator />

          {/* Auto-Download Settings */}
          <div className="space-y-3">
            <p className="text-xs text-muted-foreground">{tr("rssPage.settings.autoDownloadSection")}</p>

            <div className="flex items-center justify-between">
              <Label className="text-sm">{tr("rssPage.settings.downloadRepackProperLabel")}</Label>
              <Switch
                checked={downloadRepack}
                onCheckedChange={setDownloadRepack}
              />
            </div>
          </div>

          <Button onClick={handleSave} disabled={isUpdating} className="w-full">
            {isUpdating ? tr("rssPage.actions.saving") : tr("rssPage.actions.save")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
