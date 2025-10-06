/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useRef } from "react"
import { useForm } from "@tanstack/react-form"
import { useTranslation } from "react-i18next";
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger
} from "@/components/ui/tabs"
import { Plus, Upload, Link } from "lucide-react"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePersistedStartPaused } from "@/hooks/usePersistedStartPaused"
import { Badge } from "@/components/ui/badge"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"

interface AddTorrentDialogProps {
  instanceId: number
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

type TabValue = "file" | "url"

interface FormData {
  torrentFiles: File[] | null
  urls: string
  category: string
  tags: string[]
  startPaused: boolean
  autoTMM: boolean
  savePath: string
  skipHashCheck: boolean
  sequentialDownload: boolean
  firstLastPiecePrio: boolean
  limitUploadSpeed: number
  limitDownloadSpeed: number
  limitRatio: number
  limitSeedTime: number
  contentLayout: string
  rename: string
}

export function AddTorrentDialog({ instanceId, open: controlledOpen, onOpenChange }: AddTorrentDialogProps) {
  const { t } = useTranslation();
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>("file")
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [showFileList, setShowFileList] = useState(false)
  const [categorySearch, setCategorySearch] = useState("")
  const [tagSearch, setTagSearch] = useState("")
  const fileInputRef = useRef<HTMLInputElement>(null)
  const queryClient = useQueryClient()
  // NOTE: Use localStorage-persisted preference instead of qBittorrent's preference
  // This works around qBittorrent API not supporting start_paused_enabled setting
  const [startPausedEnabled] = usePersistedStartPaused(instanceId, false)

  // Use controlled state if provided, otherwise use internal state
  const open = controlledOpen !== undefined ? controlledOpen : internalOpen
  const setOpen = onOpenChange || setInternalOpen

  // Fetch metadata (categories, tags, preferences) with single API call
  const { data: metadata } = useInstanceMetadata(instanceId)
  const categories = metadata?.categories
  const availableTags = metadata?.tags
  const preferences = metadata?.preferences

  // Reset tag state when dialog closes
  useEffect(() => {
    if (!open) {
      setSelectedTags([])
      setNewTag("")
    }
  }, [open])


  // Combine API tags with temporarily added new tags and sort alphabetically
  const allAvailableTags = [...(availableTags || []), ...selectedTags.filter(tag => !availableTags?.includes(tag))].sort()

  const mutation = useMutation({
    retry: false, // Don't retry - could cause duplicate torrent additions
    mutationFn: async (data: FormData) => {
      // Use the user's explicit TMM choice
      const autoTMM = data.autoTMM

      const submitData: Parameters<typeof api.addTorrent>[1] = {
        startPaused: data.startPaused,
        savePath: !autoTMM && data.savePath ? data.savePath : undefined,
        autoTMM: autoTMM,
        category: data.category === "__none__" ? undefined : data.category || undefined,
        tags: data.tags.length > 0 ? data.tags : undefined,
        skipHashCheck: data.skipHashCheck,
        sequentialDownload: data.sequentialDownload,
        firstLastPiecePrio: data.firstLastPiecePrio,
        limitUploadSpeed: data.limitUploadSpeed > 0 ? data.limitUploadSpeed : undefined,
        limitDownloadSpeed: data.limitDownloadSpeed > 0 ? data.limitDownloadSpeed : undefined,
        limitRatio: data.limitRatio > 0 ? data.limitRatio : undefined,
        limitSeedTime: data.limitSeedTime > 0 ? data.limitSeedTime : undefined,
        contentLayout: data.contentLayout === "__global__" ? undefined : data.contentLayout || undefined,
        rename: data.rename || undefined,
      }

      if (activeTab === "file" && data.torrentFiles && data.torrentFiles.length > 0) {
        submitData.torrentFiles = data.torrentFiles
      } else if (activeTab === "url" && data.urls) {
        submitData.urls = data.urls.split("\n").map(u => u.trim()).filter(Boolean)
      }

      return api.addTorrent(instanceId, submitData)
    },
    onSuccess: () => {
      // Add small delay to allow qBittorrent to process the new torrent
      setTimeout(() => {
        // Use refetch instead of invalidate to avoid loading state
        queryClient.refetchQueries({
          queryKey: ["torrents-list", instanceId],
          exact: false,
          type: "active",
        })
        // Also refetch the metadata (categories, tags, counts)
        queryClient.refetchQueries({
          queryKey: ["instance-metadata", instanceId],
          exact: false,
          type: "active",
        })
      }, 500) // Give qBittorrent time to process
      setOpen(false)
      form.reset()
      setSelectedTags([])
      setNewTag("")
    },
  })

  const form = useForm({
    defaultValues: {
      torrentFiles: null as File[] | null,
      urls: "",
      category: "",
      tags: [] as string[],
      startPaused: startPausedEnabled,
      autoTMM: preferences?.auto_tmm_enabled ?? true,
      savePath: preferences?.save_path || "",
      skipHashCheck: false,
      sequentialDownload: false,
      firstLastPiecePrio: false,
      limitUploadSpeed: 0,
      limitDownloadSpeed: 0,
      limitRatio: 0,
      limitSeedTime: 0,
      contentLayout: preferences?.torrent_content_layout || "",
      rename: "",
    },
    onSubmit: async ({ value }) => {
      // Combine selected tags with any new tag
      const allTags = [...selectedTags]
      if (newTag.trim() && !allTags.includes(newTag.trim())) {
        allTags.push(newTag.trim())
      }
      await mutation.mutateAsync({ ...value, tags: allTags })
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {controlledOpen === undefined && (
        <DialogTrigger asChild>
          <Button>
            <Plus className="mr-2 h-4 w-4 transition-transform duration-200" />
            {t("add_torrent_dialog.add_torrent_button")}
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="flex flex-col w-full max-w-[95vw] sm:max-w-lg md:max-w-xl lg:max-w-2xl max-h-[90vh] sm:max-h-[85vh] p-0 !translate-y-0 !top-[5vh] sm:!top-[7.5vh]">
        <DialogHeader className="px-6 pt-6 pb-4 flex-shrink-0">
          <DialogTitle>{t("add_torrent_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("add_torrent_dialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto px-6">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              form.handleSubmit()
            }}
            className="space-y-4 pb-2"
          >
            {/* Tab selection */}
            <div className="flex rounded-md bg-muted p-1">
              <button
                type="button"
                onClick={() => setActiveTab("file")}
                className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors flex items-center justify-center ${
                  activeTab === "file"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                }`}
              >
                <Upload className="mr-2 h-4 w-4" />
                {t("add_torrent_dialog.tabs.file")}
              </button>
              <button
                type="button"
                onClick={() => setActiveTab("url")}
                className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors flex items-center justify-center ${
                  activeTab === "url"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                }`}
              >
                <Link className="mr-2 h-4 w-4" />
                {t("common.url")}
              </button>
            </div>

            {/* Main Content Tabs */}
            <Tabs defaultValue="basic" className="w-full">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="basic">{t("add_torrent_dialog.tabs.basic")}</TabsTrigger>
                <TabsTrigger value="advanced">{t("add_torrent_dialog.tabs.advanced")}</TabsTrigger>
              </TabsList>

              <TabsContent value="basic" className="space-y-4 mt-4">
                {/* File upload or URL input */}
                {activeTab === "file" ? (
                  <form.Field
                    name="torrentFiles"
                    validators={{
                      onChange: ({ value }) => {
                        if ((!value || value.length === 0) && activeTab === "file") {
                          return t("add_torrent_dialog.file_input.validation")
                        }
                        return undefined
                      },
                    }}
                  >
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="torrentFiles">{t("add_torrent_dialog.file_input.label")}</Label>
                        <Input
                          ref={fileInputRef}
                          id="torrentFiles"
                          type="file"
                          accept=".torrent"
                          multiple
                          className="sr-only"
                          onChange={(e) => {
                            const files = e.target.files ? Array.from(e.target.files) : null
                            field.handleChange(files)
                          }}
                        />
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => fileInputRef.current?.click()}
                          className="w-full"
                        >
                          <Upload className="mr-2 h-4 w-4" />
                          {t("add_torrent_dialog.file_input.button")}
                        </Button>
                        {field.state.value && field.state.value.length > 0 && (
                          <div className="flex items-center gap-2 text-sm text-muted-foreground">
                            <span>
                              {t(field.state.value.length > 1 ? "add_torrent_dialog.file_input.files_selected_other" : "add_torrent_dialog.file_input.files_selected_one", { count: field.state.value.length })}
                            </span>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  className="text-xs underline hover:text-foreground"
                                  onClick={() => setShowFileList(!showFileList)}
                                >
                                  {showFileList ? t("add_torrent_dialog.file_input.hide_files") : t("add_torrent_dialog.file_input.show_files")}
                                </button>
                              </TooltipTrigger>
                              <TooltipContent>
                                <div className="max-w-xs">
                                  {field.state.value.slice(0, 3).map((file, index) => (
                                    <div key={index} className="text-xs truncate">• {file.name}</div>
                                  ))}
                                  {field.state.value.length > 3 && (
                                    <div className="text-xs">{t("add_torrent_dialog.file_input.and_more", { count: field.state.value.length - 3 })}</div>
                                  )}
                                </div>
                              </TooltipContent>
                            </Tooltip>
                          </div>
                        )}
                        {showFileList && field.state.value && field.state.value.length > 0 && (
                          <div className="max-h-24 overflow-y-auto border rounded-md p-2">
                            <div className="text-xs text-muted-foreground space-y-0.5">
                              {field.state.value.map((file, index) => (
                                <div key={index} className="break-all">• {file.name}</div>
                              ))}
                            </div>
                          </div>
                        )}
                        {field.state.meta.isTouched && field.state.meta.errors[0] && (
                          <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                        )}
                      </div>
                    )}
                  </form.Field>
                ) : (
                  <form.Field
                    name="urls"
                    validators={{
                      onChange: ({ value }) => {
                        if (!value && activeTab === "url") {
                          return t("add_torrent_dialog.url_input.validation");
                        }
                        return undefined;
                      },
                    }}
                  >
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="urls">{t("add_torrent_dialog.url_input.label")}</Label>
                        <Textarea
                          id="urls"
                          placeholder={t("add_torrent_dialog.url_input.placeholder")}
                          rows={4}
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
                )}

                {/* Basic Toggles */}
                <div className="flex items-center justify-center gap-8">
                  <form.Field name="startPaused">
                    {(field) => (
                      <div className="flex items-center space-x-2">
                        <Switch
                          id="startPaused-left"
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                        <Label htmlFor="startPaused-left">{t("add_torrent_dialog.toggles.start_paused")}</Label>
                      </div>
                    )}
                  </form.Field>

                  <div className="w-px h-6 bg-border" />

                  <form.Field name="skipHashCheck">
                    {(field) => (
                      <div className="flex items-center space-x-2">
                        <Switch
                          id="skipHashCheck-left"
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                        />
                        <Label htmlFor="skipHashCheck-left">{t("add_torrent_dialog.toggles.skip_hash_check")}</Label>
                      </div>
                    )}
                  </form.Field>
                </div>

                {/* Category */}
                <div className="space-y-3">
                  <form.Field name="category">
                    {(field) => (
                      <>
                        {/* Header with search */}
                        <div className="flex items-center gap-2 w-full">
                          <Label className="shrink-0">{t("common.category")}</Label>
                          <Input
                            id="categorySearch"
                            value={categorySearch}
                            onChange={(e) => setCategorySearch(e.target.value)}
                            placeholder={t("add_torrent_dialog.category.search_placeholder")}
                            className="h-8 text-sm flex-1 min-w-0"
                            onKeyDown={(e) => {
                              if (e.key === "Enter" && categorySearch.trim()) {
                                e.preventDefault()
                                // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                const filtered = Object.entries(categories || {}).filter(([_key, cat]) =>
                                  cat.name.toLowerCase().includes(categorySearch.toLowerCase())
                                )

                                // If there's exactly one filtered category, select it
                                if (filtered.length === 1) {
                                  field.handleChange(filtered[0][1].name)
                                  setCategorySearch("")
                                }
                              }
                              if (e.key === "Escape") {
                                setCategorySearch("")
                              }
                            }}
                          />
                        </div>

                        {/* Available categories */}
                        {categories && Object.entries(categories).length > 0 && (
                          <div className="space-y-2">
                            <Label className="text-xs text-muted-foreground">
                              {t("add_torrent_dialog.category.available_categories")} {categorySearch && t("add_torrent_dialog.category.filtering", { search: categorySearch })}
                            </Label>
                            <div className="flex flex-wrap gap-1.5 max-h-20 overflow-y-auto">
                              {[
                                // Selected category first (if it matches search)
                                ...(field.state.value && field.state.value !== "__none__" &&
                                    (categorySearch === "" || field.state.value.toLowerCase().includes(categorySearch.toLowerCase()))? [{ name: field.state.value, isSelected: true }]: []),
                                // Then unselected categories
                                ...Object.entries(categories)
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .filter(([_key, cat]) => cat.name !== field.state.value)
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .filter(([_key, cat]) => categorySearch === "" || cat.name.toLowerCase().includes(categorySearch.toLowerCase()))
                                  // eslint-disable-next-line @typescript-eslint/no-unused-vars
                                  .map(([_key, cat]) => ({ name: cat.name, isSelected: false })),
                              ].map((cat) => (
                                <Badge
                                  key={cat.name}
                                  variant={field.state.value === cat.name ? "secondary" : "outline"}
                                  className="text-xs py-0.5 px-2 cursor-pointer hover:bg-accent"
                                  onClick={() => field.handleChange(field.state.value === cat.name ? "__none__" : cat.name)}
                                >
                                  {cat.name}
                                </Badge>
                              ))}
                            </div>
                            {/* eslint-disable-next-line @typescript-eslint/no-unused-vars */}
                            {categorySearch && Object.entries(categories).filter(([_key, cat]) => cat.name.toLowerCase().includes(categorySearch.toLowerCase())).length === 0 && (
                              <p className="text-xs text-muted-foreground">{t("add_torrent_dialog.category.no_match", { search: categorySearch })}</p>
                            )}
                          </div>
                        )}
                      </>
                    )}
                  </form.Field>
                </div>

                {/* Tags */}
                <div className="space-y-3 pt-2">
                  <div className="flex items-center gap-2 w-full">
                    <Label className="shrink-0">{t("common.tags")}</Label>
                    <Input
                      id="newTag"
                      value={newTag}
                      onChange={(e) => {
                        const value = e.target.value
                        setNewTag(value)
                        setTagSearch(value) // Update search filter
                      }}
                      placeholder={t("add_torrent_dialog.tags.search_placeholder")}
                      className="h-8 text-sm flex-1 min-w-0"
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && newTag.trim()) {
                          e.preventDefault()
                          const filteredAvailable = allAvailableTags?.filter(tag =>
                            !selectedTags.includes(tag) &&
                            tag.toLowerCase().includes(newTag.toLowerCase())
                          ) || []

                          // If there's exactly one filtered tag, add it
                          if (filteredAvailable.length === 1) {
                            setSelectedTags([...selectedTags, filteredAvailable[0]])
                            setNewTag("")
                            setTagSearch("")
                          }
                          // Otherwise, create new tag
                          else if (!selectedTags.includes(newTag.trim())) {
                            setSelectedTags([...selectedTags, newTag.trim()])
                            setNewTag("")
                            setTagSearch("")
                          }
                        }
                        if (e.key === "Escape") {
                          setNewTag("")
                          setTagSearch("")
                        }
                      }}
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        if (newTag.trim() && !selectedTags.includes(newTag.trim())) {
                          setSelectedTags([...selectedTags, newTag.trim()])
                          setNewTag("")
                          setTagSearch("")
                        }
                      }}
                      disabled={!newTag.trim() || selectedTags.includes(newTag.trim())}
                      className="h-8 px-2"
                    >
                      <Plus className="h-3 w-3" />
                    </Button>
                    {selectedTags.length > 0 && (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => setSelectedTags([])}
                        className="h-8 px-2 text-xs"
                      >
                        {t("add_torrent_dialog.tags.clear_all")}
                      </Button>
                    )}
                  </div>

                  {/* Available tags */}
                  {allAvailableTags && allAvailableTags.length > 0 && (
                    <div className="space-y-2">
                      <Label className="text-xs text-muted-foreground">
                        {t("add_torrent_dialog.tags.available_tags")} {tagSearch && t("add_torrent_dialog.category.filtering", { search: tagSearch })}
                      </Label>
                      <div className="flex flex-wrap gap-1.5 max-h-20 overflow-y-auto">
                        {[...selectedTags.filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase())),
                          ...allAvailableTags
                            .filter(tag => !selectedTags.includes(tag))
                            .filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase()))]
                          .map((tag) => (
                            <Badge
                              key={tag}
                              variant={selectedTags.includes(tag) ? "secondary" : "outline"}
                              className="text-xs py-0.5 px-2 cursor-pointer hover:bg-accent"
                              onClick={() => {
                                if (selectedTags.includes(tag)) {
                                  setSelectedTags(selectedTags.filter(t => t !== tag))
                                } else {
                                  setSelectedTags([...selectedTags, tag])
                                }
                              }}
                            >
                              {tag}
                              {!allAvailableTags.includes(tag) && (
                                <span className="ml-1 text-[10px] opacity-70">{t("add_torrent_dialog.tags.new")}</span>
                              )}
                            </Badge>
                          ))}
                      </div>
                      {tagSearch &&
                        [...selectedTags, ...allAvailableTags]
                          .filter(tag => tagSearch === "" || tag.toLowerCase().includes(tagSearch.toLowerCase()))
                          .length === 0 && (
                        <p className="text-xs text-muted-foreground">{t("add_torrent_dialog.tags.no_match", { search: tagSearch })}</p>
                      )}
                    </div>
                  )}
                </div>
              </TabsContent>

              <TabsContent value="advanced" className="space-y-4 mt-4">

                {/* Automatic Torrent Management */}
                <form.Field name="autoTMM">
                  {(field) => (
                    <div className="flex items-center space-x-2">
                      <Switch
                        id="autoTMM"
                        checked={field.state.value}
                        onCheckedChange={field.handleChange}
                      />
                      <Label htmlFor="autoTMM">{t("add_torrent_dialog.advanced.auto_tmm")}</Label>
                    </div>
                  )}
                </form.Field>

                {/* Save Path - show based on TMM toggle */}
                <form.Field name="autoTMM">
                  {(autoTMMField) => (
                    <>
                      {!autoTMMField.state.value ? (
                        <form.Field name="savePath">
                          {(field) => (
                            <div className="space-y-2">
                              <Label htmlFor="savePath">{t("add_torrent_dialog.advanced.save_path.label")}</Label>
                              <Input
                                id="savePath"
                                placeholder={preferences?.save_path || t("add_torrent_dialog.advanced.save_path.placeholder")}
                                value={field.state.value}
                                onBlur={field.handleBlur}
                                onChange={(e) => field.handleChange(e.target.value)}
                              />
                              <p className="text-xs text-muted-foreground">
                                {t("add_torrent_dialog.advanced.save_path.description")}
                              </p>
                            </div>
                          )}
                        </form.Field>
                      ) : (
                        <div className="space-y-2">
                          <Label>{t("add_torrent_dialog.advanced.save_path.label")}</Label>
                          <div className="px-3 py-2 bg-muted rounded-md">
                            <p className="text-sm text-muted-foreground">
                              {t("add_torrent_dialog.advanced.save_path.tmm_enabled")}
                            </p>
                          </div>
                        </div>
                      )}
                    </>
                  )}
                </form.Field>


                {/* Advanced Options */}
                <div className="space-y-4">
                  <Label className="text-sm font-medium">{t("add_torrent_dialog.advanced.advanced_options")}</Label>
                  {/* Sequential Download & First/Last Piece Priority */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <form.Field name="sequentialDownload">
                      {(field) => (
                        <div className="flex items-center space-x-2">
                          <Switch
                            id="sequentialDownload"
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                          />
                          <Label htmlFor="sequentialDownload">{t("add_torrent_dialog.advanced.sequential_download.label")}</Label>
                          <span className="text-xs text-muted-foreground ml-2">
                            {t("add_torrent_dialog.advanced.sequential_download.description")}
                          </span>
                        </div>
                      )}
                    </form.Field>

                    {/* First/Last Piece Priority */}
                    <form.Field name="firstLastPiecePrio">
                      {(field) => (
                        <div className="flex items-center space-x-2">
                          <Switch
                            id="firstLastPiecePrio"
                            checked={field.state.value}
                            onCheckedChange={field.handleChange}
                          />
                          <Label htmlFor="firstLastPiecePrio">{t("add_torrent_dialog.advanced.first_last_prio.label")}</Label>
                          <span className="text-xs text-muted-foreground ml-2">
                            {t("add_torrent_dialog.advanced.first_last_prio.description")}
                          </span>
                        </div>
                      )}
                    </form.Field>

                  </div>

                  {/* Speed Limits */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="limitDownloadSpeed">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitDownloadSpeed">{t("add_torrent_dialog.advanced.speed_limits.download")}</Label>
                          <Input
                            id="limitDownloadSpeed"
                            type="number"
                            min="0"
                            placeholder={t("add_torrent_dialog.advanced.speed_limits.placeholder")}
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="limitUploadSpeed">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitUploadSpeed">{t("add_torrent_dialog.advanced.speed_limits.upload")}</Label>
                          <Input
                            id="limitUploadSpeed"
                            type="number"
                            min="0"
                            placeholder={t("add_torrent_dialog.advanced.speed_limits.placeholder")}
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>

                  {/* Seeding Limits */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="limitRatio">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitRatio">{t("add_torrent_dialog.advanced.seeding_limits.ratio")}</Label>
                          <Input
                            id="limitRatio"
                            type="number"
                            min="0"
                            step="0.1"
                            placeholder={t("add_torrent_dialog.advanced.seeding_limits.placeholder")}
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseFloat(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="limitSeedTime">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="limitSeedTime">{t("add_torrent_dialog.advanced.seeding_limits.time")}</Label>
                          <Input
                            id="limitSeedTime"
                            type="number"
                            min="0"
                            placeholder={t("add_torrent_dialog.advanced.seeding_limits.placeholder")}
                            value={field.state.value || ""}
                            onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>

                  {/* Content Layout & Rename - available regardless of TMM */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <form.Field name="contentLayout">
                      {(field) => (
                        <div className="space-y-2">
                          <Label>{t("add_torrent_dialog.advanced.content_layout.label")}</Label>
                          <Select
                            value={field.state.value}
                            onValueChange={field.handleChange}
                          >
                            <SelectTrigger id="contentLayout">
                              <SelectValue placeholder={t("add_torrent_dialog.advanced.content_layout.global")} />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="__global__">{t("add_torrent_dialog.advanced.content_layout.global")}</SelectItem>
                              <SelectItem value="Original">{t("add_torrent_dialog.advanced.content_layout.original")}</SelectItem>
                              <SelectItem value="Subfolder">{t("add_torrent_dialog.advanced.content_layout.subfolder")}</SelectItem>
                              <SelectItem value="NoSubfolder">{t("add_torrent_dialog.advanced.content_layout.no_subfolder")}</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                      )}
                    </form.Field>

                    {/* Rename Torrent */}
                    <form.Field name="rename">
                      {(field) => (
                        <div className="space-y-2">
                          <Label htmlFor="rename">{t("add_torrent_dialog.advanced.rename.label")}</Label>
                          <Input
                            id="rename"
                            placeholder={t("add_torrent_dialog.advanced.rename.placeholder")}
                            value={field.state.value}
                            onChange={(e) => field.handleChange(e.target.value)}
                          />
                        </div>
                      )}
                    </form.Field>
                  </div>
                </div>
              </TabsContent>
            </Tabs>

            {/* Auto-applied Settings Info - Compact */}
            {(preferences?.add_trackers_enabled && preferences?.add_trackers) || preferences?.excluded_file_names_enabled ? (
              <div className="bg-muted rounded-md p-3 text-xs text-muted-foreground">
                <p className="font-medium mb-1">{t("add_torrent_dialog.advanced.auto_applied.label")}</p>
                <div className="space-y-0.5">
                  {preferences?.add_trackers_enabled && preferences?.add_trackers && (
                    <div>• {t("add_torrent_dialog.advanced.auto_applied.trackers")}</div>
                  )}
                  {preferences?.excluded_file_names_enabled && preferences?.excluded_file_names && (
                    <div>• {t("add_torrent_dialog.advanced.auto_applied.exclusions", { exclusions: preferences.excluded_file_names })}</div>
                  )}
                </div>
              </div>
            ) : null}

          </form>
        </div>

        {/* Fixed footer with submit buttons */}
        <div className="flex-shrink-0 px-6 py-3 border-t bg-background">
          <div className="flex flex-col sm:flex-row gap-3 sm:gap-2">
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
            >
              {([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  disabled={!canSubmit || isSubmitting || mutation.isPending}
                  className="w-full sm:flex-1 h-11 sm:h-10 order-1 sm:order-2"
                  onClick={() => form.handleSubmit()}
                >
                  {isSubmitting || mutation.isPending ? t("add_torrent_dialog.adding") : t("add_torrent_dialog.add_torrent_button")}
                </Button>
              )}
            </form.Subscribe>
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto px-6 sm:px-4 h-11 sm:h-10 order-2 sm:order-1"
              onClick={() => setOpen(false)}
            >
              {t("common.cancel")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}