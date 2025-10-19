/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
import { Tree } from "@/components/ui/file-tree"
import { pathsToTreeView } from "@/components/ui/file-tree-utils"
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
import type { Torrent } from "@/types"
import { Loader2, Plus, X } from "lucide-react"
import type { ChangeEvent, KeyboardEvent } from "react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useVirtualizer } from "@tanstack/react-virtual"

interface SetTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  initialTags?: string[]
}

interface AddTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  initialTags?: string[]
}

export const AddTagsDialog = memo(function AddTagsDialog({
  open,
  onOpenChange,
  availableTags,
  hashCount,
  onConfirm,
  isPending = false,
  initialTags = [],
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

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: displayTags.length,
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
          {displayTags && displayTags.length > 0 && (
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
              </div>
            </div>
          )}

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

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: displayTags.length,
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
          {displayTags && displayTags.length > 0 && (
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
              </div>
            </div>
          )}

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
  availableCategories: Record<string, unknown>
  hashCount: number
  onConfirm: (category: string) => void
  isPending?: boolean
  initialCategory?: string
}

interface SetLocationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (location: string) => void
  isPending?: boolean
  initialLocation?: string
}

export const SetLocationDialog = memo(function SetLocationDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
  initialLocation = "",
}: SetLocationDialogProps) {
  const [location, setLocation] = useState("")
  const wasOpen = useRef(false)
  const inputRef = useRef<HTMLInputElement>(null)

  // Initialize location only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setLocation(initialLocation)
      // Focus the input when dialog opens
      setTimeout(() => inputRef.current?.focus(), 0)
    }
    wasOpen.current = open
  }, [open, initialLocation])

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
    if (e.key === "Enter" && !isPending && location.trim()) {
      e.preventDefault()
      handleConfirm()
    }
  }, [isPending, location, handleConfirm])

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
              ref={inputRef}
              id="location"
              type="text"
              value={location}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setLocation(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="/path/to/save/location"
              disabled={isPending}
            />
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
  onConfirm: (payload: { oldPath: string; newName: string }) => void | Promise<void>
  isPending?: boolean
}

export const RenameTorrentFileDialog = memo(function RenameTorrentFileDialog({
  open,
  onOpenChange,
  files = [],
  isLoading = false,
  onConfirm,
  isPending = false,
}: RenameTorrentFileDialogProps) {
  const [selectedPath, setSelectedPath] = useState("")
  const [newName, setNewName] = useState("")
  const wasOpen = useRef(false)

  const sortedFiles = useMemo(() => {
    return files.slice().sort((a, b) => a.name.localeCompare(b.name))
  }, [files])

  const fileTreeElements = useMemo(() => {
    const filePaths = sortedFiles.map(file => file.name)
    if (filePaths.length === 0) {
      return []
    }
    return pathsToTreeView(filePaths, {
      selectablePaths: new Set(filePaths),
    })
  }, [sortedFiles])

  useEffect(() => {
    if (open && !wasOpen.current) {
      const defaultPath = sortedFiles[0]?.name ?? ""
      setSelectedPath(defaultPath)
      if (defaultPath) {
        const segments = defaultPath.split("/")
        setNewName(segments[segments.length - 1] || defaultPath)
      } else {
        setNewName("")
      }
    }
    if (!open) {
      setSelectedPath("")
      setNewName("")
    }
    wasOpen.current = open
  }, [open, sortedFiles])

  const handleConfirm = useCallback(() => {
    if (!selectedPath) return
    const trimmedName = newName.trim()
    if (!trimmedName) return
    onConfirm({ oldPath: selectedPath, newName: trimmedName })
  }, [newName, onConfirm, selectedPath])

  const handlePathChange = useCallback((value: string) => {
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

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-xl sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Rename File</DialogTitle>
          <DialogDescription>
            Choose a file from the torrent and provide its new name. Folder structure is preserved.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              Fetching file list...
            </div>
          ) : sortedFiles.length === 0 ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              No files available for this torrent yet.
            </div>
          ) : (
            <>
              <div className="space-y-2">
                <Label>Select File</Label>
                <div className="rounded-md border p-2">
                  <Tree
                    className="h-64"
                    elements={fileTreeElements}
                    initialSelectedId={selectedPath || sortedFiles[0]?.name}
                    onSelectionChange={handlePathChange}
                  />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="fileName">New File Name</Label>
                <Input
                  id="fileName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder="Enter new file name"
                  disabled={isPending}
                  className="font-mono"
                  title={newName}
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim()) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
                <p className="text-xs text-muted-foreground">
                  Only the file name is changed. Folder paths remain the same.
                </p>
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
            disabled={isPending || !selectedPath || !newName.trim() || sortedFiles.length === 0}
          >
            Rename File
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
  onConfirm: (payload: { oldPath: string; newName: string }) => void | Promise<void>
  isPending?: boolean
}

export const RenameTorrentFolderDialog = memo(function RenameTorrentFolderDialog({
  open,
  onOpenChange,
  folders = [],
  isLoading = false,
  onConfirm,
  isPending = false,
}: RenameTorrentFolderDialogProps) {
  const [selectedPath, setSelectedPath] = useState("")
  const [newName, setNewName] = useState("")
  const wasOpen = useRef(false)

  const sortedFolders = useMemo(() => {
    return folders.slice().sort((a, b) => a.name.localeCompare(b.name))
  }, [folders])

  const folderTreeElements = useMemo(() => {
    const folderPaths = sortedFolders.map(folder => folder.name)
    if (folderPaths.length === 0) {
      return []
    }
    return pathsToTreeView(folderPaths, {
      selectablePaths: new Set(folderPaths),
      leafType: "folder",
    })
  }, [sortedFolders])

  useEffect(() => {
    if (open && !wasOpen.current) {
      const defaultPath = sortedFolders[0]?.name ?? ""
      setSelectedPath(defaultPath)
      if (defaultPath) {
        const segments = defaultPath.split("/")
        setNewName(segments[segments.length - 1] || defaultPath)
      } else {
        setNewName("")
      }
    }
    if (!open) {
      setSelectedPath("")
      setNewName("")
    }
    wasOpen.current = open
  }, [open, sortedFolders])

  const handleConfirm = useCallback(() => {
    if (!selectedPath) return
    const trimmedName = newName.trim()
    if (!trimmedName) return
    onConfirm({ oldPath: selectedPath, newName: trimmedName })
  }, [newName, onConfirm, selectedPath])

  const handlePathChange = useCallback((value: string) => {
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

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-xl sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Rename Folder</DialogTitle>
          <DialogDescription>
            Choose a folder within the torrent and provide its new name. Any nested structure will be kept intact.
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              Fetching folder list...
            </div>
          ) : sortedFolders.length === 0 ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              No folders available to rename.
            </div>
          ) : (
            <>
              <div className="space-y-2">
                <Label>Select Folder</Label>
                <div className="rounded-md border p-2">
                  <Tree
                    className="h-64"
                    elements={folderTreeElements}
                    initialSelectedId={selectedPath || sortedFolders[0]?.name}
                    onSelectionChange={handlePathChange}
                  />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="folderName">New Folder Name</Label>
                <Input
                  id="folderName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder="Enter new folder name"
                  disabled={isPending}
                  className="font-mono"
                  title={newName}
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim()) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
                <p className="text-xs text-muted-foreground">
                  Only the selected folder name is changed. Parent directories remain the same.
                </p>
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
            disabled={isPending || !selectedPath || !newName.trim() || sortedFolders.length === 0}
          >
            Rename Folder
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
}: SetCategoryDialogProps) {
  const [categoryInput, setCategoryInput] = useState("")
  const wasOpen = useRef(false)

  // Initialize category only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setCategoryInput(initialCategory)
    }
    wasOpen.current = open
  }, [open, initialCategory])

  const handleConfirm = useCallback(() => {
    onConfirm(categoryInput)
    setCategoryInput("")
  }, [categoryInput, onConfirm])

  const handleCancel = useCallback(() => {
    setCategoryInput("")
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Set Category for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Select a category from the list or create a new one
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label>Category</Label>
            <Select value={categoryInput || "__none__"} onValueChange={(value: string) => setCategoryInput(value === "__none__" ? "" : value)}>
              <SelectTrigger>
                <SelectValue placeholder="Select a category..." />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">
                  <span className="text-muted-foreground">(No category)</span>
                </SelectItem>
                {availableCategories && Object.keys(availableCategories).map((category) => (
                  <SelectItem key={category} value={category}>
                    {category}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* Option to enter new category */}
          <div className="space-y-2">
            <Label htmlFor="newCategory">Or create new category</Label>
            <Input
              id="newCategory"
              placeholder="Enter new category name"
              value={categoryInput && categoryInput !== "__none__" && (!availableCategories || !Object.keys(availableCategories).includes(categoryInput)) ? categoryInput : ""}
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

  // Virtualization for large tag lists
  const virtualizer = useVirtualizer({
    count: relevantTags.length,
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
                  readOnly={trackerURLs.length === 1}
                />
                {trackerURLs.length === 0 && (
                  <p className="text-xs text-muted-foreground">
                    Enter the complete tracker URL including the announce path
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
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={handleCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleConfirm}
            disabled={!oldURL.trim() || !newURL.trim() || oldURL === newURL || isPending || loadingURLs}
          >
            Update Tracker
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
})

const SHARE_DEFAULT_RATIO_LIMIT = 0
const SHARE_DEFAULT_SEEDING_LIMIT = 0
const SHARE_DEFAULT_INACTIVE_LIMIT = 0
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

interface ShareLimitFormState {
  ratioEnabled: boolean
  ratioLimit: number
  seedingTimeEnabled: boolean
  seedingTimeLimit: number
  inactiveSeedingTimeEnabled: boolean
  inactiveSeedingTimeLimit: number
}

const normalizeShareSignature = (torrent: TorrentLimitSnapshot): string => {
  return [
    safeNumber(torrent.ratio_limit, LIMIT_USE_GLOBAL),
    safeNumber(torrent.seeding_time_limit, LIMIT_USE_GLOBAL),
    safeNumber(torrent.inactive_seeding_time_limit, LIMIT_USE_GLOBAL),
    safeNumber(torrent.max_ratio, LIMIT_UNLIMITED),
    safeNumber(torrent.max_seeding_time, LIMIT_UNLIMITED),
    safeNumber(torrent.max_inactive_seeding_time, LIMIT_UNLIMITED),
  ].join("|")
}

const buildShareLimitInitialState = (torrents?: TorrentLimitSnapshot[]): ShareLimitFormState => {
  const base: ShareLimitFormState = {
    ratioEnabled: false,
    ratioLimit: SHARE_DEFAULT_RATIO_LIMIT,
    seedingTimeEnabled: false,
    seedingTimeLimit: SHARE_DEFAULT_SEEDING_LIMIT,
    inactiveSeedingTimeEnabled: false,
    inactiveSeedingTimeLimit: SHARE_DEFAULT_INACTIVE_LIMIT,
  }

  if (!torrents || torrents.length === 0) {
    return base
  }

  const signatures = torrents.map(normalizeShareSignature)
  const allMatch = signatures.every((signature) => signature === signatures[0])

  if (!allMatch) {
    return base
  }

  const [first] = torrents
  const ratioLimitValue = safeNumber(first.ratio_limit, LIMIT_UNLIMITED)
  const seedingTimeLimitValue = safeNumber(first.seeding_time_limit, LIMIT_UNLIMITED)
  const inactiveSeedingTimeLimitValue = safeNumber(first.inactive_seeding_time_limit, LIMIT_UNLIMITED)

  return {
    ...base,
    ratioEnabled: ratioLimitValue >= 0,
    ratioLimit: ratioLimitValue >= 0 ? ratioLimitValue : base.ratioLimit,
    seedingTimeEnabled: seedingTimeLimitValue >= 0,
    seedingTimeLimit: seedingTimeLimitValue >= 0 ? seedingTimeLimitValue : base.seedingTimeLimit,
    inactiveSeedingTimeEnabled: inactiveSeedingTimeLimitValue >= 0,
    inactiveSeedingTimeLimit:
      inactiveSeedingTimeLimitValue >= 0 ? inactiveSeedingTimeLimitValue : base.inactiveSeedingTimeLimit,
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
  const [useGlobalLimits, setUseGlobalLimits] = useState(false)
  const [ratioEnabled, setRatioEnabled] = useState(false)
  const [ratioLimit, setRatioLimit] = useState(SHARE_DEFAULT_RATIO_LIMIT)
  const [seedingTimeEnabled, setSeedingTimeEnabled] = useState(false)
  const [seedingTimeLimit, setSeedingTimeLimit] = useState(SHARE_DEFAULT_SEEDING_LIMIT) // 24 hours in minutes
  const [inactiveSeedingTimeEnabled, setInactiveSeedingTimeEnabled] = useState(false)
  const [inactiveSeedingTimeLimit, setInactiveSeedingTimeLimit] = useState(SHARE_DEFAULT_INACTIVE_LIMIT) // 7 days in minutes
  const wasOpen = useRef(false)

  const shareInitialState = useMemo(() => buildShareLimitInitialState(torrents), [torrents])

  // Reset form when dialog opens with torrent values
  useEffect(() => {
    if (open && !wasOpen.current) {
      // Check if all torrents have global limits (-2 for all three)
      const hasGlobalLimits = torrents && torrents.length > 0 &&
        torrents.every(t =>
          t.ratio_limit === LIMIT_USE_GLOBAL &&
          t.seeding_time_limit === LIMIT_USE_GLOBAL &&
          t.inactive_seeding_time_limit === LIMIT_USE_GLOBAL
        )

      setUseGlobalLimits(hasGlobalLimits || false)
      setRatioEnabled(!hasGlobalLimits && shareInitialState.ratioEnabled)
      setRatioLimit(shareInitialState.ratioLimit)
      setSeedingTimeEnabled(!hasGlobalLimits && shareInitialState.seedingTimeEnabled)
      setSeedingTimeLimit(shareInitialState.seedingTimeLimit)
      setInactiveSeedingTimeEnabled(!hasGlobalLimits && shareInitialState.inactiveSeedingTimeEnabled)
      setInactiveSeedingTimeLimit(shareInitialState.inactiveSeedingTimeLimit)
    }
    wasOpen.current = open
  }, [open, shareInitialState, torrents])

  const handleConfirm = useCallback((): void => {
    if (useGlobalLimits) {
      // When using global limits, set all to -2
      onConfirm(LIMIT_USE_GLOBAL, LIMIT_USE_GLOBAL, LIMIT_USE_GLOBAL)
    } else {
      onConfirm(
        ratioEnabled ? ratioLimit : -1,  // -1 means unlimited (no limit)
        seedingTimeEnabled ? seedingTimeLimit : -1,
        inactiveSeedingTimeEnabled ? inactiveSeedingTimeLimit : -1
      )
    }
    // Reset form
    setUseGlobalLimits(false)
    setRatioEnabled(false)
    setRatioLimit(SHARE_DEFAULT_RATIO_LIMIT)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(SHARE_DEFAULT_SEEDING_LIMIT)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(SHARE_DEFAULT_INACTIVE_LIMIT)
    onOpenChange(false)
  }, [onConfirm, useGlobalLimits, ratioEnabled, ratioLimit, seedingTimeEnabled, seedingTimeLimit, inactiveSeedingTimeEnabled, inactiveSeedingTimeLimit, onOpenChange])

  const handleCancel = useCallback((): void => {
    setUseGlobalLimits(false)
    setRatioEnabled(false)
    setRatioLimit(SHARE_DEFAULT_RATIO_LIMIT)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(SHARE_DEFAULT_SEEDING_LIMIT)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(SHARE_DEFAULT_INACTIVE_LIMIT)
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Share Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Configure seeding limits or use global defaults from qBittorrent settings.
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          {/* Global limits toggle */}
          <div className="space-y-2 pb-2 border-b">
            <div className="flex items-center space-x-2">
              <Switch
                id="useGlobalLimits"
                checked={useGlobalLimits}
                onCheckedChange={setUseGlobalLimits}
              />
              <Label htmlFor="useGlobalLimits" className="text-sm font-medium">Use global limits</Label>
            </div>
            <p className="text-xs text-muted-foreground ml-6">
              When enabled, torrents will follow the global share limits configured in qBittorrent settings
            </p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="ratioEnabled"
                checked={ratioEnabled}
                onCheckedChange={setRatioEnabled}
                disabled={useGlobalLimits}
              />
              <Label htmlFor="ratioEnabled" className="text-sm">Set ratio limit</Label>
            </div>
            <div className="ml-6 space-y-1">
              <Input
                id="ratioLimit"
                type="number"
                min="0"
                step="0.1"
                value={ratioLimit}
                disabled={!ratioEnabled || useGlobalLimits}
                onChange={(e) => setRatioLimit(parseFloat(e.target.value) || 0)}
                placeholder="0"
              />
              <p className="text-xs text-muted-foreground">
                Stop seeding when ratio reaches this value
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="seedingTimeEnabled"
                checked={seedingTimeEnabled}
                onCheckedChange={setSeedingTimeEnabled}
                disabled={useGlobalLimits}
              />
              <Label htmlFor="seedingTimeEnabled" className="text-sm">Set seeding time limit</Label>
            </div>
            <div className="ml-6 space-y-1">
              <Input
                id="seedingTimeLimit"
                type="number"
                min="0"
                value={seedingTimeLimit}
                disabled={!seedingTimeEnabled || useGlobalLimits}
                onChange={(e) => setSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="0"
              />
              <p className="text-xs text-muted-foreground">
                Minutes (1440 = 24 hours)
              </p>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="inactiveSeedingTimeEnabled"
                checked={inactiveSeedingTimeEnabled}
                onCheckedChange={setInactiveSeedingTimeEnabled}
                disabled={useGlobalLimits}
              />
              <Label htmlFor="inactiveSeedingTimeEnabled" className="text-sm">Set inactive seeding limit</Label>
            </div>
            <div className="ml-6 space-y-1">
              <Input
                id="inactiveSeedingTimeLimit"
                type="number"
                min="0"
                value={inactiveSeedingTimeLimit}
                disabled={!inactiveSeedingTimeEnabled || useGlobalLimits}
                onChange={(e) => setInactiveSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="0"
              />
              <p className="text-xs text-muted-foreground">
                Minutes (10080 = 7 days)
              </p>
            </div>
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
