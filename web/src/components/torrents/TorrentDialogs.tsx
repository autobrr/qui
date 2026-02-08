/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import type { Category, InstanceCapabilities, Torrent } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import { AlertTriangle, Loader2, Plus, X } from "lucide-react"
import type { ChangeEvent, KeyboardEvent } from "react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { cn } from "@/lib/utils"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { buildCategoryTree, type CategoryNode } from "./CategoryTree"

interface SetTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  initialTags?: string[]
  isLoadingTags?: boolean
}

interface AddTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  initialTags?: string[]
  isLoadingTags?: boolean
}

export const AddTagsDialog = memo(function AddTagsDialog({
  open,
  onOpenChange,
  availableTags,
  hashCount,
  onConfirm,
  isPending = false,
  initialTags = [],
  isLoadingTags = false,
}: AddTagsDialogProps) {
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [temporaryTags, setTemporaryTags] = useState<string[]>([])
  const wasOpen = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // Initialize selected tags only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setSelectedTags([]) // Start with empty selection for add operation
      setTemporaryTags([])
    }
    wasOpen.current = open
  }, [open, initialTags])

  // Combine server tags with temporary tags for display
  const displayTags = [...(availableTags || []), ...temporaryTags].sort()

  // Only use virtualization for large tag lists (>50 tags)
  const shouldUseVirtualization = displayTags.length > 50

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? displayTags.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 32, // Approximate height of each tag item
    overscan: 5,
  })

  const handleConfirm = useCallback((): void => {
    const allTags = [...selectedTags]
    if (newTag.trim() && !allTags.includes(newTag.trim())) {
      allTags.push(newTag.trim())
    }
    onConfirm(allTags)
    setSelectedTags([])
    setNewTag("")
    setTemporaryTags([])
  }, [selectedTags, newTag, onConfirm])

  const handleCancel = useCallback((): void => {
    setSelectedTags([])
    setNewTag("")
    setTemporaryTags([])
    onOpenChange(false)
  }, [onOpenChange])

  const addNewTag = useCallback((tagToAdd: string): void => {
    const trimmedTag = tagToAdd.trim()
    if (trimmedTag && !displayTags.includes(trimmedTag)) {
      // Add to temporary tags if it's not already in server tags
      if (!availableTags?.includes(trimmedTag)) {
        setTemporaryTags(prev => [...prev, trimmedTag])
      }
      // Add to selected tags
      setSelectedTags(prev => [...prev, trimmedTag])
      setNewTag("")
    }
  }, [displayTags, availableTags])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Add Tags to {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Select tags to add to the selected torrents. These tags will be added to any existing tags on each torrent.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {/* Existing tags */}
          {isLoadingTags ? (
            <div className="space-y-2">
              <Label>Available Tags</Label>
              <div className="h-48 border rounded-md p-3 flex items-center justify-center">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">Loading tags...</span>
                </div>
              </div>
            </div>
          ) : displayTags && displayTags.length > 0 ? (
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
              <div
                ref={scrollContainerRef}
                className="h-48 border rounded-md p-3 overflow-y-auto"
              >
                {shouldUseVirtualization ? (
                  // Virtualized rendering for large tag lists
                  <div
                    style={{
                      height: `${virtualizer.getTotalSize()}px`,
                      width: "100%",
                      position: "relative",
                    }}
                  >
                    {virtualizer.getVirtualItems().map((virtualRow) => {
                      const tag = displayTags[virtualRow.index]
                      const isTemporary = temporaryTags.includes(tag)
                      return (
                        <div
                          key={virtualRow.key}
                          data-index={virtualRow.index}
                          ref={virtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        >
                          <div className="flex items-center space-x-2 py-1">
                            <Checkbox
                              id={`add-tag-${tag}`}
                              checked={selectedTags.includes(tag)}
                              onCheckedChange={(checked: boolean | string) => {
                                if (checked) {
                                  setSelectedTags([...selectedTags, tag])
                                } else {
                                  setSelectedTags(selectedTags.filter((t: string) => t !== tag))
                                }
                              }}
                            />
                            <label
                              htmlFor={`add-tag-${tag}`}
                              className={`text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer ${
                                isTemporary ? "text-primary italic" : ""
                              }`}
                            >
                              {tag}
                              {isTemporary && <span className="ml-1 text-xs text-muted-foreground">(new)</span>}
                            </label>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  // Simple rendering for small tag lists - faster!
                  <div className="space-y-1">
                    {displayTags.map((tag) => {
                      const isTemporary = temporaryTags.includes(tag)
                      return (
                        <div key={tag} className="flex items-center space-x-2 py-1">
                          <Checkbox
                            id={`add-tag-${tag}`}
                            checked={selectedTags.includes(tag)}
                            onCheckedChange={(checked: boolean | string) => {
                              if (checked) {
                                setSelectedTags([...selectedTags, tag])
                              } else {
                                setSelectedTags(selectedTags.filter((t: string) => t !== tag))
                              }
                            }}
                          />
                          <label
                            htmlFor={`add-tag-${tag}`}
                            className={`text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer ${
                              isTemporary ? "text-primary italic" : ""
                            }`}
                          >
                            {tag}
                            {isTemporary && <span className="ml-1 text-xs text-muted-foreground">(new)</span>}
                          </label>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            </div>
          ) : null}

          {/* Add new tag */}
          <div className="space-y-2">
            <Label htmlFor="newTag">Create New Tag</Label>
            <div className="flex gap-2">
              <Input
                id="newTag"
                value={newTag}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setNewTag(e.target.value)}
                placeholder="Enter new tag"
                onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                  if (e.key === "Enter" && newTag.trim()) {
                    e.preventDefault()
                    addNewTag(newTag)
                  }
                }}
              />
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => addNewTag(newTag)}
                disabled={!newTag.trim() || displayTags.includes(newTag.trim())}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </div>
          </div>

          {/* Selected tags summary */}
          {selectedTags.length > 0 && (
            <div className="text-sm text-muted-foreground">
              Tags to add: {selectedTags.join(", ")}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>Cancel</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || selectedTags.length === 0}
          >
            Add Tags
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

export const SetTagsDialog = memo(function SetTagsDialog({
  open,
  onOpenChange,
  availableTags,
  hashCount,
  onConfirm,
  isPending = false,
  initialTags = [],
  isLoadingTags = false,
}: SetTagsDialogProps) {
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [newTag, setNewTag] = useState("")
  const [temporaryTags, setTemporaryTags] = useState<string[]>([]) // New state for temporarily created tags
  const wasOpen = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // Initialize selected tags only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setSelectedTags(initialTags)
      setTemporaryTags([]) // Clear temporary tags when opening dialog
    }
    wasOpen.current = open
  }, [open, initialTags])

  // Combine server tags with temporary tags for display
  const displayTags = [...(availableTags || []), ...temporaryTags].sort()

  // Only use virtualization for large tag lists (>50 tags)
  const shouldUseVirtualization = displayTags.length > 50

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? displayTags.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 32, // Approximate height of each tag item
    overscan: 5,
  })

  const handleConfirm = useCallback((): void => {
    const allTags = [...selectedTags]
    if (newTag.trim() && !allTags.includes(newTag.trim())) {
      allTags.push(newTag.trim())
    }
    onConfirm(allTags)
    setSelectedTags([])
    setNewTag("")
    setTemporaryTags([]) // Clear temporary tags after confirming
  }, [selectedTags, newTag, onConfirm])

  const handleCancel = useCallback((): void => {
    setSelectedTags([])
    setNewTag("")
    setTemporaryTags([]) // Clear temporary tags when cancelling
    onOpenChange(false)
  }, [onOpenChange])

  const addNewTag = useCallback((tagToAdd: string): void => {
    const trimmedTag = tagToAdd.trim()
    if (trimmedTag && !displayTags.includes(trimmedTag)) {
      // Add to temporary tags if it's not already in server tags
      if (!availableTags?.includes(trimmedTag)) {
        setTemporaryTags(prev => [...prev, trimmedTag])
      }
      // Add to selected tags
      setSelectedTags(prev => [...prev, trimmedTag])
      setNewTag("")
    }
  }, [displayTags, availableTags])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Replace Tags for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Select tags from the list or add a new one. Selected tags will replace all existing tags on the torrents. Leave all unchecked to remove all tags.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {/* Existing tags */}
          {isLoadingTags ? (
            <div className="space-y-2">
              <Label>Available Tags</Label>
              <div className="h-48 border rounded-md p-3 flex items-center justify-center">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">Loading tags...</span>
                </div>
              </div>
            </div>
          ) : displayTags && displayTags.length > 0 ? (
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
              <div
                ref={scrollContainerRef}
                className="h-48 border rounded-md p-3 overflow-y-auto"
              >
                {shouldUseVirtualization ? (
                  // Virtualized rendering for large tag lists
                  <div
                    style={{
                      height: `${virtualizer.getTotalSize()}px`,
                      width: "100%",
                      position: "relative",
                    }}
                  >
                    {virtualizer.getVirtualItems().map((virtualRow) => {
                      const tag = displayTags[virtualRow.index]
                      const isTemporary = temporaryTags.includes(tag)
                      return (
                        <div
                          key={virtualRow.key}
                          data-index={virtualRow.index}
                          ref={virtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        >
                          <div className="flex items-center space-x-2 py-1">
                            <Checkbox
                              id={`tag-${tag}`}
                              checked={selectedTags.includes(tag)}
                              onCheckedChange={(checked: boolean | string) => {
                                if (checked) {
                                  setSelectedTags([...selectedTags, tag])
                                } else {
                                  setSelectedTags(selectedTags.filter((t: string) => t !== tag))
                                }
                              }}
                            />
                            <label
                              htmlFor={`tag-${tag}`}
                              className={`text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer ${
                                isTemporary ? "text-primary italic" : ""
                              }`}
                            >
                              {tag}
                              {isTemporary && <span className="ml-1 text-xs text-muted-foreground">(new)</span>}
                            </label>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  // Simple rendering for small tag lists - faster!
                  <div className="space-y-1">
                    {displayTags.map((tag) => {
                      const isTemporary = temporaryTags.includes(tag)
                      return (
                        <div key={tag} className="flex items-center space-x-2 py-1">
                          <Checkbox
                            id={`tag-${tag}`}
                            checked={selectedTags.includes(tag)}
                            onCheckedChange={(checked: boolean | string) => {
                              if (checked) {
                                setSelectedTags([...selectedTags, tag])
                              } else {
                                setSelectedTags(selectedTags.filter((t: string) => t !== tag))
                              }
                            }}
                          />
                          <label
                            htmlFor={`tag-${tag}`}
                            className={`text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer ${
                              isTemporary ? "text-primary italic" : ""
                            }`}
                          >
                            {tag}
                            {isTemporary && <span className="ml-1 text-xs text-muted-foreground">(new)</span>}
                          </label>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            </div>
          ) : null}

          {/* Add new tag */}
          <div className="space-y-2">
            <Label htmlFor="newTag">Add New Tag</Label>
            <div className="flex gap-2">
              <Input
                id="newTag"
                value={newTag}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setNewTag(e.target.value)}
                placeholder="Enter new tag"
                onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                  if (e.key === "Enter" && newTag.trim()) {
                    e.preventDefault()
                    addNewTag(newTag)
                  }
                }}
              />
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => addNewTag(newTag)}
                disabled={!newTag.trim() || displayTags.includes(newTag.trim())}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </div>
          </div>

          {/* Selected tags summary */}
          {selectedTags.length > 0 && (
            <div className="text-sm text-muted-foreground">
              Selected: {selectedTags.join(", ")}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>Cancel</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending}
          >
            Replace Tags
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface SetCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableCategories: Record<string, Category>
  hashCount: number
  onConfirm: (category: string) => void
  isPending?: boolean
  initialCategory?: string
  isLoadingCategories?: boolean
  useSubcategories?: boolean
}

interface SetLocationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (location: string) => void
  isPending?: boolean
  initialLocation?: string
  instanceId?: number
  capabilities?: InstanceCapabilities | null
}

export const SetLocationDialog = memo(function SetLocationDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
  initialLocation = "",
  instanceId = 0,
  capabilities,
}: SetLocationDialogProps) {
  const [location, setLocation] = useState("")
  const wasOpen = useRef(false)

  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false

  const {
    suggestions,
    handleInputChange: handleAutocompleteChange,
    handleSelect,
    handleKeyDown: handleAutocompleteKeyDown,
    handleBlur: handleAutocompleteBlur,
    highlightedIndex,
    showSuggestions,
    inputRef: autocompleteInputRef,
  } = usePathAutocomplete(setLocation, instanceId)

  const inputRef = useRef<HTMLInputElement>(null)
  const effectiveInputRef = supportsPathAutocomplete ? autocompleteInputRef : inputRef

  // Initialize location only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setLocation(initialLocation)
      if (supportsPathAutocomplete) {
        handleAutocompleteChange(initialLocation)
      }
      // Focus the input when dialog opens
      setTimeout(() => effectiveInputRef.current?.focus(), 0)
    }
    wasOpen.current = open
  }, [open, initialLocation, supportsPathAutocomplete, handleAutocompleteChange, effectiveInputRef])

  const handleConfirm = useCallback(() => {
    if (location.trim()) {
      onConfirm(location.trim())
      setLocation("")
    }
  }, [location, onConfirm])

  const handleCancel = useCallback(() => {
    setLocation("")
    onOpenChange(false)
  }, [onOpenChange])

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    if (supportsPathAutocomplete) {
      handleAutocompleteKeyDown(e)
    }
    if (e.key === "Enter" && !isPending && location.trim()) {
      e.preventDefault()
      handleConfirm()
    }
  }, [isPending, location, handleConfirm, supportsPathAutocomplete, handleAutocompleteKeyDown])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Set Location for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Enter the new save location for the selected torrents. This will disable Auto TMM and move the torrents to the specified location.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="location">Location</Label>
            <Input
              ref={effectiveInputRef}
              id="location"
              type="text"
              autoComplete="off"
              spellCheck={false}
              value={location}
              onChange={(e: ChangeEvent<HTMLInputElement>) => {
                setLocation(e.target.value)
                if (supportsPathAutocomplete) {
                  handleAutocompleteChange(e.target.value)
                }
              }}
              onKeyDown={handleKeyDown}
              onBlur={supportsPathAutocomplete ? handleAutocompleteBlur : undefined}
              placeholder="/path/to/save/location"
              disabled={isPending}
            />
            {supportsPathAutocomplete && showSuggestions && suggestions.length > 0 && (
              <div className="relative">
                <div className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
                  <div className="max-h-55 overflow-y-auto py-1">
                    {suggestions.map((entry, idx) => (
                      <button
                        key={entry}
                        type="button"
                        title={entry}
                        className={cn(
                          "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                          highlightedIndex === idx
                            ? "bg-accent text-accent-foreground"
                            : "hover:bg-accent/70",
                        )}
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => handleSelect(entry)}
                      >
                        <span className="block truncate text-left">{entry}</span>
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel} disabled={isPending}>
            Cancel
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !location.trim()}
          >
            Set Location
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentName?: string
  onConfirm: (name: string) => void | Promise<void>
  isPending?: boolean
}

export const RenameTorrentDialog = memo(function RenameTorrentDialog({
  open,
  onOpenChange,
  currentName = "",
  onConfirm,
  isPending = false,
}: RenameTorrentDialogProps) {
  const [name, setName] = useState("")
  const wasOpen = useRef(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open && !wasOpen.current) {
      setName(currentName)
      setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 0)
    }
    wasOpen.current = open
  }, [open, currentName])

  const handleConfirm = useCallback(() => {
    const trimmed = name.trim()
    if (!trimmed) return
    onConfirm(trimmed)
  }, [name, onConfirm])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Rename Torrent</DialogTitle>
          <DialogDescription>
            Update the display name for this torrent. This changes how it appears in qBittorrent and qui.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="torrentName">Torrent Name</Label>
            <Input
              ref={inputRef}
              id="torrentName"
              value={name}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
              placeholder="Enter new torrent name"
              disabled={isPending}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                if (e.key === "Enter" && !isPending && name.trim()) {
                  e.preventDefault()
                  handleConfirm()
                }
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button onClick={handleConfirm} disabled={isPending || !name.trim()}>
            Rename
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentFileDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  files?: { name: string }[]
  isLoading?: boolean
  onConfirm: (payload: { oldPath: string; newPath: string }) => void | Promise<void>
  isPending?: boolean
  initialPath?: string
}

export const RenameTorrentFileDialog = memo(function RenameTorrentFileDialog({
  open,
  onOpenChange,
  files = [],
  isLoading = false,
  onConfirm,
  isPending = false,
  initialPath,
}: RenameTorrentFileDialogProps) {
  const [newName, setNewName] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  // Parse the initial path into folder and filename
  const { folderPath, fileName } = useMemo(() => {
    if (!initialPath) return { folderPath: "", fileName: "" }
    const lastSlash = initialPath.lastIndexOf("/")
    if (lastSlash === -1) return { folderPath: "", fileName: initialPath }
    return {
      folderPath: initialPath.slice(0, lastSlash),
      fileName: initialPath.slice(lastSlash + 1),
    }
  }, [initialPath])

  // Check if file exists in the list
  const fileExists = useMemo(() => {
    return initialPath ? files.some(f => f.name === initialPath) : false
  }, [files, initialPath])

  // Initialize newName when dialog opens or path changes
  useEffect(() => {
    if (open && fileName) {
      setNewName(fileName)
      // Focus and select the filename (without extension) after a brief delay
      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus()
          const dotIndex = fileName.lastIndexOf(".")
          if (dotIndex > 0) {
            inputRef.current.setSelectionRange(0, dotIndex)
          } else {
            inputRef.current.select()
          }
        }
      }, 50)
    }
    if (!open) {
      setNewName("")
    }
  }, [open, fileName])

  const newPath = useMemo(() => {
    const trimmed = newName.trim()
    if (!trimmed) return ""
    return folderPath ? `${folderPath}/${trimmed}` : trimmed
  }, [folderPath, newName])

  const hasChanges = newName.trim() !== fileName

  const handleConfirm = useCallback(() => {
    if (!initialPath || !newName.trim()) return
    onConfirm({ oldPath: initialPath, newPath })
  }, [initialPath, newName, newPath, onConfirm])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setNewName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-md sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Rename File</DialogTitle>
        </DialogHeader>
        <div className="py-2 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              Loading...
            </div>
          ) : !initialPath || !fileExists ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              No file selected.
            </div>
          ) : (
            <>
              {/* Current path display */}
              {folderPath && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">Location</Label>
                  <div className="text-xs font-mono text-muted-foreground bg-muted/50 rounded px-2.5 py-1.5 break-all">
                    {folderPath}/
                  </div>
                </div>
              )}

              {/* New name input */}
              <div className="space-y-1.5">
                <Label htmlFor="fileName">File Name</Label>
                <Input
                  ref={inputRef}
                  id="fileName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder="Enter file name"
                  disabled={isPending}
                  className="font-mono"
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim() && hasChanges) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
              </div>

            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !initialPath || !newName.trim() || !hasChanges || !fileExists}
          >
            {isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            Rename
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentFolderDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  folders?: { name: string }[]
  isLoading?: boolean
  onConfirm: (payload: { oldPath: string; newPath: string }) => void | Promise<void>
  isPending?: boolean
  initialPath?: string
}

export const RenameTorrentFolderDialog = memo(function RenameTorrentFolderDialog({
  open,
  onOpenChange,
  folders = [],
  isLoading = false,
  onConfirm,
  isPending = false,
  initialPath,
}: RenameTorrentFolderDialogProps) {
  const [selectedPath, setSelectedPath] = useState("")
  const [newName, setNewName] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  const sortedFolders = useMemo(() => {
    return folders.slice().sort((a, b) => a.name.localeCompare(b.name))
  }, [folders])

  // Parse the selected path into parent and folder name
  const { parentPath, folderName } = useMemo(() => {
    const path = selectedPath || initialPath || ""
    if (!path) return { parentPath: "", folderName: "" }
    const lastSlash = path.lastIndexOf("/")
    if (lastSlash === -1) return { parentPath: "", folderName: path }
    return {
      parentPath: path.slice(0, lastSlash),
      folderName: path.slice(lastSlash + 1),
    }
  }, [selectedPath, initialPath])

  // Check if folder exists
  const folderExists = useMemo(() => {
    const path = selectedPath || initialPath
    return path ? folders.some(f => f.name === path) : false
  }, [folders, selectedPath, initialPath])

  // Initialize when dialog opens
  useEffect(() => {
    if (open) {
      const path = initialPath || sortedFolders[0]?.name || ""
      setSelectedPath(path)
      if (path) {
        const segments = path.split("/")
        const name = segments[segments.length - 1] || path
        setNewName(name)
        setTimeout(() => {
          if (inputRef.current) {
            inputRef.current.focus()
            inputRef.current.select()
          }
        }, 50)
      }
    }
    if (!open) {
      setSelectedPath("")
      setNewName("")
    }
  }, [open, initialPath, sortedFolders])

  const newPath = useMemo(() => {
    const trimmed = newName.trim()
    if (!trimmed) return ""
    return parentPath ? `${parentPath}/${trimmed}` : trimmed
  }, [parentPath, newName])

  const hasChanges = newName.trim() !== folderName

  const handleConfirm = useCallback(() => {
    const path = selectedPath || initialPath
    if (!path || !newName.trim()) return
    onConfirm({ oldPath: path, newPath })
  }, [selectedPath, initialPath, newName, newPath, onConfirm])

  const handleFolderSelect = useCallback((value: string) => {
    setSelectedPath(value)
    const segments = value.split("/")
    setNewName(segments[segments.length - 1] || value)
  }, [])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setSelectedPath("")
      setNewName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  // If we have an initialPath, show simplified UI. Otherwise show folder selector.
  const showFolderSelector = !initialPath && sortedFolders.length > 1

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-md sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Rename Folder</DialogTitle>
        </DialogHeader>
        <div className="py-2 space-y-4 overflow-hidden">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              Loading...
            </div>
          ) : sortedFolders.length === 0 ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              No folders available to rename.
            </div>
          ) : (
            <>
              {/* Folder selector - only if no initialPath and multiple folders */}
              {showFolderSelector && (
                <div className="space-y-1.5">
                  <Label htmlFor="folderSelect">Select Folder</Label>
                  <Select value={selectedPath} onValueChange={handleFolderSelect}>
                    <SelectTrigger id="folderSelect" className="font-mono text-xs">
                      <SelectValue placeholder="Choose a folder..." />
                    </SelectTrigger>
                    <SelectContent>
                      {sortedFolders.map((folder) => (
                        <SelectItem key={folder.name} value={folder.name} className="font-mono text-xs">
                          {folder.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {/* Parent path display */}
              {parentPath && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">Location</Label>
                  <div className="text-xs font-mono text-muted-foreground bg-muted/50 rounded px-2.5 py-1.5 break-all">
                    {parentPath}/
                  </div>
                </div>
              )}

              {/* New name input */}
              <div className="space-y-1.5">
                <Label htmlFor="folderName">Folder Name</Label>
                <Input
                  ref={inputRef}
                  id="folderName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder="Enter folder name"
                  disabled={isPending}
                  className="font-mono"
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim() && hasChanges) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
              </div>


              <p className="text-xs text-muted-foreground">
                All files inside this folder will be moved to the new location.
              </p>
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            Cancel
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !folderExists || !newName.trim() || !hasChanges}
          >
            {isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            Rename
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

export const SetCategoryDialog = memo(function SetCategoryDialog({
  open,
  onOpenChange,
  availableCategories,
  hashCount,
  onConfirm,
  isPending = false,
  initialCategory = "",
  isLoadingCategories = false,
  useSubcategories = false,
}: SetCategoryDialogProps) {
  const [categoryInput, setCategoryInput] = useState("")
  const [searchQuery, setSearchQuery] = useState("")
  const wasOpen = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // Initialize category only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setCategoryInput(initialCategory)
      setSearchQuery("")
    }
    wasOpen.current = open
  }, [open, initialCategory])

  const handleConfirm = useCallback(() => {
    onConfirm(categoryInput)
    setCategoryInput("")
    setSearchQuery("")
  }, [categoryInput, onConfirm])

  const handleCancel = useCallback(() => {
    setCategoryInput("")
    setSearchQuery("")
    onOpenChange(false)
  }, [onOpenChange])

  // Filter categories based on search, with subcategory support
  const categoryList = Object.keys(availableCategories || {}).sort()

  const filteredCategories = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()

    if (useSubcategories) {
      const tree = buildCategoryTree(availableCategories || {}, {})
      const shouldIncludeCache = new Map<CategoryNode, boolean>()

      const shouldIncludeNode = (node: CategoryNode): boolean => {
        const cached = shouldIncludeCache.get(node)
        if (cached !== undefined) {
          return cached
        }

        const nodeMatches = query === "" || node.name.toLowerCase().includes(query)
        if (nodeMatches) {
          shouldIncludeCache.set(node, true)
          return true
        }

        for (const child of node.children) {
          if (shouldIncludeNode(child)) {
            shouldIncludeCache.set(node, true)
            return true
          }
        }

        shouldIncludeCache.set(node, false)
        return false
      }

      const flattened: Array<{ name: string; displayName: string; level: number }> = []

      const visitNodes = (nodes: CategoryNode[]) => {
        for (const node of nodes) {
          if (shouldIncludeNode(node)) {
            flattened.push({
              name: node.name,
              displayName: node.displayName,
              level: node.level,
            })
            visitNodes(node.children)
          }
        }
      }

      visitNodes(tree)
      return flattened
    }

    const names = categoryList
    const namesFiltered = query ? names.filter(cat => cat.toLowerCase().includes(query)) : names

    return namesFiltered.map((name) => ({
      name,
      displayName: name,
      level: 0,
    }))
  }, [availableCategories, categoryList, searchQuery, useSubcategories])

  const shouldUseVirtualization = filteredCategories.length > 50

  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? filteredCategories.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 36,
    overscan: 5,
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Category for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Select a category from the list or create a new one
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {/* Search bar for categories */}
          {!isLoadingCategories && categoryList.length > 10 && (
            <div className="space-y-2">
              <Label htmlFor="categorySearch">Search Categories</Label>
              <Input
                id="categorySearch"
                placeholder="Type to search..."
                value={searchQuery}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setSearchQuery(e.target.value)}
              />
            </div>
          )}

          {/* Category list with optional virtualization */}
          <div className="space-y-2">
            <Label>Select Category</Label>
            {isLoadingCategories ? (
              <div className="max-h-64 border rounded-md p-3 flex items-center justify-center">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">Loading categories...</span>
                </div>
              </div>
            ) : (
              <div
                ref={scrollContainerRef}
                className="max-h-64 border rounded-md overflow-y-auto"
              >
                {/* No category option */}
                <button
                  type="button"
                  onClick={() => setCategoryInput("")}
                  className={`w-full text-left px-3 py-2 hover:bg-accent transition-colors ${
                    categoryInput === "" ? "bg-accent" : ""
                  }`}
                >
                  <span className="text-sm text-muted-foreground italic">(No category)</span>
                </button>

                {shouldUseVirtualization ? (
                // Virtualized rendering for large lists
                  <div
                    style={{
                      height: `${virtualizer.getTotalSize()}px`,
                      width: "100%",
                      position: "relative",
                    }}
                  >
                    {virtualizer.getVirtualItems().map((virtualRow) => {
                      const category = filteredCategories[virtualRow.index]
                      return (
                        <div
                          key={virtualRow.key}
                          data-index={virtualRow.index}
                          ref={virtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        >
                          <button
                            type="button"
                            onClick={() => setCategoryInput(category.name)}
                            className={`w-full text-left px-3 py-2 hover:bg-accent transition-colors ${
                              categoryInput === category.name ? "bg-accent" : ""
                            }`}
                            title={category.name}
                          >
                            <span
                              className="text-sm"
                              style={category.level > 0 ? { paddingLeft: category.level * 12 } : undefined}
                            >
                              {category.displayName}
                            </span>
                          </button>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                // Simple rendering for small lists - much faster!
                  <div>
                    {filteredCategories.map((category) => (
                      <button
                        key={category.name}
                        type="button"
                        onClick={() => setCategoryInput(category.name)}
                        className={`w-full text-left px-3 py-2 hover:bg-accent transition-colors ${
                          categoryInput === category.name ? "bg-accent" : ""
                        }`}
                        title={category.name}
                      >
                        <span
                          className="text-sm"
                          style={category.level > 0 ? { paddingLeft: category.level * 12 } : undefined}
                        >
                          {category.displayName}
                        </span>
                      </button>
                    ))}
                  </div>
                )}

                {filteredCategories.length === 0 && searchQuery && (
                  <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                    No categories found matching "{searchQuery}"
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Option to enter new category */}
          <div className="space-y-2">
            <Label htmlFor="newCategory">Or create new category</Label>
            <Input
              id="newCategory"
              placeholder="Enter new category name"
              value={categoryInput && !categoryList.includes(categoryInput) ? categoryInput : ""}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setCategoryInput(e.target.value)}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                if (e.key === "Enter") {
                  handleConfirm()
                }
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>Cancel</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending}
          >
            Set Category
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface CreateAndAssignCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (category: string) => void
  isPending?: boolean
}

export const CreateAndAssignCategoryDialog = memo(function CreateAndAssignCategoryDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
}: CreateAndAssignCategoryDialogProps) {
  const [categoryName, setCategoryName] = useState("")
  const wasOpen = useRef(false)

  // Reset when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      setCategoryName("")
    }
    wasOpen.current = open
  }, [open])

  const handleConfirm = useCallback(() => {
    if (categoryName.trim()) {
      onConfirm(categoryName.trim())
      setCategoryName("")
    }
  }, [categoryName, onConfirm])

  const handleCancel = useCallback(() => {
    setCategoryName("")
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create New Category</DialogTitle>
          <DialogDescription>
            Enter a name for the new category. It will be created and assigned to {hashCount} torrent(s).
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="categoryName">Category Name</Label>
          <Input
            id="categoryName"
            placeholder="Enter category name"
            value={categoryName}
            onChange={(e: ChangeEvent<HTMLInputElement>) => setCategoryName(e.target.value)}
            onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
              if (e.key === "Enter" && categoryName.trim()) {
                handleConfirm()
              }
            }}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>Cancel</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !categoryName.trim()}
          >
            Create and Assign
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RemoveTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  currentTags?: string[]
}

export const RemoveTagsDialog = memo(function RemoveTagsDialog({
  open,
  onOpenChange,
  availableTags,
  hashCount,
  onConfirm,
  isPending = false,
  currentTags = [],
}: RemoveTagsDialogProps) {
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const wasOpen = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)

  // Initialize with current tags when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      // Reset selection when dialog opens
      setSelectedTags([])
    }
    wasOpen.current = open
  }, [open, currentTags, availableTags])

  const handleConfirm = useCallback(() => {
    if (selectedTags.length > 0) {
      onConfirm(selectedTags)
      setSelectedTags([])
    }
  }, [selectedTags, onConfirm])

  const handleCancel = useCallback(() => {
    setSelectedTags([])
    onOpenChange(false)
  }, [onOpenChange])

  // Filter available tags to only show those that are on the selected torrents
  const relevantTags = (availableTags || []).filter(tag => currentTags.includes(tag))

  // Only use virtualization for large tag lists (>50 tags)
  const shouldUseVirtualization = relevantTags.length > 50

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? relevantTags.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 32, // Approximate height of each tag item
    overscan: 5,
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle>Remove Tags from {hashCount} torrent(s)</AlertDialogTitle>
          <AlertDialogDescription>
            Select which tags to remove from the selected torrents.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-4">
          {relevantTags.length > 0 ? (
            <div className="space-y-2">
              <Label>Tags to Remove</Label>
              <div
                ref={scrollContainerRef}
                className="h-48 border rounded-md p-3 overflow-y-auto"
              >
                {shouldUseVirtualization ? (
                  // Virtualized rendering for large tag lists (>50 tags)
                  <div
                    style={{
                      height: `${virtualizer.getTotalSize()}px`,
                      width: "100%",
                      position: "relative",
                    }}
                  >
                    {virtualizer.getVirtualItems().map((virtualRow) => {
                      const tag = relevantTags[virtualRow.index]
                      return (
                        <div
                          key={virtualRow.key}
                          data-index={virtualRow.index}
                          ref={virtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        >
                          <div className="flex items-center space-x-2 py-1">
                            <Checkbox
                              id={`remove-tag-${tag}`}
                              checked={selectedTags.includes(tag)}
                              onCheckedChange={(checked) => {
                                if (checked) {
                                  setSelectedTags([...selectedTags, tag])
                                } else {
                                  setSelectedTags(selectedTags.filter(t => t !== tag))
                                }
                              }}
                            />
                            <label
                              htmlFor={`remove-tag-${tag}`}
                              className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer"
                            >
                              {tag}
                            </label>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  // Simple rendering for small tag lists (≤50 tags)
                  <div className="space-y-1">
                    {relevantTags.map((tag) => (
                      <div key={tag} className="flex items-center space-x-2 py-1">
                        <Checkbox
                          id={`remove-tag-${tag}`}
                          checked={selectedTags.includes(tag)}
                          onCheckedChange={(checked) => {
                            if (checked) {
                              setSelectedTags([...selectedTags, tag])
                            } else {
                              setSelectedTags(selectedTags.filter(t => t !== tag))
                            }
                          }}
                        />
                        <label
                          htmlFor={`remove-tag-${tag}`}
                          className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 cursor-pointer"
                        >
                          {tag}
                        </label>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              No tags found on the selected torrents.
            </div>
          )}

          {/* Selected tags summary */}
          {selectedTags.length > 0 && (
            <div className="text-sm text-muted-foreground">
              Will remove: {selectedTags.join(", ")}
            </div>
          )}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={handleCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleConfirm}
            disabled={selectedTags.length === 0 || isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            <X className="mr-2 h-4 w-4" />
            Remove Tags
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
})

interface EditTrackerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  tracker: string
  trackerURLs?: string[]
  loadingURLs?: boolean
  selectedHashes: string[]
  onConfirm: (oldURL: string, newURL: string) => void
  isPending?: boolean
  onConvertHttpToHttps?: () => void
  isConverting?: boolean
}

export const EditTrackerDialog = memo(function EditTrackerDialog({
  open,
  onOpenChange,
  instanceId: _instanceId, // eslint-disable-line @typescript-eslint/no-unused-vars
  tracker,
  trackerURLs = [],
  loadingURLs = false,
  selectedHashes,
  onConfirm,
  isPending = false,
  onConvertHttpToHttps,
  isConverting = false,
}: EditTrackerDialogProps) {
  const [oldURL, setOldURL] = useState("")
  const [newURL, setNewURL] = useState("")
  const wasOpen = useRef(false)

  // Initialize URLs when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      // Set the first tracker URL if available, otherwise clear
      if (trackerURLs && trackerURLs.length > 0) {
        setOldURL(trackerURLs[0])
      } else {
        setOldURL("")
      }
      setNewURL("")
    }
    wasOpen.current = open
  }, [open, tracker, trackerURLs])

  // Update oldURL selection when trackerURLs refresh (e.g., after HTTP→HTTPS conversion)
  // If the selected URL was converted, try to select its https equivalent or first available
  useEffect(() => {
    if (!open || !oldURL) return
    // If current selection still exists, keep it
    if (trackerURLs.includes(oldURL)) return
    // If it was an http:// URL, try to find its https:// equivalent by matching hostname/pathname
    if (oldURL.startsWith("http://")) {
      try {
        const parsed = new URL(oldURL)
        // Find an HTTPS URL with matching hostname and pathname (port may differ)
        const httpsMatch = trackerURLs.find((url) => {
          if (!url.startsWith("https://")) return false
          try {
            const candidate = new URL(url)
            return (
              candidate.hostname.toLowerCase() === parsed.hostname.toLowerCase() &&
              candidate.pathname === parsed.pathname &&
              candidate.search === parsed.search
            )
          } catch {
            return false
          }
        })
        if (httpsMatch) {
          setOldURL(httpsMatch)
          return
        }
      } catch {
        // Parsing failed, fall through to fallback
      }
    }
    // Fall back to first available URL
    if (trackerURLs.length > 0) {
      setOldURL(trackerURLs[0])
    }
  }, [open, oldURL, trackerURLs])

  const handleConfirm = useCallback((): void => {
    if (oldURL.trim() && newURL.trim()) {
      onConfirm(oldURL.trim(), newURL.trim())
      setOldURL("")
      setNewURL("")
    }
  }, [oldURL, newURL, onConfirm])

  const handleCancel = useCallback((): void => {
    setOldURL("")
    setNewURL("")
    onOpenChange(false)
  }, [onOpenChange])

  const hashCount = selectedHashes.length
  const isFilteredMode = hashCount === 0 // When no hashes provided, we're updating all torrents with this tracker

  // Check if there are any HTTP URLs that could be converted to HTTPS
  const hasHttpUrls = useMemo(
    () => trackerURLs.some((url) => url.startsWith("http://")),
    [trackerURLs]
  )

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>Edit Tracker URL - {tracker}</AlertDialogTitle>
          <AlertDialogDescription>
            Update the tracker URL for all torrents using <strong className="font-mono">{tracker}</strong>.
            This is useful for updating passkeys or changing tracker addresses.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="oldURL">Current Full Tracker URL</Label>
            {loadingURLs ? (
              <div className="flex items-center justify-center py-3 text-sm text-muted-foreground">
                <span className="animate-pulse">Loading tracker URLs...</span>
              </div>
            ) : trackerURLs && trackerURLs.length > 1 ? (
              <div className="space-y-2">
                <select
                  className="w-full px-3 py-2 text-sm font-mono border rounded-md bg-background"
                  value={oldURL}
                  onChange={(e) => setOldURL(e.target.value)}
                >
                  <option value="">Select a tracker URL</option>
                  {trackerURLs.map((url) => (
                    <option key={url} value={url}>
                      {url}
                    </option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground">
                  Multiple tracker URLs found. Select the one you want to update.
                </p>
              </div>
            ) : (
              <>
                <Input
                  id="oldURL"
                  value={oldURL}
                  onChange={(e) => setOldURL(e.target.value)}
                  placeholder={trackerURLs.length === 0 ? `e.g., http://${tracker}:6969/announce` : ""}
                  className="font-mono text-sm"
                />
                {trackerURLs.length === 0 && (
                  <p className="text-xs text-muted-foreground">
                    Enter the complete tracker URL including the announce path
                  </p>
                )}
                {trackerURLs.length === 1 && (
                  <p className="text-xs text-muted-foreground">
                    Pre-populated from detected URL. Edit if needed (e.g., different scheme).
                  </p>
                )}
              </>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="newURL">New Full Tracker URL</Label>
            <Input
              id="newURL"
              value={newURL}
              onChange={(e) => setNewURL(e.target.value)}
              placeholder={`e.g., http://${tracker}:6969/announce?passkey=new_key`}
              className="font-mono text-sm"
            />
            <p className="text-xs text-muted-foreground">
              Enter the new complete URL (typically with updated passkey)
            </p>
          </div>
          {isFilteredMode && (
            <div className="bg-muted p-3 rounded-md">
              <p className="text-sm text-muted-foreground">
                <strong>Note:</strong> This will update all torrents that have the exact matching tracker URL.
              </p>
            </div>
          )}
          {hasHttpUrls && onConvertHttpToHttps && (
            <div className="pt-2 border-t">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onConvertHttpToHttps}
                disabled={isConverting || loadingURLs || isPending}
                className="w-full"
              >
                {isConverting ? "Converting..." : "Convert all HTTP to HTTPS"}
              </Button>
              <p className="text-xs text-muted-foreground mt-1">
                Upgrades all http:// tracker URLs to https:// for this domain
              </p>
            </div>
          )}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={handleCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleConfirm}
            disabled={!oldURL.trim() || !newURL.trim() || oldURL === newURL || isPending || loadingURLs || isConverting}
          >
            Update Tracker
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
})

const LIMIT_USE_GLOBAL = -2
const LIMIT_UNLIMITED = -1
const SPEED_DEFAULT_LIMIT = 0

// Helper function to safely get numeric values with fallback
const safeNumber = (value: number | undefined, fallback: number) =>
  typeof value === "number" ? value : fallback

// Single type for torrent limit fields used in dialogs
type TorrentLimitSnapshot = Pick<
  Torrent,
  | "ratio_limit"
  | "seeding_time_limit"
  | "inactive_seeding_time_limit"
  | "max_ratio"
  | "max_seeding_time"
  | "max_inactive_seeding_time"
  | "dl_limit"
  | "up_limit"
>

interface ShareLimitDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  torrents?: TorrentLimitSnapshot[]
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => void
  isPending?: boolean
}

// Share limit mode: matches qBittorrent sentinel values
type ShareLimitMode = "global" | "unlimited" | "custom"

interface ShareLimitFieldState {
  mode: ShareLimitMode
  customValue: number
  isMixed: boolean // True when selection has different values
}

// Convert a raw limit value to mode + custom value
function valueToFieldState(value: number | undefined, defaultCustom: number): Omit<ShareLimitFieldState, "isMixed"> {
  if (value === undefined || value === LIMIT_USE_GLOBAL) {
    return { mode: "global", customValue: defaultCustom }
  }
  if (value === LIMIT_UNLIMITED) {
    return { mode: "unlimited", customValue: defaultCustom }
  }
  return { mode: "custom", customValue: value }
}

// Check if all torrents have the same value for a field
function checkFieldConsistency(
  torrents: TorrentLimitSnapshot[] | undefined,
  getter: (t: TorrentLimitSnapshot) => number | undefined
): { isMixed: boolean; commonValue: number | undefined } {
  if (!torrents || torrents.length === 0) {
    return { isMixed: false, commonValue: undefined }
  }
  const firstValue = getter(torrents[0])
  const allSame = torrents.every(t => getter(t) === firstValue)
  return { isMixed: !allSame, commonValue: allSame ? firstValue : undefined }
}

// Build initial state from selected torrents
function buildShareLimitFieldStates(torrents?: TorrentLimitSnapshot[]): {
  ratio: ShareLimitFieldState
  seedTime: ShareLimitFieldState
  inactiveTime: ShareLimitFieldState
} {
  const ratioCheck = checkFieldConsistency(torrents, t => t.ratio_limit)
  const seedTimeCheck = checkFieldConsistency(torrents, t => t.seeding_time_limit)
  const inactiveTimeCheck = checkFieldConsistency(torrents, t => t.inactive_seeding_time_limit)

  return {
    ratio: {
      ...valueToFieldState(ratioCheck.commonValue, 1.0),
      isMixed: ratioCheck.isMixed,
    },
    seedTime: {
      ...valueToFieldState(seedTimeCheck.commonValue, 1440),
      isMixed: seedTimeCheck.isMixed,
    },
    inactiveTime: {
      ...valueToFieldState(inactiveTimeCheck.commonValue, 10080),
      isMixed: inactiveTimeCheck.isMixed,
    },
  }
}

// Convert mode + custom value to API value
function fieldStateToValue(mode: ShareLimitMode, customValue: number, isRatio: boolean): number {
  switch (mode) {
    case "global":
      return LIMIT_USE_GLOBAL
    case "unlimited":
      return LIMIT_UNLIMITED
    case "custom":
      // Normalize ratio to 2 decimal places
      return isRatio ? Math.round(customValue * 100) / 100 : customValue
  }
}

export const ShareLimitDialog = memo(function ShareLimitDialog({
  open,
  onOpenChange,
  hashCount,
  torrents,
  onConfirm,
  isPending = false,
}: ShareLimitDialogProps) {
  const [ratioMode, setRatioMode] = useState<ShareLimitMode>("global")
  const [ratioCustom, setRatioCustom] = useState(1.0)
  const [ratioMixed, setRatioMixed] = useState(false)
  const [ratioTouched, setRatioTouched] = useState(false) // User explicitly changed this field

  const [seedTimeMode, setSeedTimeMode] = useState<ShareLimitMode>("global")
  const [seedTimeCustom, setSeedTimeCustom] = useState(1440)
  const [seedTimeMixed, setSeedTimeMixed] = useState(false)
  const [seedTimeTouched, setSeedTimeTouched] = useState(false)

  const [inactiveTimeMode, setInactiveTimeMode] = useState<ShareLimitMode>("global")
  const [inactiveTimeCustom, setInactiveTimeCustom] = useState(10080)
  const [inactiveTimeMixed, setInactiveTimeMixed] = useState(false)
  const [inactiveTimeTouched, setInactiveTimeTouched] = useState(false)

  const wasOpen = useRef(false)

  // Reset form when dialog opens with torrent values
  useEffect(() => {
    if (open && !wasOpen.current) {
      const states = buildShareLimitFieldStates(torrents)

      setRatioMode(states.ratio.isMixed ? "global" : states.ratio.mode)
      setRatioCustom(states.ratio.customValue)
      setRatioMixed(states.ratio.isMixed)
      setRatioTouched(false)

      setSeedTimeMode(states.seedTime.isMixed ? "global" : states.seedTime.mode)
      setSeedTimeCustom(states.seedTime.customValue)
      setSeedTimeMixed(states.seedTime.isMixed)
      setSeedTimeTouched(false)

      setInactiveTimeMode(states.inactiveTime.isMixed ? "global" : states.inactiveTime.mode)
      setInactiveTimeCustom(states.inactiveTime.customValue)
      setInactiveTimeMixed(states.inactiveTime.isMixed)
      setInactiveTimeTouched(false)
    }
    wasOpen.current = open
  }, [open, torrents])

  // Check if any mixed field hasn't been explicitly addressed by the user
  const hasUnresolvedMixed = (ratioMixed && !ratioTouched) ||
    (seedTimeMixed && !seedTimeTouched) ||
    (inactiveTimeMixed && !inactiveTimeTouched)

  const handleConfirm = useCallback((): void => {
    onConfirm(
      fieldStateToValue(ratioMode, ratioCustom, true),
      fieldStateToValue(seedTimeMode, seedTimeCustom, false),
      fieldStateToValue(inactiveTimeMode, inactiveTimeCustom, false)
    )
    // Reset form
    setRatioMode("global")
    setRatioCustom(1.0)
    setRatioMixed(false)
    setRatioTouched(false)
    setSeedTimeMode("global")
    setSeedTimeCustom(1440)
    setSeedTimeMixed(false)
    setSeedTimeTouched(false)
    setInactiveTimeMode("global")
    setInactiveTimeCustom(10080)
    setInactiveTimeMixed(false)
    setInactiveTimeTouched(false)
    onOpenChange(false)
  }, [onConfirm, ratioMode, ratioCustom, seedTimeMode, seedTimeCustom, inactiveTimeMode, inactiveTimeCustom, onOpenChange])

  const handleCancel = useCallback((): void => {
    setRatioMode("global")
    setRatioCustom(1.0)
    setRatioMixed(false)
    setRatioTouched(false)
    setSeedTimeMode("global")
    setSeedTimeCustom(1440)
    setSeedTimeMixed(false)
    setSeedTimeTouched(false)
    setInactiveTimeMode("global")
    setInactiveTimeCustom(10080)
    setInactiveTimeMixed(false)
    setInactiveTimeTouched(false)
    onOpenChange(false)
  }, [onOpenChange])

  // Helper to set all fields to global (shortcut) - marks all as touched
  const setAllGlobal = useCallback(() => {
    setRatioMode("global")
    setRatioTouched(true)
    setSeedTimeMode("global")
    setSeedTimeTouched(true)
    setInactiveTimeMode("global")
    setInactiveTimeTouched(true)
  }, [])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Share Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Configure seeding limits for selected torrents. All three fields will be applied.
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          {/* Quick action: Set all to global */}
          <div className="flex justify-end">
            <Button variant="outline" size="sm" onClick={setAllGlobal}>
              Set all to Global
            </Button>
          </div>

          {/* Ratio limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">Ratio limit</Label>
              {ratioMixed && !ratioTouched && (
                <span className="text-xs text-yellow-600">Select a value</span>
              )}
              {ratioMixed && ratioTouched && (
                <span className="text-xs text-muted-foreground">(was mixed)</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={ratioMode}
                onValueChange={(value: ShareLimitMode) => {
                  setRatioMode(value)
                  setRatioTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">Use global</SelectItem>
                  <SelectItem value="unlimited">Unlimited</SelectItem>
                  <SelectItem value="custom">Custom</SelectItem>
                </SelectContent>
              </Select>
              {ratioMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  step="0.01"
                  className="flex-1"
                  value={ratioCustom}
                  onChange={(e) => {
                    const val = parseFloat(e.target.value)
                    if (Number.isFinite(val)) setRatioCustom(val)
                  }}
                  placeholder="e.g. 2.0"
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {ratioMode === "global" ? "Follow qBittorrent global settings" :ratioMode === "unlimited" ? "No ratio limit" :"Stop seeding when ratio reaches this value"}
            </p>
          </div>

          {/* Seeding time limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">Seeding time limit</Label>
              {seedTimeMixed && !seedTimeTouched && (
                <span className="text-xs text-yellow-600">Select a value</span>
              )}
              {seedTimeMixed && seedTimeTouched && (
                <span className="text-xs text-muted-foreground">(was mixed)</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={seedTimeMode}
                onValueChange={(value: ShareLimitMode) => {
                  setSeedTimeMode(value)
                  setSeedTimeTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">Use global</SelectItem>
                  <SelectItem value="unlimited">Unlimited</SelectItem>
                  <SelectItem value="custom">Custom</SelectItem>
                </SelectContent>
              </Select>
              {seedTimeMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  className="flex-1"
                  value={seedTimeCustom}
                  onChange={(e) => {
                    const val = parseInt(e.target.value, 10)
                    if (Number.isFinite(val)) setSeedTimeCustom(val)
                  }}
                  placeholder="e.g. 1440"
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {seedTimeMode === "global" ? "Follow qBittorrent global settings" :seedTimeMode === "unlimited" ? "No time limit" :"Minutes (1440 = 24 hours)"}
            </p>
          </div>

          {/* Inactive seeding time limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">Inactive seeding limit</Label>
              {inactiveTimeMixed && !inactiveTimeTouched && (
                <span className="text-xs text-yellow-600">Select a value</span>
              )}
              {inactiveTimeMixed && inactiveTimeTouched && (
                <span className="text-xs text-muted-foreground">(was mixed)</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={inactiveTimeMode}
                onValueChange={(value: ShareLimitMode) => {
                  setInactiveTimeMode(value)
                  setInactiveTimeTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">Use global</SelectItem>
                  <SelectItem value="unlimited">Unlimited</SelectItem>
                  <SelectItem value="custom">Custom</SelectItem>
                </SelectContent>
              </Select>
              {inactiveTimeMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  className="flex-1"
                  value={inactiveTimeCustom}
                  onChange={(e) => {
                    const val = parseInt(e.target.value, 10)
                    if (Number.isFinite(val)) setInactiveTimeCustom(val)
                  }}
                  placeholder="e.g. 10080"
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {inactiveTimeMode === "global" ? "Follow qBittorrent global settings" :inactiveTimeMode === "unlimited" ? "No inactive limit" :"Minutes (10080 = 7 days)"}
            </p>
          </div>
        </div>
        <DialogFooter className="flex-col sm:flex-row gap-2">
          {hasUnresolvedMixed && (
            <p className="text-xs text-yellow-600 text-left sm:flex-1">
              Select values for all mixed fields before applying
            </p>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={handleCancel}>
              Cancel
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={isPending || hasUnresolvedMixed}
            >
              {isPending ? "Setting..." : "Apply Limits"}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface SpeedLimitsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  torrents?: TorrentLimitSnapshot[]
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending?: boolean
}

interface SpeedLimitFormState {
  uploadEnabled: boolean
  uploadLimit: number
  downloadEnabled: boolean
  downloadLimit: number
}

const buildSpeedLimitInitialState = (torrents?: TorrentLimitSnapshot[]): SpeedLimitFormState => {
  const base: SpeedLimitFormState = {
    uploadEnabled: false,
    uploadLimit: SPEED_DEFAULT_LIMIT,
    downloadEnabled: false,
    downloadLimit: SPEED_DEFAULT_LIMIT,
  }

  if (!torrents || torrents.length === 0) {
    return base
  }

  const uploadValues = torrents.map((torrent) => safeNumber(torrent.up_limit, 0))
  const downloadValues = torrents.map((torrent) => safeNumber(torrent.dl_limit, 0))

  const uploadsMatch = uploadValues.every((value) => value === uploadValues[0])
  const downloadsMatch = downloadValues.every((value) => value === downloadValues[0])

  const firstUpload = uploadValues[0]
  const firstDownload = downloadValues[0]

  return {
    ...base,
    uploadEnabled: uploadsMatch && firstUpload > 0,
    uploadLimit: uploadsMatch && firstUpload > 0 ? Math.round(firstUpload / 1024) : base.uploadLimit,
    downloadEnabled: downloadsMatch && firstDownload > 0,
    downloadLimit: downloadsMatch && firstDownload > 0 ? Math.round(firstDownload / 1024) : base.downloadLimit,
  }
}

export const SpeedLimitsDialog = memo(function SpeedLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  torrents,
  onConfirm,
  isPending = false,
}: SpeedLimitsDialogProps) {
  const [uploadEnabled, setUploadEnabled] = useState(false)
  const [uploadLimit, setUploadLimit] = useState(SPEED_DEFAULT_LIMIT)
  const [downloadEnabled, setDownloadEnabled] = useState(false)
  const [downloadLimit, setDownloadLimit] = useState(SPEED_DEFAULT_LIMIT)
  const wasOpen = useRef(false)

  const speedInitialState = useMemo(() => buildSpeedLimitInitialState(torrents), [torrents])

  // Reset form when dialog opens with torrent values
  useEffect(() => {
    if (open && !wasOpen.current) {
      setUploadEnabled(speedInitialState.uploadEnabled)
      setUploadLimit(speedInitialState.uploadLimit)
      setDownloadEnabled(speedInitialState.downloadEnabled)
      setDownloadLimit(speedInitialState.downloadLimit)
    }
    wasOpen.current = open
  }, [open, speedInitialState])

  const handleConfirm = useCallback((): void => {
    onConfirm(
      uploadEnabled ? uploadLimit : 0,  // 0 means use global limit
      downloadEnabled ? downloadLimit : 0  // 0 means use global limit
    )
    // Reset form
    setUploadEnabled(false)
    setUploadLimit(SPEED_DEFAULT_LIMIT)
    setDownloadEnabled(false)
    setDownloadLimit(SPEED_DEFAULT_LIMIT)
  }, [onConfirm, uploadEnabled, uploadLimit, downloadEnabled, downloadLimit])

  const handleCancel = useCallback((): void => {
    setUploadEnabled(false)
    setUploadLimit(SPEED_DEFAULT_LIMIT)
    setDownloadEnabled(false)
    setDownloadLimit(SPEED_DEFAULT_LIMIT)
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Speed Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Set upload and download speed limits in KB/s. Disable to use global limits.
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="uploadEnabled"
                checked={uploadEnabled}
                onCheckedChange={setUploadEnabled}
              />
              <Label htmlFor="uploadEnabled">Set upload limit (KB/s)</Label>
            </div>
            <Input
              type="number"
              min="0"
              value={uploadLimit}
              disabled={!uploadEnabled}
              onChange={(e) => setUploadLimit(parseInt(e.target.value) || 0)}
              placeholder="0"
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="downloadEnabled"
                checked={downloadEnabled}
                onCheckedChange={setDownloadEnabled}
              />
              <Label htmlFor="downloadEnabled">Set download limit (KB/s)</Label>
            </div>
            <Input
              type="number"
              min="0"
              value={downloadLimit}
              disabled={!downloadEnabled}
              onChange={(e) => setDownloadLimit(parseInt(e.target.value) || 0)}
              placeholder="0"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>
            Cancel
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending}
          >
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface TmmConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  enable: boolean
  onConfirm: () => void
  isPending?: boolean
}

export const TmmConfirmDialog = memo(function TmmConfirmDialog({
  open,
  onOpenChange,
  count,
  enable,
  onConfirm,
  isPending = false,
}: TmmConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-warning" />
            {enable ? "Enable" : "Disable"} TMM for {count} torrent(s)?
          </DialogTitle>
          <DialogDescription>
            Automatic Torrent Management will move files based on category settings. This may affect cross-seeded torrents sharing the same data.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={onConfirm} disabled={isPending}>
            {enable ? "Enable" : "Disable"} TMM
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface LocationWarningDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  onConfirm: () => void
  isPending?: boolean
}

export const LocationWarningDialog = memo(function LocationWarningDialog({
  open,
  onOpenChange,
  count,
  onConfirm,
  isPending = false,
}: LocationWarningDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-warning" />
            Set Location for {count} torrent(s)?
          </DialogTitle>
          <DialogDescription>
            Changing the save location will move files on disk. This may affect cross-seeded torrents sharing the same data.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={onConfirm} disabled={isPending}>
            Continue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})
