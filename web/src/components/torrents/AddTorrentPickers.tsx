/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  ResponsiveCommand,
  ResponsiveCommandEmpty,
  ResponsiveCommandInput,
  ResponsiveCommandItem,
  ResponsiveCommandList,
  ResponsiveCommandPopover,
  useResponsiveMobile
} from "@/components/ui/responsive-command-popover"
import { TruncatedText } from "@/components/ui/truncated-text"
import { parseTypedTags } from "@/lib/add-torrent-tags"
import { cn } from "@/lib/utils"
import type { Category } from "@/types"
import { Check, ChevronsUpDown, Plus, Tag, X } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

const POPOVER_WIDTH = "max(var(--radix-popover-trigger-width), 22rem)"
const MAX_TAG_CHIPS = 8

const byName = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" }).compare

function matches(value: string, query: string) {
  const name = value.toLowerCase()
  return query.toLowerCase().split(/\s+/).every(word => name.includes(word))
}

interface CategoryPickerProps {
  id?: string
  categories: Record<string, Category> | undefined
  value: string
  onChange: (value: string) => void
}

export function CategoryPicker({ id, categories, value, onChange }: CategoryPickerProps) {
  const { t } = useTranslation("torrents")
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")

  const sorted = useMemo(
    () => Object.values(categories ?? {}).sort((a, b) => byName(a.name, b.name)),
    [categories]
  )

  const handleOpenChange = (next: boolean) => {
    if (next) setQuery("")
    setOpen(next)
  }

  const pick = (name: string) => {
    onChange(name)
    handleOpenChange(false)
  }

  return (
    <ResponsiveCommandPopover
      open={open}
      onOpenChange={handleOpenChange}
      title={t("addTorrentDialog.options.category")}
      popoverWidth={POPOVER_WIDTH}
      trigger={
        <Button
          id={id}
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between font-normal h-10"
        >
          <span className={cn("truncate", !value && "text-muted-foreground")}>
            {value || t("addTorrentDialog.options.noCategory")}
          </span>
          <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
        </Button>
      }
    >
      <ResponsiveCommand shouldFilter={false} defaultValue={value || "__none__"}>
        <PickerDone onDone={() => handleOpenChange(false)} />
        <ResponsiveCommandInput
          placeholder={t("addTorrentDialog.options.searchCategories")}
          value={query}
          onValueChange={setQuery}
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
        />
        <ResponsiveCommandList>
          <PickerList>
            {!query.trim() && (
              <PickerItem
                value="__none__"
                selected={!value}
                onSelect={() => pick("")}
                label={<span className="text-muted-foreground">{t("addTorrentDialog.options.noCategory")}</span>}
              />
            )}
            {sorted.filter(cat => matches(cat.name, query)).map(cat => (
              <PickerItem
                searching={query.trim() !== ""}
                key={cat.name}
                value={cat.name}
                selected={cat.name === value}
                onSelect={() => pick(cat.name)}
                label={<CategoryName name={cat.name} />}
                hint={cat.savePath}
              />
            ))}
          </PickerList>
          <ResponsiveCommandEmpty>
            {t("addTorrentDialog.options.noCategoriesMatch", { query: query.trim() })}
          </ResponsiveCommandEmpty>
        </ResponsiveCommandList>
      </ResponsiveCommand>
    </ResponsiveCommandPopover>
  )
}

function CategoryName({ name }: { name: string }) {
  const slash = name.lastIndexOf("/")
  if (slash === -1) return <span className="break-words">{name}</span>
  return (
    <span className="break-words">
      <span className="text-muted-foreground">{name.slice(0, slash + 1)}</span>
      {name.slice(slash + 1)}
    </span>
  )
}

interface TagPickerProps {
  id?: string
  availableTags: string[] | undefined
  value: string[]
  onChange: (value: string[]) => void
}

export function TagPicker({ id, availableTags, value, onChange }: TagPickerProps) {
  const { t } = useTranslation("torrents")
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [pinned, setPinned] = useState<string[]>([])
  const [navigated, setNavigated] = useState(false)
  const [active, setActive] = useState("")

  const known = useMemo(() => new Set(availableTags ?? []), [availableTags])
  const selectedSet = new Set(value)
  const trimmed = query.trim()

  const ordered = useMemo(() => {
    const pinnedSet = new Set(pinned)
    const rest = [...known, ...value.filter(tag => !known.has(tag))]
      .filter(tag => !pinnedSet.has(tag))
      .sort(byName)
    return [...pinned, ...rest]
  }, [known, pinned, value])

  const typedTags = parseTypedTags(trimmed)
  const isList = typedTags.length > 1
  const searchTerm = isList ? trimmed : typedTags[0] ?? ""
  const visible = searchTerm ? ordered.filter(tag => matches(tag, searchTerm)) : ordered
  const canAdd = isList ? typedTags.some(tag => !selectedSet.has(tag)) : typedTags.length === 1 && !ordered.includes(typedTags[0])
  const shownChips = value.length > MAX_TAG_CHIPS ? value.slice(-(MAX_TAG_CHIPS - 1)) : value

  const handleOpenChange = (next: boolean) => {
    if (next) {
      setPinned(value)
      setQuery("")
      setNavigated(false)
    }
    setOpen(next)
  }

  const toggle = (tag: string) => {
    onChange(selectedSet.has(tag) ? value.filter(v => v !== tag) : [...value, tag])
  }

  const clearSearch = () => {
    setQuery("")
    setNavigated(false)
  }

  const addTyped = () => {
    onChange([...value, ...typedTags.filter(tag => !selectedSet.has(tag))])
    clearSearch()
  }

  return (
    <div className="space-y-2">
      <ResponsiveCommandPopover
        open={open}
        onOpenChange={handleOpenChange}
        title={value.length > 0 ? t("addTorrentDialog.options.tagsWithCount", { n: value.length }) : t("addTorrentDialog.options.tags")}
        popoverWidth={POPOVER_WIDTH}
        trigger={
          <Button
            id={id}
            type="button"
            variant="outline"
            aria-expanded={open}
            className="h-10 w-full justify-between font-normal text-muted-foreground"
          >
            <span className="flex items-center gap-2">
              <Tag className="h-4 w-4" />
              {t("addTorrentDialog.options.addTags")}
            </span>
            <span className="flex items-center gap-2">
              {value.length > 0 && (
                <span className="rounded-full bg-secondary px-2 text-xs text-secondary-foreground">{value.length}</span>
              )}
              <ChevronsUpDown className="h-4 w-4 shrink-0 opacity-50" />
            </span>
          </Button>
        }
      >
        <ResponsiveCommand shouldFilter={false} value={active} onValueChange={setActive}>
          <PickerDone
            onDone={() => {
              if (visible.length === 0 && canAdd) addTyped()
              handleOpenChange(false)
            }}
            onClear={value.length > 0 ? () => onChange([]) : undefined} />
          <ResponsiveCommandInput
            placeholder={t("addTorrentDialog.options.tagsPlaceholder")}
            value={query}
            onValueChange={(next) => {
              setQuery(next)
              setNavigated(false)
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") setNavigated(true)
              if (e.key !== "Enter" || e.nativeEvent.isComposing || e.keyCode === 229) return
              if (!trimmed) {
                if (!navigated) e.preventDefault()
                return
              }
              e.preventDefault()
              if (visible.length === 0) {
                if (canAdd) addTyped()
              } else if (visible.includes(active) && !selectedSet.has(active)) {
                onChange([...value, active])
                clearSearch()
              }
            }}
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
          />
          {canAdd && <AddTypedRow label={isList ? t("addTorrentDialog.options.addTypedTags", { tags: typedTags.join(", ") }) : t("addTorrentDialog.options.createTag", { tag: typedTags[0] })} onClick={addTyped} />}
          <ResponsiveCommandList>
            <PickerList>
              {visible.map(tag => (
                <PickerItem
                  searching={trimmed !== ""}
                  key={tag}
                  value={tag}
                  selected={selectedSet.has(tag)}
                  onSelect={() => toggle(tag)}
                  label={<TruncatedText openOnTap={false}>{tag}</TruncatedText>}
                  hint={known.has(tag) ? undefined : t("addTorrentDialog.options.newTag")}
                />
              ))}
            </PickerList>
            {!trimmed && (
              <ResponsiveCommandEmpty>
                {t("addTorrentDialog.options.noTagsYet")}
              </ResponsiveCommandEmpty>
            )}
          </ResponsiveCommandList>
        </ResponsiveCommand>
      </ResponsiveCommandPopover>
      {value.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {shownChips.map(tag => (
            <span
              key={tag}
              className="inline-flex h-8 max-w-full items-center rounded-md border bg-secondary pl-2.5 text-xs text-secondary-foreground sm:h-7 sm:pl-2"
            >
              <TruncatedText>{tag}</TruncatedText>
              {!known.has(tag) && (
                <span className="ml-1 text-[10px] text-muted-foreground">{t("addTorrentDialog.options.newTag")}</span>
              )}
              <button
                type="button"
                onClick={() => toggle(tag)}
                aria-label={t("addTorrentDialog.options.removeTag", { tag })}
                className="relative flex h-8 w-10 shrink-0 items-center justify-center rounded-r-md text-muted-foreground after:absolute after:-inset-y-1 after:inset-x-0 hover:bg-destructive/10 hover:text-destructive sm:h-7 sm:w-7 sm:after:hidden"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </span>
          ))}
          {shownChips.length < value.length && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 px-2.5 text-xs font-normal sm:h-7"
              onClick={() => handleOpenChange(true)}
            >
              {t("addTorrentDialog.options.moreTags", { count: value.length - shownChips.length })}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}

const keepEnterOnButton = (e: React.KeyboardEvent) => {
  if (e.key === "Enter") e.stopPropagation()
}

function AddTypedRow({ label, onClick }: { label: string; onClick: () => void }) {
  const isMobile = useResponsiveMobile()
  return (
    <button
      type="button"
      onClick={onClick}
      onKeyDown={keepEnterOnButton}
      className={cn(
        "flex w-full items-center gap-2 border-b text-left text-primary hover:bg-accent",
        isMobile ? "min-h-12 px-6 text-[15px]" : "px-3 py-2 text-sm"
      )}
    >
      <Plus className="h-4 w-4 shrink-0" />
      <span className="truncate">{label}</span>
    </button>
  )
}

function PickerList({ children }: { children: React.ReactNode }) {
  const isMobile = useResponsiveMobile()
  return <div className={cn(isMobile ? "px-2 py-1" : "p-1")}>{children}</div>
}

function PickerItem({ value, selected, onSelect, label, hint, searching = false }: {
  value: string
  selected?: boolean
  onSelect: () => void
  label: React.ReactNode
  hint?: string
  searching?: boolean
}) {
  const { t } = useTranslation("torrents")
  const isMobile = useResponsiveMobile()
  return (
    <ResponsiveCommandItem value={value} onSelect={onSelect} disableHighlight={isMobile && !searching} className="group gap-2">
      <span className={cn("flex shrink-0 items-center justify-center", isMobile ? "h-5 w-5" : "h-4 w-4")}>
        <Check className={cn("h-4 w-4 text-primary", selected ? "opacity-100" : "opacity-0")} />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        {label}
        {hint && <span className="truncate text-xs text-muted-foreground">{hint}</span>}
      </span>
      {selected && <span className="sr-only">{t("addTorrentDialog.options.selectedState")}</span>}
      {searching && (
        <kbd aria-hidden className="hidden shrink-0 rounded border px-1 font-sans text-[10px] text-muted-foreground group-data-[selected=true]:inline">↵</kbd>
      )}
    </ResponsiveCommandItem>
  )
}

function PickerDone({ onDone, onClear }: { onDone: () => void; onClear?: () => void }) {
  const { t } = useTranslation("torrents")
  const isMobile = useResponsiveMobile()
  if (!isMobile) return null
  return (
    <div className="flex items-center justify-end gap-2 border-b px-4 py-2">
      {onClear && (
        <Button type="button" variant="ghost" className="h-10 mr-auto px-3" onClick={onClear} onKeyDown={keepEnterOnButton}>
          {t("addTorrentDialog.options.clearAll")}
        </Button>
      )}
      <Button type="button" className="h-10 px-6" onClick={onDone} onKeyDown={keepEnterOnButton}>
        {t("addTorrentDialog.options.done")}
      </Button>
    </div>
  )
}
