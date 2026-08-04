/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AccordionContent,
  AccordionItem,
  AccordionTrigger
} from "@/components/ui/accordion"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  useCreateFilterView,
  useDeleteFilterView,
  useFilterViews,
  useUpdateFilterView
} from "@/hooks/useFilterViews"
import { filterViewsEqual, toViewFilters } from "@/lib/filter-views"
import { cn } from "@/lib/utils"
import type { FilterView, TorrentFilters } from "@/types"
import { Bookmark, MoreVertical, Pencil, Plus, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

interface FilterViewsSectionProps {
  selectedFilters: TorrentFilters
  /** Must be the sidebar's applyFilterChange so subcategory expansion is recomputed. */
  onApply: (filters: TorrentFilters) => void
  hasActiveFilters: boolean
  triggerClassName: string
  contentClassName: string
  itemClassName: string
}

export function FilterViewsSection({
  selectedFilters,
  onApply,
  hasActiveFilters,
  triggerClassName,
  contentClassName,
  itemClassName,
}: FilterViewsSectionProps) {
  const { t } = useTranslation("torrents")
  const { data: views } = useFilterViews()
  const createView = useCreateFilterView()
  const updateView = useUpdateFilterView()
  const deleteView = useDeleteFilterView()

  const [nameDialogOpen, setNameDialogOpen] = useState(false)
  const [renameTarget, setRenameTarget] = useState<FilterView | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<FilterView | null>(null)
  const [nameDraft, setNameDraft] = useState("")

  const openSaveDialog = () => {
    setRenameTarget(null)
    setNameDraft("")
    setNameDialogOpen(true)
  }

  const openRenameDialog = (view: FilterView) => {
    setRenameTarget(view)
    setNameDraft(view.name)
    setNameDialogOpen(true)
  }

  const submitName = () => {
    const name = nameDraft.trim()
    if (!name || createView.isPending || updateView.isPending) return

    // Close only once the write lands, so a 409 leaves the typed name in place.
    const opts = { onSuccess: () => setNameDialogOpen(false) }
    if (renameTarget) {
      updateView.mutate({
        id: renameTarget.id,
        data: { name, filters: renameTarget.filters },
      }, opts)
    } else {
      createView.mutate({ name, filters: toViewFilters(selectedFilters) }, opts)
    }
  }

  return (
    <>
      <AccordionItem value="views" className="border rounded-lg">
        <AccordionTrigger className={cn(triggerClassName, "hover:no-underline")}>
          <div className="flex items-center gap-2">
            <Bookmark className="h-4 w-4" />
            <span className="text-sm font-medium">{t("filterSidebar.views.title")}</span>
          </div>
        </AccordionTrigger>
        <AccordionContent className={contentClassName}>
          <div className="flex flex-col">
            {views?.length === 0 && (
              <p className={cn("text-xs text-muted-foreground", itemClassName)}>
                {t("filterSidebar.views.empty")}
              </p>
            )}

            {views?.map(view => {
              const isActive = filterViewsEqual(selectedFilters, view.filters)
              return (
                <div
                  key={view.id}
                  className={cn("flex items-center rounded transition-colors", isActive ? "bg-muted" : "hover:bg-muted/50")}
                >
                  <button
                    type="button"
                    onClick={() => onApply(toViewFilters(view.filters))}
                    className={cn("flex-1 min-w-0 truncate text-left text-sm", itemClassName, isActive && "font-medium")}
                  >
                    {view.name}
                  </button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        className="shrink-0 px-1 text-muted-foreground hover:text-foreground"
                        aria-label={t("filterSidebar.views.actions")}
                      >
                        <MoreVertical className="h-3.5 w-3.5" />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onSelect={() => openRenameDialog(view)}>
                        <Pencil className="h-4 w-4" />
                        {t("filterSidebar.views.rename")}
                      </DropdownMenuItem>
                      <DropdownMenuItem variant="destructive" onSelect={() => setDeleteTarget(view)}>
                        <Trash2 className="h-4 w-4" />
                        {t("filterSidebar.views.delete")}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              )
            })}

            <button
              type="button"
              disabled={!hasActiveFilters}
              onClick={openSaveDialog}
              className={cn(
                "mt-1 flex items-center gap-1.5 self-start rounded text-xs text-muted-foreground transition-colors hover:bg-muted/50 hover:text-foreground disabled:pointer-events-none disabled:opacity-50",
                itemClassName
              )}
            >
              <Plus className="h-3.5 w-3.5" />
              {t("filterSidebar.views.saveCurrent")}
            </button>
          </div>
        </AccordionContent>
      </AccordionItem>

      <AlertDialog open={nameDialogOpen} onOpenChange={setNameDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {renameTarget ? t("filterSidebar.views.renameTitle") : t("filterSidebar.views.saveTitle")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {renameTarget ? t("filterSidebar.views.renameDescription") : t("filterSidebar.views.saveDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="py-4 space-y-2">
            <Label htmlFor="filterViewName">{t("filterSidebar.views.nameLabel")}</Label>
            <Input
              id="filterViewName"
              value={nameDraft}
              maxLength={100}
              onChange={(e) => setNameDraft(e.target.value)}
              placeholder={t("filterSidebar.views.namePlaceholder")}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  submitName()
                }
              }}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("filterSidebar.views.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                submitName()
              }}
              disabled={!nameDraft.trim() || createView.isPending || updateView.isPending}
            >
              {t("filterSidebar.views.save")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("filterSidebar.views.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("filterSidebar.views.deleteDescription", { name: deleteTarget?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("filterSidebar.views.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteTarget && deleteView.mutate(deleteTarget.id)}
              disabled={deleteView.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("filterSidebar.views.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
