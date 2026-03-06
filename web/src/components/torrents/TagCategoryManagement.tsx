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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/lib/api"
import type { Category } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface CreateTagDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
}

function useTr() {
  const { t } = useTranslation("common")
  return useCallback(
    (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)),
    [t]
  )
}

export function CreateTagDialog({ open, onOpenChange, instanceId }: CreateTagDialogProps) {
  const tr = useTr()
  const [newTag, setNewTag] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (tags: string[]) => api.createTags(instanceId, tags),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.tagCreated"))
      setNewTag("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedCreateTag"), {
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
          <AlertDialogTitle>{tr("tagCategoryManagement.createTag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("tagCategoryManagement.createTag.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="newTag">{tr("tagCategoryManagement.createTag.nameLabel")}</Label>
          <Input
            id="newTag"
            value={newTag}
            onChange={(e) => setNewTag(e.target.value)}
            placeholder={tr("tagCategoryManagement.createTag.namePlaceholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleCreate()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setNewTag("")}>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!newTag.trim() || mutation.isPending}
          >
            {tr("tagCategoryManagement.actions.create")}
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
  const tr = useTr()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.deleteTags(instanceId, [tag]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.tagDeleted"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedDeleteTag"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("tagCategoryManagement.deleteTag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("tagCategoryManagement.deleteTag.description", { tag })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {tr("tagCategoryManagement.actions.delete")}
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
  parent?: string
}

export function CreateCategoryDialog({ open, onOpenChange, instanceId, parent }: CreateCategoryDialogProps) {
  const tr = useTr()
  const [name, setName] = useState("")
  const [savePath, setSavePath] = useState("")
  const queryClient = useQueryClient()

  // Pre-fill with parent path when dialog opens
  useEffect(() => {
    if (open) {
      if (parent) {
        setName(parent + "/")
      } else {
        setName("")
      }
      setSavePath("")
    }
  }, [open, parent])

  const mutation = useMutation({
    mutationFn: ({ name, savePath }: { name: string; savePath?: string }) =>
      api.createCategory(instanceId, name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.categoryCreated"))
      setName("")
      setSavePath("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedCreateCategory"), {
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
          <AlertDialogTitle>{parent ? tr("tagCategoryManagement.createCategory.subTitle") : tr("tagCategoryManagement.createCategory.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {parent
              ? tr("tagCategoryManagement.createCategory.subDescription", { parent })
              : tr("tagCategoryManagement.createCategory.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="categoryName">
              {parent ? tr("tagCategoryManagement.createCategory.subNameLabel") : tr("tagCategoryManagement.createCategory.nameLabel")}
            </Label>
            <Input
              id="categoryName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={tr("tagCategoryManagement.createCategory.namePlaceholder")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="savePath">{tr("tagCategoryManagement.createCategory.savePathLabel")}</Label>
            <Input
              id="savePath"
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder={tr("tagCategoryManagement.createCategory.savePathPlaceholder")}
            />
          </div>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!name.trim() || mutation.isPending}
          >
            {tr("tagCategoryManagement.actions.create")}
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
  category: Category
}

export function EditCategoryDialog({ open, onOpenChange, instanceId, category }: EditCategoryDialogProps) {
  const tr = useTr()
  const [newSavePath, setNewSavePath] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (savePath: string) => api.editCategory(instanceId, category.name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.categoryUpdated"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedUpdateCategory"), {
        description: error.message,
      })
    },
  })

  const handleSave = () => {
    mutation.mutate(newSavePath.trim())
  }

  const handleOpenChange = (isOpen: boolean) => {
    if (!isOpen) {
      setNewSavePath("")
    }
    onOpenChange(isOpen)
  }

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("tagCategoryManagement.editCategory.title", { category: category.name })}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("tagCategoryManagement.editCategory.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="oldSavePath">{tr("tagCategoryManagement.editCategory.currentPathLabel")}</Label>
          <Input
            id="oldSavePath"
            value={category.savePath || tr("tagCategoryManagement.editCategory.noPath")}
            className={!category.savePath ? "text-muted-foreground italic" : ""}
            disabled={!category.savePath}
            readOnly
          />
          <Label htmlFor="editSavePath">{tr("tagCategoryManagement.editCategory.newPathLabel")}</Label>
          <Input
            id="editSavePath"
            value={newSavePath}
            onChange={(e) => setNewSavePath(e.target.value)}
            placeholder={tr("tagCategoryManagement.createCategory.savePathPlaceholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleSave()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleSave}
            disabled={mutation.isPending}
          >
            {tr("tagCategoryManagement.actions.save")}
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
  const tr = useTr()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.removeCategories(instanceId, [categoryName]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.categoryDeleted"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedDeleteCategory"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("tagCategoryManagement.deleteCategory.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("tagCategoryManagement.deleteCategory.description", { category: categoryName })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {tr("tagCategoryManagement.actions.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface DeleteEmptyCategoriesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  categories: Record<string, Category>
  torrentCounts?: Record<string, number>
}

export function DeleteEmptyCategoriesDialog({
  open,
  onOpenChange,
  instanceId,
  categories,
  torrentCounts = {},
}: DeleteEmptyCategoriesDialogProps) {
  const tr = useTr()
  const queryClient = useQueryClient()

  const emptyCategories = Object.keys(categories).filter(categoryName => {
    const count = torrentCounts[`category:${categoryName}`] || 0
    return count === 0
  })

  const mutation = useMutation({
    mutationFn: () => api.removeCategories(instanceId, emptyCategories),
    onSuccess: () => {
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(tr("tagCategoryManagement.toasts.emptyCategoriesRemoved", { count: emptyCategories.length }))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedRemoveEmptyCategories"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("tagCategoryManagement.deleteEmptyCategories.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {emptyCategories.length === 0 ? (
              tr("tagCategoryManagement.deleteEmptyCategories.none")
            ) : (
              <>
                {tr("tagCategoryManagement.deleteEmptyCategories.confirmWithCannotUndo", { count: emptyCategories.length })}
                <div className="mt-3 max-h-40 overflow-y-auto">
                  <div className="text-sm space-y-1">
                    {emptyCategories.map(categoryName => (
                      <div key={categoryName} className="text-muted-foreground">
                        • {categoryName}
                      </div>
                    ))}
                  </div>
                </div>
              </>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          {emptyCategories.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {tr("tagCategoryManagement.deleteEmptyCategories.action", { count: emptyCategories.length })}
            </AlertDialogAction>
          )}
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
  const tr = useTr()
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
      toast.success(tr("tagCategoryManagement.toasts.unusedTagsDeleted", { count: unusedTags.length }))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(tr("tagCategoryManagement.toasts.failedDeleteUnusedTags"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("tagCategoryManagement.deleteUnusedTags.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {unusedTags.length === 0 ? (
              tr("tagCategoryManagement.deleteUnusedTags.none")
            ) : (
              <>
                {tr("tagCategoryManagement.deleteUnusedTags.confirmWithCannotUndo", { count: unusedTags.length })}
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
          <AlertDialogCancel>{tr("tagCategoryManagement.actions.cancel")}</AlertDialogCancel>
          {unusedTags.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {tr("tagCategoryManagement.deleteUnusedTags.action", { count: unusedTags.length })}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
