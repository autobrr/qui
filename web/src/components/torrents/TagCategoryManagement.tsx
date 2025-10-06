/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
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
import type { Category } from "@/types";

interface CreateTagDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
}

export function CreateTagDialog({ open, onOpenChange, instanceId }: CreateTagDialogProps) {
  const { t } = useTranslation()
  const [newTag, setNewTag] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (tags: string[]) => api.createTags(instanceId, tags),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tag_category_management_dialogs.create_tag.success"))
      setNewTag("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.create_tag.error"), {
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
          <AlertDialogTitle>{t("tag_category_management_dialogs.create_tag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tag_category_management_dialogs.create_tag.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="newTag">{t("tag_category_management_dialogs.create_tag.label")}</Label>
          <Input
            id="newTag"
            value={newTag}
            onChange={(e) => setNewTag(e.target.value)}
            placeholder={t("tag_category_management_dialogs.create_tag.placeholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleCreate()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setNewTag("")}>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!newTag.trim() || mutation.isPending}
          >
            {t("common.buttons.create")}
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
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.deleteTags(instanceId, [tag]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["tags", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tag_category_management_dialogs.delete_tag.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.delete_tag.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tag_category_management_dialogs.delete_tag.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tag_category_management_dialogs.delete_tag.description", { tag })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {t("common.buttons.delete")}
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
  const { t } = useTranslation()
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
      toast.success(t("tag_category_management_dialogs.create_category.success"))
      setName("")
      setSavePath("")
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.create_category.error"), {
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
          <AlertDialogTitle>{t("tag_category_management_dialogs.create_category.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tag_category_management_dialogs.create_category.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="categoryName">{t("tag_category_management_dialogs.create_category.name_label")}</Label>
            <Input
              id="categoryName"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("tag_category_management_dialogs.create_category.name_placeholder")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="savePath">{t("tag_category_management_dialogs.create_category.path_label")}</Label>
            <Input
              id="savePath"
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder={t("tag_category_management_dialogs.create_category.path_placeholder")}
            />
          </div>
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel
            onClick={() => {
              setName("")
              setSavePath("")
            }}
          >
            {t("common.cancel")}
          </AlertDialogCancel>
          <AlertDialogAction
            onClick={handleCreate}
            disabled={!name.trim() || mutation.isPending}
          >
                                  {t("common.buttons.create")}          </AlertDialogAction>
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
  const { t } = useTranslation()
  const [newSavePath, setNewSavePath] = useState("")
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: (savePath: string) => api.editCategory(instanceId, category.name, savePath),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tag_category_management_dialogs.edit_category.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.edit_category.error"), {
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
          <AlertDialogTitle>
            {t("tag_category_management_dialogs.edit_category.title", { name: category.name })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("tag_category_management_dialogs.edit_category.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="oldSavePath">{t("tag_category_management_dialogs.edit_category.current_path_label")}</Label>
          <Input
            id="oldSavePath"
            value={category.savePath || t("tag_category_management_dialogs.edit_category.no_path")}
            className={!category.savePath ? "text-muted-foreground italic" : ""}
            disabled={!category.savePath}
            readOnly
          />
          <Label htmlFor="editSavePath">{t("tag_category_management_dialogs.edit_category.new_path_label")}</Label>
          <Input
            id="editSavePath"
            value={newSavePath}
            onChange={(e) => setNewSavePath(e.target.value)}
            placeholder={t("tag_category_management_dialogs.edit_category.new_path_placeholder")}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                handleSave()
              }
            }}
          />
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleSave}
            disabled={mutation.isPending}
          >
                              {t("common.buttons.save")}          </AlertDialogAction>
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
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: () => api.removeCategories(instanceId, [categoryName]),
    onSuccess: () => {
      // Refetch instead of invalidate to keep showing stale data
      queryClient.refetchQueries({ queryKey: ["categories", instanceId] })
      queryClient.refetchQueries({ queryKey: ["instance-metadata", instanceId] })
      toast.success(t("tag_category_management_dialogs.delete_category.success"))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.delete_category.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tag_category_management_dialogs.delete_category.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("tag_category_management_dialogs.delete_category.description", { name: categoryName })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {t("common.buttons.delete")}
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
  const { t } = useTranslation()
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
      toast.success(t("tag_category_management_dialogs.delete_unused_tags.success", { count: unusedTags.length }))
      onOpenChange(false)
    },
    onError: (error: Error) => {
      toast.error(t("tag_category_management_dialogs.delete_unused_tags.error"), {
        description: error.message,
      })
    },
  })

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("tag_category_management_dialogs.delete_unused_tags.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {unusedTags.length === 0 ? (
              t("tag_category_management_dialogs.delete_unused_tags.no_unused")
            ) : (
              <>
                {t("tag_category_management_dialogs.delete_unused_tags.confirm", { count: unusedTags.length })}
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
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          {unusedTags.length > 0 && (
            <AlertDialogAction
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("tag_category_management_dialogs.delete_unused_tags.delete_button", { count: unusedTags.length })}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}