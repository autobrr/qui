/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { toast } from "sonner"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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

interface CreateTagDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
}

export function CreateTagDialog({ open, onOpenChange, instanceId }: CreateTagDialogProps) {
  const [newTag, setNewTag] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (tags: string[]) => api.createTags(instanceId, tags),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success("Tag created successfully")
      setNewTag("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to create tag", {
        description: error.message,
      })
    },
  })

  const handleCreate = () => {
    if (newTag.trim()) {
      mutation.mutate([newTag.trim()])
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Create New Tag</AlertDialogTitle>
          <AlertDialogDescription>
            Enter a name for the new tag
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4">
          <Label htmlFor="newTag">Tag Name</Label>
          <Input
            id="newTag"
            value={newTag}
            onChange={(e) => setNewTag(e.target.value)}
            placeholder="Enter tag name"
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleCreate()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setNewTag("")}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!newTag.trim() || mutation.isPending}
          >
            Create
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface DeleteTagDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  tag: string
}

export function DeleteTagDialog({ open, onOpenChange, instanceId, tag }: DeleteTagDialogProps) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.deleteTags(instanceId, [tag]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success("Tag deleted successfully")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to delete tag", {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Tag</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete the tag "{tag}"? This action cannot be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface CreateCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
}

export function CreateCategoryDialog({ open, onOpenChange, instanceId }: CreateCategoryDialogProps) {
  const [name, setName] = useState("")
  const [savePath, setSavePath] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: ({ name, savePath }: { name: string; savePath?: string }) => 
      api.createCategory(instanceId, name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success("Category created successfully")
      setName("")
      setSavePath("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to create category", {
        description: error.message,
      })
    },
  })

  const handleCreate = () => {
    if (name.trim()) {
      mutation.mutate({ name: name.trim(), savePath: savePath.trim() || undefined })
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Create New Category</AlertDialogTitle>
          <AlertDialogDescription>
            Enter details for the new category
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-4">
          <div>
            <Label htmlFor="categoryName">Category Name</Label>
            <Input
              id="categoryName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Enter category name"
            />
          </div>
          <div>
            <Label htmlFor="savePath">Save Path (optional)</Label>
            <Input
              id="savePath"
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder="e.g. /downloads/movies"
            />
          </div>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => {
            setName("")
            setSavePath("")
          }}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!name.trim() || mutation.isPending}
          >
            Create
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface EditCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  category: { name: string; savePath: string }
}

export function EditCategoryDialog({ open, onOpenChange, instanceId, category }: EditCategoryDialogProps) {
  const [savePath, setSavePath] = useState(category.savePath)
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (savePath: string) => api.editCategory(instanceId, category.name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success("Category updated successfully")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to update category", {
        description: error.message,
      })
    },
  })

  const handleSave = () => {
    mutation.mutate(savePath.trim())
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Edit Category: {category.name}</AlertDialogTitle>
          <AlertDialogDescription>
            Update the save path for this category
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4">
          <Label htmlFor="editSavePath">Save Path</Label>
          <Input
            id="editSavePath"
            value={savePath}
            onChange={(e) => setSavePath(e.target.value)}
            placeholder="e.g. /downloads/movies"
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleSave()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleSave}
            disabled={mutation.isPending}
          >
            Save
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface DeleteCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  categoryName: string
}

export function DeleteCategoryDialog({ open, onOpenChange, instanceId, categoryName }: DeleteCategoryDialogProps) {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.removeCategories(instanceId, [categoryName]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success("Category deleted successfully")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to delete category", {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Category</AlertDialogTitle>
          <AlertDialogDescription>
            Are you sure you want to delete the category "{categoryName}"? 
            Torrents in this category will become uncategorized.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface DeleteUnusedTagsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  tags: string[]
  torrentCounts?: Record<string, number>
}

export function DeleteUnusedTagsDialog({ 
  open, 
  onOpenChange, 
  instanceId, 
  tags,
  torrentCounts = {},
}: DeleteUnusedTagsDialogProps) {
  const queryClient = useQueryClient()
  
  // Find unused tags (tags with 0 torrents)
  const unusedTags = tags.filter(tag => {
    const count = torrentCounts[`tag:${tag}`] || 0
    return count === 0
  })

  const mutation = useMutation({
    mutationFn: () => api.deleteTags(instanceId, unusedTags),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(`Deleted ${unusedTags.length} unused tag${unusedTags.length !== 1 ? "s" : ""}`)
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error("Failed to delete unused tags", {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete Unused Tags</AlertDialogTitle>
          <AlertDialogDescription>
            {unusedTags.length === 0 ? (
              "There are no unused tags to delete."
            ) : (
              <>
                Are you sure you want to delete {unusedTags.length} unused tag{unusedTags.length !== 1 ? "s" : ""}? 
                This action cannot be undone.
                <div className="mt-3 max-h-40 overflow-y-auto">
                  <div className="text-sm space-y-1">
                    {unusedTags.map(tag => (
                      <div key={tag} className="text-muted-foreground">
                        • {tag}
                      </div>
                    ))}
                  </div>
                </div>
              </>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          {unusedTags.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete {unusedTags.length} Tag{unusedTags.length !== 1 ? "s" : ""}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}