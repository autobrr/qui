/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, useRef } from "react"
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
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>("file")
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [advancedOpen, setAdvancedOpen] = useState(false)
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

  // Auto-collapse advanced options on mobile
  useEffect(() => {
    if (open && typeof window !== "undefined") {
      const isMobile = window.innerWidth < 640
      if (isMobile) {
        setAdvancedOpen(false)
      }
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
            Add Torrent
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="flex flex-col w-full max-w-[95vw] sm:max-w-md md:max-w-lg lg:max-w-xl xl:max-w-2xl max-h-[90vh] sm:max-h-[85vh] p-0">
        <DialogHeader className="px-6 pt-6 pb-4 flex-shrink-0">
          <DialogTitle>Add New Torrent</DialogTitle>
          <DialogDescription>
            Add a torrent file or magnet link to start downloading
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto px-6">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              form.handleSubmit()
            }}
            className="space-y-4 pb-4"
          >
            {/* Tab selection */}
            <div className="flex rounded-md bg-muted p-1">
              <button
                type="button"
                onClick={() => setActiveTab("file")}
                className={`flex-1 rounded-sm px-3 py-2 text-sm font-medium min-h-[44px] transition-colors flex items-center justify-center ${
                  activeTab === "file"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                }`}
              >
                <Upload className="mr-2 h-4 w-4" />
                File
              </button>
              <button
                type="button"
                onClick={() => setActiveTab("url")}
                className={`flex-1 rounded-sm px-3 py-2 text-sm font-medium min-h-[44px] transition-colors flex items-center justify-center ${
                  activeTab === "url"? "bg-accent text-accent-foreground shadow-sm": "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                }`}
              >
                <Link className="mr-2 h-4 w-4" />
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
                      Browse for Torrent Files
                    </Button>
                    {field.state.value && field.state.value.length > 0 && (
                      <div className="space-y-1">
                        <p className="text-sm text-muted-foreground">
                          {field.state.value.length} file{field.state.value.length > 1 ? "s" : ""} selected:
                        </p>
                        <div className="max-h-32 overflow-y-auto border rounded-md p-3">
                          <div className="text-xs text-muted-foreground space-y-1">
                            {field.state.value.map((file, index) => (
                              <div key={index} className="break-all">• {file.name}</div>
                            ))}
                          </div>
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
                  <Label>Category</Label>
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
                  <ScrollArea className="h-24 sm:h-32 border rounded-md p-2 sm:p-3">
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
                <Label>Add New Tag</Label>
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

            {/* Automatic Torrent Management */}
            <form.Field name="autoTMM">
              {(field) => (
                <div className="flex items-center space-x-2">
                  <Switch
                    id="autoTMM"
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                  />
                  <Label htmlFor="autoTMM">Automatic Torrent Management</Label>
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
                          <Label htmlFor="savePath">Save Path</Label>
                          <Input
                            id="savePath"
                            placeholder={preferences?.save_path || "Leave empty for default"}
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                          />
                          <p className="text-xs text-muted-foreground">
                            Manual save path (TMM disabled)
                          </p>
                        </div>
                      )}
                    </form.Field>
                  ) : (
                    <div className="space-y-2">
                      <Label>Save Path</Label>
                      <div className="px-3 py-2 bg-muted rounded-md">
                        <p className="text-sm text-muted-foreground">
                          Automatic Torrent Management is enabled. Save path will be determined by category settings.
                        </p>
                      </div>
                    </div>
                  )}
                </>
              )}
            </form.Field>

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
              <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md bg-muted px-4 py-3 text-sm font-medium hover:bg-muted/80 transition-colors min-h-[44px]">
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
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
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
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
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

                {/* Content Layout - available regardless of TMM */}
                <form.Field name="contentLayout">
                  {(field) => (
                    <div className="space-y-2">
                      <Label>Content layout</Label>
                      <Select
                        value={field.state.value}
                        onValueChange={field.handleChange}
                      >
                        <SelectTrigger id="contentLayout">
                          <SelectValue placeholder="Use global setting" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="__global__">Use global setting</SelectItem>
                          <SelectItem value="Original">Original</SelectItem>
                          <SelectItem value="Subfolder">Create subfolder</SelectItem>
                          <SelectItem value="NoSubfolder">Don't create subfolder</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  )}
                </form.Field>

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

            {/* Auto-applied Settings Info - Compact */}
            {(preferences?.add_trackers_enabled && preferences?.add_trackers) || preferences?.excluded_file_names_enabled ? (
              <div className="bg-muted rounded-md p-3 text-xs text-muted-foreground">
                <p className="font-medium mb-1">Auto-applied:</p>
                <div className="space-y-0.5">
                  {preferences?.add_trackers_enabled && preferences?.add_trackers && (
                    <div>• Auto-add trackers</div>
                  )}
                  {preferences?.excluded_file_names_enabled && preferences?.excluded_file_names && (
                    <div>• File exclusions: {preferences.excluded_file_names}</div>
                  )}
                </div>
              </div>
            ) : null}

          </form>
        </div>

        {/* Fixed footer with submit buttons */}
        <div className="flex-shrink-0 px-6 py-4 border-t bg-background">
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
                  {isSubmitting || mutation.isPending ? "Adding..." : "Add Torrent"}
                </Button>
              )}
            </form.Subscribe>
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto px-6 sm:px-4 h-11 sm:h-10 order-2 sm:order-1"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}