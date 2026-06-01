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
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface CreateTagDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
}

export function CreateTagDialog({ open, onOpenChange, instanceId }: CreateTagDialogProps) {
  const { t } = useTranslation("torrents")
  const [newTag, setNewTag] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (tags: string[]) => api.createTags(instanceId, tags),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tagCategoryManagement.createTag.toast.success"))
      setNewTag("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.createTag.toast.error"), {
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
          <AlertDialogTitle>{t("tagCategoryManagement.createTag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tagCategoryManagement.createTag.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="newTag">{t("tagCategoryManagement.createTag.tagName")}</Label>
          <Input
            id="newTag"
            value={newTag}
            onChange={(e) => setNewTag(e.target.value)}
            placeholder={t("tagCategoryManagement.createTag.placeholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleCreate()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setNewTag("")}>{t("tagCategoryManagement.createTag.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!newTag.trim() || mutation.isPending}
          >
            {t("tagCategoryManagement.createTag.create")}
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
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.deleteTags(instanceId, [tag]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tagCategoryManagement.deleteTag.toast.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.deleteTag.toast.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tagCategoryManagement.deleteTag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tagCategoryManagement.deleteTag.description", { tag })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("tagCategoryManagement.deleteTag.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {t("tagCategoryManagement.deleteTag.delete")}
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
  const { t } = useTranslation("torrents")
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
      toast.success(t("tagCategoryManagement.createCategory.toast.success"))
      setName("")
      setSavePath("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.createCategory.toast.error"), {
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
          <AlertDialogTitle>{parent ? t("tagCategoryManagement.createCategory.createSubcategoryTitle") : t("tagCategoryManagement.createCategory.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {parent ? t("tagCategoryManagement.createCategory.subcategoryDescription", { parent }) : t("tagCategoryManagement.createCategory.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="categoryName">{parent ? t("tagCategoryManagement.createCategory.subcategoryName") : t("tagCategoryManagement.createCategory.categoryName")}</Label>
            <Input
              id="categoryName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("tagCategoryManagement.createCategory.placeholder")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="savePath">{t("tagCategoryManagement.createCategory.savePath")}</Label>
            <Input
              id="savePath"
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder={t("tagCategoryManagement.createCategory.savePathPlaceholder")}
            />
          </div>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("tagCategoryManagement.createCategory.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!name.trim() || mutation.isPending}
          >
            {t("tagCategoryManagement.createCategory.create")}
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
  const { t } = useTranslation("torrents")
  const [newSavePath, setNewSavePath] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (savePath: string) => api.editCategory(instanceId, category.name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tagCategoryManagement.editCategory.toast.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.editCategory.toast.error"), {
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
          <AlertDialogTitle>{t("tagCategoryManagement.editCategory.title", { name: category.name })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tagCategoryManagement.editCategory.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="oldSavePath">{t("tagCategoryManagement.editCategory.currentSavePath")}</Label>
          <Input
            id="oldSavePath"
            value={category.savePath || t("tagCategoryManagement.editCategory.noSavePath")}
            className={!category.savePath ? "text-muted-foreground italic" : ""}
            disabled={!category.savePath}
            readOnly
          />
          <Label htmlFor="editSavePath">{t("tagCategoryManagement.editCategory.newSavePath")}</Label>
          <Input
            id="editSavePath"
            value={newSavePath}
            onChange={(e) => setNewSavePath(e.target.value)}
            placeholder={t("tagCategoryManagement.editCategory.placeholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleSave()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("tagCategoryManagement.editCategory.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleSave}
            disabled={mutation.isPending}
          >
            {t("tagCategoryManagement.editCategory.save")}
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
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.removeCategories(instanceId, [categoryName]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tagCategoryManagement.deleteCategory.toast.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.deleteCategory.toast.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tagCategoryManagement.deleteCategory.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tagCategoryManagement.deleteCategory.description", { name: categoryName })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("tagCategoryManagement.deleteCategory.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {t("tagCategoryManagement.deleteCategory.delete")}
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
  const { t } = useTranslation("torrents")
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
      toast.success(t("tagCategoryManagement.deleteEmptyCategories.toast.success", { count: emptyCategories.length, plural: emptyCategories.length === 1 ? "y" : "ies" }))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.deleteEmptyCategories.toast.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tagCategoryManagement.deleteEmptyCategories.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {emptyCategories.length === 0 ? (
              t("tagCategoryManagement.deleteEmptyCategories.noEmpty")
            ) : (
              <>
                {t("tagCategoryManagement.deleteEmptyCategories.confirm", { count: emptyCategories.length, plural: emptyCategories.length === 1 ? "y" : "ies" })}
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
          <AlertDialogCancel>{t("tagCategoryManagement.deleteEmptyCategories.cancel")}</AlertDialogCancel>
          {emptyCategories.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("tagCategoryManagement.deleteEmptyCategories.remove", { count: emptyCategories.length, plural: emptyCategories.length === 1 ? "y" : "ies" })}
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
  const { t } = useTranslation("torrents")
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
      toast.success(t("tagCategoryManagement.deleteUnusedTags.toast.success", { count: unusedTags.length, plural: unusedTags.length !== 1 ? "s" : "" }))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tagCategoryManagement.deleteUnusedTags.toast.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tagCategoryManagement.deleteUnusedTags.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {unusedTags.length === 0 ? (
              t("tagCategoryManagement.deleteUnusedTags.noUnused")
            ) : (
              <>
                {t("tagCategoryManagement.deleteUnusedTags.confirm", { count: unusedTags.length, plural: unusedTags.length !== 1 ? "s" : "" })}
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
          <AlertDialogCancel>{t("tagCategoryManagement.deleteUnusedTags.cancel")}</AlertDialogCancel>
          {unusedTags.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("tagCategoryManagement.deleteUnusedTags.delete", { count: unusedTags.length, plural: unusedTags.length !== 1 ? "s" : "" })}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
