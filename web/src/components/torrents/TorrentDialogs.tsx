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
  availableTags: string[]
  hashCount: number
  onConfirm: (tags: string[]) => void
  isPending?: boolean
  initialTags?: string[]
}

interface AddTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[]
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
  const displayTags = [...availableTags, ...temporaryTags].sort()

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
      if (!availableTags.includes(trimmedTag)) {
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
  const displayTags = [...availableTags, ...temporaryTags].sort()

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
      if (!availableTags.includes(trimmedTag)) {
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
  availableTags: string[]
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
  const relevantTags = availableTags.filter(tag => currentTags.includes(tag))

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
