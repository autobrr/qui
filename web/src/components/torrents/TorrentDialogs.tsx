/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useState, useEffect, useRef, useCallback } from "react"
import type { ChangeEvent, KeyboardEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { Switch } from "@/components/ui/switch"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
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
import { Plus, X } from "lucide-react"

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
              <ScrollArea className="h-48 border rounded-md p-3">
                <div className="space-y-2">
                  {displayTags.map((tag) => {
                    const isTemporary = temporaryTags.includes(tag)
                    return (
                      <div key={tag} className="flex items-center space-x-2">
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
              </ScrollArea>
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
              <ScrollArea className="h-48 border rounded-md p-3">
                <div className="space-y-2">
                  {displayTags.map((tag) => {
                    const isTemporary = temporaryTags.includes(tag)
                    return (
                      <div key={tag} className="flex items-center space-x-2">
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
              <ScrollArea className="h-48 border rounded-md p-3">
                <div className="space-y-2">
                  {relevantTags.map((tag) => (
                    <div key={tag} className="flex items-center space-x-2">
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
              </ScrollArea>
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

interface ShareLimitDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => void
  isPending?: boolean
}

export const ShareLimitDialog = memo(function ShareLimitDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
}: ShareLimitDialogProps) {
  const [ratioEnabled, setRatioEnabled] = useState(false)
  const [ratioLimit, setRatioLimit] = useState(1.5)
  const [seedingTimeEnabled, setSeedingTimeEnabled] = useState(false)
  const [seedingTimeLimit, setSeedingTimeLimit] = useState(1440) // 24 hours in minutes
  const [inactiveSeedingTimeEnabled, setInactiveSeedingTimeEnabled] = useState(false)
  const [inactiveSeedingTimeLimit, setInactiveSeedingTimeLimit] = useState(10080) // 7 days in minutes
  const wasOpen = useRef(false)

  // Reset form when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      setRatioEnabled(false)
      setRatioLimit(1.5)
      setSeedingTimeEnabled(false)
      setSeedingTimeLimit(1440)
      setInactiveSeedingTimeEnabled(false)
      setInactiveSeedingTimeLimit(10080)
    }
    wasOpen.current = open
  }, [open])

  const handleConfirm = useCallback((): void => {
    onConfirm(
      ratioEnabled ? ratioLimit : -1,
      seedingTimeEnabled ? seedingTimeLimit : -1,
      inactiveSeedingTimeEnabled ? inactiveSeedingTimeLimit : -1
    )
    // Reset form
    setRatioEnabled(false)
    setRatioLimit(1.5)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(1440)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(10080)
    onOpenChange(false)
  }, [onConfirm, ratioEnabled, ratioLimit, seedingTimeEnabled, seedingTimeLimit, inactiveSeedingTimeEnabled, inactiveSeedingTimeLimit, onOpenChange])

  const handleCancel = useCallback((): void => {
    setRatioEnabled(false)
    setRatioLimit(1.5)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(1440)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(10080)
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Share Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Configure seeding limits. Use -1 or disable to remove limits.
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="ratioEnabled"
                checked={ratioEnabled}
                onCheckedChange={setRatioEnabled}
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
                disabled={!ratioEnabled}
                onChange={(e) => setRatioLimit(parseFloat(e.target.value) || 0)}
                placeholder="1.5"
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
              />
              <Label htmlFor="seedingTimeEnabled" className="text-sm">Set seeding time limit</Label>
            </div>
            <div className="ml-6 space-y-1">
              <Input
                id="seedingTimeLimit"
                type="number"
                min="0"
                value={seedingTimeLimit}
                disabled={!seedingTimeEnabled}
                onChange={(e) => setSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="1440"
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
              />
              <Label htmlFor="inactiveSeedingTimeEnabled" className="text-sm">Set inactive seeding limit</Label>
            </div>
            <div className="ml-6 space-y-1">
              <Input
                id="inactiveSeedingTimeLimit"
                type="number"
                min="0"
                value={inactiveSeedingTimeLimit}
                disabled={!inactiveSeedingTimeEnabled}
                onChange={(e) => setInactiveSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="10080"
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
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending?: boolean
}

export const SpeedLimitsDialog = memo(function SpeedLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
}: SpeedLimitsDialogProps) {
  const [uploadEnabled, setUploadEnabled] = useState(false)
  const [uploadLimit, setUploadLimit] = useState(1024)
  const [downloadEnabled, setDownloadEnabled] = useState(false)
  const [downloadLimit, setDownloadLimit] = useState(1024)
  const wasOpen = useRef(false)

  // Reset form when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      setUploadEnabled(false)
      setUploadLimit(1024)
      setDownloadEnabled(false)
      setDownloadLimit(1024)
    }
    wasOpen.current = open
  }, [open])

  const handleConfirm = useCallback((): void => {
    onConfirm(
      uploadEnabled ? uploadLimit : -1,
      downloadEnabled ? downloadLimit : -1
    )
    // Reset form
    setUploadEnabled(false)
    setUploadLimit(1024)
    setDownloadEnabled(false)
    setDownloadLimit(1024)
  }, [onConfirm, uploadEnabled, uploadLimit, downloadEnabled, downloadLimit])

  const handleCancel = useCallback((): void => {
    setUploadEnabled(false)
    setUploadLimit(1024)
    setDownloadEnabled(false)
    setDownloadLimit(1024)
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Set Speed Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Set upload and download speed limits in KB/s. Use -1 or disable to remove limits.
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
              placeholder="1024"
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
              placeholder="1024"
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
