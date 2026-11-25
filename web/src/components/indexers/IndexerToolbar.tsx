/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ArrowDownWideNarrow, ArrowUpNarrowWide, Loader2, MoreHorizontal, SlidersHorizontal, TestTube, Trash2 } from "lucide-react"
import { useId, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { activeFilterCount, NO_FILTERS, type IndexerFilters, type IndexerSort } from "@/components/indexers/indexer-view"
import { Editor } from "@/components/kit/Editor"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useIsPhone } from "@/hooks/useMediaQuery"

const FILTER_GROUPS = [
  {
    key: "status",
    labelKey: "indexers.table.filterStatus",
    options: [
      { value: "all", labelKey: "indexers.table.filterAll" },
      { value: "enabled", labelKey: "indexers.table.filterEnabled" },
      { value: "disabled", labelKey: "indexers.table.filterDisabled" },
    ],
  },
  {
    key: "test",
    labelKey: "indexers.table.filterTestStatus",
    options: [
      { value: "all", labelKey: "indexers.table.filterAll" },
      { value: "ok", labelKey: "indexers.table.working" },
      { value: "error", labelKey: "indexers.table.failed" },
      { value: "untested", labelKey: "indexers.table.untested" },
    ],
  },
  {
    key: "backend",
    labelKey: "indexers.table.filterBackend",
    options: [
      { value: "all", labelKey: "indexers.table.filterAll" },
      { value: "jackett", labelKey: "indexers.table.filterJackett" },
      { value: "prowlarr", labelKey: "indexers.table.filterProwlarr" },
      { value: "native", labelKey: "indexers.table.filterNative" },
    ],
  },
] as const satisfies readonly { key: keyof IndexerFilters; labelKey: string; options: readonly { value: string; labelKey: string }[] }[]

const SORT_FIELDS = [
  { value: "priority", labelKey: "indexers.table.priority" },
  { value: "name", labelKey: "indexers.table.name" },
  { value: "backend", labelKey: "indexers.table.backend" },
  { value: "status", labelKey: "indexers.table.status" },
] as const satisfies readonly { value: IndexerSort["field"]; labelKey: string }[]

interface IndexerToolbarProps {
  summary: string
  filters: IndexerFilters
  onFiltersChange: (filters: IndexerFilters) => void
  sort: IndexerSort
  onSortChange: (sort: IndexerSort) => void
  onTestAll: () => void
  testing: boolean
  onDeleteAll: () => void
}

export function IndexerToolbar({ summary, filters, onFiltersChange, sort, onSortChange, onTestAll, testing, onDeleteAll }: IndexerToolbarProps) {
  const { t } = useTranslation("settings")
  const phone = useIsPhone()
  const [panelOpen, setPanelOpen] = useState(false)
  const filterCount = activeFilterCount(filters)

  const trigger = (
    <Button variant="outline" size="sm" className="relative max-sm:size-10 max-sm:px-0" onClick={phone ? () => setPanelOpen(true) : undefined}>
      <SlidersHorizontal />
      <span className="max-sm:sr-only">{t("indexers.table.sortAndFilter")}</span>
      {filterCount > 0 && (
        <span className="grid min-w-5 place-items-center rounded-full bg-primary px-1 text-xs tabular-nums text-primary-foreground max-sm:absolute max-sm:-top-1.5 max-sm:-right-1.5">
          {filterCount}
        </span>
      )}
    </Button>
  )
  const panel = <SortAndFilters filters={filters} onFiltersChange={onFiltersChange} sort={sort} onSortChange={onSortChange} />
  const clear = (
    <Button type="button" variant={phone ? "outline" : "ghost"} size={phone ? "default" : "sm"} onClick={() => onFiltersChange(NO_FILTERS)}>
      {t("indexers.table.clearFilters")}
    </Button>
  )

  return (
    <div className="flex items-center gap-2">
      <p className="min-w-0 flex-1 text-sm text-muted-foreground">{summary}</p>

      {phone ? (
        <>
          {trigger}
          <Editor
            open={panelOpen}
            onOpenChange={setPanelOpen}
            title={t("indexers.table.sortAndFilter")}
            secondaryActions={filterCount > 0 ? clear : undefined}
          >
            {panel}
          </Editor>
        </>
      ) : (
        <Popover open={panelOpen} onOpenChange={setPanelOpen}>
          <PopoverTrigger asChild>{trigger}</PopoverTrigger>
          <PopoverContent align="end" collisionPadding={16} className="w-96 p-0">
            <div className="p-4">{panel}</div>
            {filterCount > 0 && <div className="flex justify-end border-t p-2">{clear}</div>}
          </PopoverContent>
        </Popover>
      )}

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="icon" className="size-8 max-sm:size-10" aria-label={t("common:kit.moreActions")}>
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-44">
          <DropdownMenuItem className="pointer-coarse:py-2.5" disabled={testing} onSelect={onTestAll}>
            {testing ? <Loader2 className="animate-spin" /> : <TestTube />}
            {t("indexers.table.testAll")}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" className="pointer-coarse:py-2.5" onSelect={onDeleteAll}>
            <Trash2 />
            {t("indexers.page.deleteAll")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

interface SortAndFiltersProps {
  filters: IndexerFilters
  onFiltersChange: (filters: IndexerFilters) => void
  sort: IndexerSort
  onSortChange: (sort: IndexerSort) => void
}

function SortAndFilters({ filters, onFiltersChange, sort, onSortChange }: SortAndFiltersProps) {
  const { t } = useTranslation("settings")
  const ascending = sort.direction === "asc"

  return (
    <div className="space-y-4">
      <Segmented
        label={t("indexers.table.sortBy")}
        value={sort.field}
        options={SORT_FIELDS.map((option) => ({ value: option.value, label: t(option.labelKey) }))}
        onChange={(field) => onSortChange({ ...sort, field })}
        aside={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="-mr-2 h-7 gap-1 px-2 text-xs text-muted-foreground pointer-coarse:h-9"
            onClick={() => onSortChange({ ...sort, direction: ascending ? "desc" : "asc" })}
          >
            {ascending ? <ArrowUpNarrowWide /> : <ArrowDownWideNarrow />}
            <span className="sr-only">{t("indexers.table.order")}: </span>
            {t(ascending ? "indexers.table.sortAscending" : "indexers.table.sortDescending")}
          </Button>
        }
      />
      <div className="space-y-4 border-t pt-4">
        {FILTER_GROUPS.map((group) => (
          <Segmented
            key={group.key}
            label={t(group.labelKey)}
            value={filters[group.key]}
            options={group.options.map((option) => ({ value: option.value, label: t(option.labelKey) }))}
            onChange={(value) => onFiltersChange({ ...filters, [group.key]: value })}
          />
        ))}
      </div>
    </div>
  )
}

interface SegmentedProps<V extends string> {
  label: string
  value: V
  options: readonly { value: V; label: string }[]
  onChange: (value: V) => void
  aside?: ReactNode
}

function Segmented<V extends string>({ label, value, options, onChange, aside }: SegmentedProps<V>) {
  const name = useId()
  const labelId = useId()

  return (
    <div className="space-y-1.5">
      <div className="flex min-h-7 items-center justify-between gap-2">
        <span id={labelId} className="text-xs font-medium text-muted-foreground">{label}</span>
        {aside}
      </div>
      <div role="radiogroup" aria-labelledby={labelId} className="flex gap-0.5 rounded-lg bg-muted p-0.75">
        {options.map((option) => (
          <label
            key={option.value}
            className="flex h-8 min-w-0 flex-auto cursor-pointer items-center justify-center rounded-md border border-transparent px-2 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground has-checked:border-muted-foreground/30 has-checked:bg-background has-checked:text-foreground has-checked:shadow-sm has-focus-visible:ring-[3px] has-focus-visible:ring-ring/50 dark:has-checked:bg-input/30 pointer-coarse:h-9"
          >
            <input type="radio" name={name} value={option.value} checked={value === option.value} onChange={() => onChange(option.value)} className="sr-only" />
            <TruncatedText openOnTap={false}>{option.label}</TruncatedText>
          </label>
        ))}
      </div>
    </div>
  )
}
