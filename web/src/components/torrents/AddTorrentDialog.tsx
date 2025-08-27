/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible"
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
import { Plus, Upload, Link, ChevronDown } from "lucide-react"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePersistedStartPaused } from "@/hooks/usePersistedStartPaused"

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
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>("file")
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [advancedOpen, setAdvancedOpen] = useState(false)
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
      // Determine autoTMM setting: use instance preference unless user provided custom save path
      const hasCustomSavePath = data.savePath && data.savePath !== (preferences?.save_path || "")
      const autoTMM = hasCustomSavePath ? false : (preferences?.auto_tmm_enabled ?? true)
      
      const submitData: Parameters<typeof api.addTorrent>[1] = {
        startPaused: data.startPaused,
        savePath: hasCustomSavePath ? data.savePath : undefined,
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
        contentLayout: data.contentLayout || undefined,
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
            Add Torrent
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="sm:max-w-[525px]">
        <DialogHeader>
          <DialogTitle>Add New Torrent</DialogTitle>
          <DialogDescription>
            Add a torrent file or magnet link to start downloading
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            form.handleSubmit()
          }}
          className="space-y-4"
        >
          {/* Tab selection */}
          <div className="flex rounded-md bg-muted p-1">
            <button
              type="button"
              onClick={() => setActiveTab("file")}
              className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
                activeTab === "file"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
              }`}
            >
              <Upload className="mr-2 h-4 w-4 inline" />
              File
            </button>
            <button
              type="button"
              onClick={() => setActiveTab("url")}
              className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
                activeTab === "url"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
              }`}
            >
              <Link className="mr-2 h-4 w-4 inline" />
              URL
            </button>
          </div>

          {/* File upload or URL input */}
          {activeTab === "file" ? (
            <form.Field
              name="torrentFiles"
              validators={{
                onChange: ({ value }) => {
                  if ((!value || value.length === 0) && activeTab === "file") {
                    return "Please select at least one torrent file"
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="torrentFiles">Torrent Files</Label>
                  <Input
                    id="torrentFiles"
                    type="file"
                    accept=".torrent"
                    multiple
                    onChange={(e) => {
                      const files = e.target.files ? Array.from(e.target.files) : null
                      field.handleChange(files)
                    }}
                  />
                  {field.state.value && field.state.value.length > 0 && (
                    <p className="text-sm text-muted-foreground">
                      {field.state.value.length} file{field.state.value.length > 1 ? "s" : ""} selected
                    </p>
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
                    return "Please enter at least one URL or magnet link"
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="urls">URLs / Magnet Links</Label>
                  <Textarea
                    id="urls"
                    placeholder="Enter URLs or magnet links (one per line)"
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

          {/* Category */}
          <form.Field name="category">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="category">Category</Label>
                <Select
                  value={field.state.value}
                  onValueChange={field.handleChange}
                >
                  <SelectTrigger id="category">
                    <SelectValue placeholder="Select a category" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__none__">No category</SelectItem>
                    {categories && Object.entries(categories).map(([key, cat]) => (
                      <SelectItem key={key} value={cat.name}>
                        {cat.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
          </form.Field>

          {/* Tags */}
          <div className="space-y-4">
            {/* Existing tags */}
            {allAvailableTags && allAvailableTags.length > 0 && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label>Available Tags</Label>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setSelectedTags([])}
                    disabled={selectedTags.length === 0}
                  >
                    Deselect All
                  </Button>
                </div>
                <ScrollArea className="h-32 border rounded-md p-3">
                  <div className="space-y-2">
                    {allAvailableTags.map((tag) => (
                      <div key={tag} className="flex items-center space-x-2">
                        <Checkbox
                          id={`tag-${tag}`}
                          checked={selectedTags.includes(tag)}
                          onCheckedChange={(checked) => {
                            if (checked) {
                              setSelectedTags([...selectedTags, tag])
                            } else {
                              setSelectedTags(selectedTags.filter((t) => t !== tag))
                            }
                          }}
                        />
                        <label
                          htmlFor={`tag-${tag}`}
                          className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer flex items-center gap-1"
                        >
                          {tag}
                          {!availableTags?.includes(tag) && (
                            <span className="text-xs text-muted-foreground">(new)</span>
                          )}
                        </label>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </div>
            )}
            
            {/* Add new tag */}
            <div className="space-y-2">
              <Label htmlFor="newTag">Add New Tag</Label>
              <div className="flex gap-2">
                <Input
                  id="newTag"
                  value={newTag}
                  onChange={(e) => setNewTag(e.target.value)}
                  placeholder="Enter new tag"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && newTag.trim()) {
                      e.preventDefault()
                      if (!selectedTags.includes(newTag.trim())) {
                        setSelectedTags([...selectedTags, newTag.trim()])
                        setNewTag("")
                      }
                    }
                  }}
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    if (newTag.trim() && !selectedTags.includes(newTag.trim())) {
                      setSelectedTags([...selectedTags, newTag.trim()])
                      setNewTag("")
                    }
                  }}
                  disabled={!newTag.trim() || selectedTags.includes(newTag.trim())}
                >
                  <Plus className="h-4 w-4" />
                </Button>
              </div>
            </div>
            
            {/* Selected tags summary */}
            <div className="text-sm text-muted-foreground min-h-5">
              {selectedTags.length > 0 ? `Selected: ${selectedTags.join(", ")}` : "No tags selected"}
            </div>
          </div>

          {/* Save Path - only show when auto TMM is disabled */}
          {!preferences?.auto_tmm_enabled && (
            <form.Field name="savePath">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="savePath">Save Path</Label>
                  <Input
                    id="savePath"
                    placeholder={preferences?.save_path || "Leave empty for default"}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Automatic Torrent Management is disabled for this instance
                  </p>
                </div>
              )}
            </form.Field>
          )}
          
          {/* Auto TMM info when enabled */}
          {preferences?.auto_tmm_enabled && (
            <div className="space-y-2">
              <Label>Save Path</Label>
              <div className="px-3 py-2 bg-muted rounded-md">
                <p className="text-sm text-muted-foreground">
                  Automatic Torrent Management is enabled. Save path will be determined by category settings.
                </p>
              </div>
            </div>
          )}

          {/* Start Paused */}
          <form.Field name="startPaused">
            {(field) => (
              <div className="flex items-center space-x-2">
                <Switch
                  id="startPaused"
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor="startPaused">Start paused</Label>
              </div>
            )}
          </form.Field>

          {/* Skip Hash Check */}
          <form.Field name="skipHashCheck">
            {(field) => (
              <div className="flex items-center space-x-2">
                <Switch
                  id="skipHashCheck"
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor="skipHashCheck">Skip hash check</Label>
              </div>
            )}
          </form.Field>

          {/* Advanced Options */}
          <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
            <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md bg-muted px-4 py-2 text-sm font-medium hover:bg-muted/80">
              Advanced Options
              <ChevronDown 
                className={`h-4 w-4 transition-transform duration-200 ${advancedOpen ? "rotate-180" : ""}`} 
              />
            </CollapsibleTrigger>
            <CollapsibleContent className="space-y-4 pt-4">
              {/* Sequential Download */}
              <form.Field name="sequentialDownload">
                {(field) => (
                  <div className="flex items-center space-x-2">
                    <Switch
                      id="sequentialDownload"
                      checked={field.state.value}
                      onCheckedChange={field.handleChange}
                    />
                    <Label htmlFor="sequentialDownload">Sequential download</Label>
                    <span className="text-xs text-muted-foreground ml-2">
                      (useful for media files)
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
                    <Label htmlFor="firstLastPiecePrio">First/last piece priority</Label>
                    <span className="text-xs text-muted-foreground ml-2">
                      (start playback faster)
                    </span>
                  </div>
                )}
              </form.Field>

              {/* Speed Limits */}
              <div className="grid grid-cols-2 gap-4">
                <form.Field name="limitDownloadSpeed">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="limitDownloadSpeed">Download limit (KB/s)</Label>
                      <Input
                        id="limitDownloadSpeed"
                        type="number"
                        min="0"
                        placeholder="0 = unlimited"
                        value={field.state.value || ""}
                        onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                      />
                    </div>
                  )}
                </form.Field>

                <form.Field name="limitUploadSpeed">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="limitUploadSpeed">Upload limit (KB/s)</Label>
                      <Input
                        id="limitUploadSpeed"
                        type="number"
                        min="0"
                        placeholder="0 = unlimited"
                        value={field.state.value || ""}
                        onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                      />
                    </div>
                  )}
                </form.Field>
              </div>

              {/* Seeding Limits */}
              <div className="grid grid-cols-2 gap-4">
                <form.Field name="limitRatio">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="limitRatio">Ratio limit</Label>
                      <Input
                        id="limitRatio"
                        type="number"
                        min="0"
                        step="0.1"
                        placeholder="0 = use global"
                        value={field.state.value || ""}
                        onChange={(e) => field.handleChange(parseFloat(e.target.value) || 0)}
                      />
                    </div>
                  )}
                </form.Field>

                <form.Field name="limitSeedTime">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="limitSeedTime">Seed time limit (minutes)</Label>
                      <Input
                        id="limitSeedTime"
                        type="number"
                        min="0"
                        placeholder="0 = use global"
                        value={field.state.value || ""}
                        onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                      />
                    </div>
                  )}
                </form.Field>
              </div>

              {/* Content Layout */}
              {!preferences?.auto_tmm_enabled && (
                <form.Field name="contentLayout">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="contentLayout">Content layout</Label>
                      <Select
                        value={field.state.value}
                        onValueChange={field.handleChange}
                      >
                        <SelectTrigger id="contentLayout">
                          <SelectValue placeholder="Use global setting" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="">Use global setting</SelectItem>
                          <SelectItem value="Original">Original</SelectItem>
                          <SelectItem value="Subfolder">Create subfolder</SelectItem>
                          <SelectItem value="NoSubfolder">Don't create subfolder</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  )}
                </form.Field>
              )}

              {/* Rename Torrent */}
              <form.Field name="rename">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="rename">Rename torrent</Label>
                    <Input
                      id="rename"
                      placeholder="Leave empty to use original name"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </div>
                )}
              </form.Field>
            </CollapsibleContent>
          </Collapsible>

          {/* Auto-applied Settings Info */}
          {(preferences?.add_trackers_enabled && preferences?.add_trackers) || preferences?.excluded_file_names_enabled ? (
            <div className="bg-blue-50 dark:bg-blue-950 rounded-md p-3 text-sm">
              <p className="font-medium text-blue-900 dark:text-blue-100 mb-1">Auto-applied settings:</p>
              <ul className="text-blue-800 dark:text-blue-200 space-y-1">
                {preferences?.add_trackers_enabled && preferences?.add_trackers && (
                  <li>• Trackers will be automatically added</li>
                )}
                {preferences?.excluded_file_names_enabled && preferences?.excluded_file_names && (
                  <li>• Files matching patterns will be excluded: {preferences.excluded_file_names}</li>
                )}
              </ul>
            </div>
          ) : null}

          {/* Submit buttons */}
          <div className="flex gap-2 pt-4">
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
            >
              {([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  disabled={!canSubmit || isSubmitting || mutation.isPending}
                  className="flex-1"
                >
                  {isSubmitting || mutation.isPending ? "Adding..." : "Add Torrent"}
                </Button>
              )}
            </form.Subscribe>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}