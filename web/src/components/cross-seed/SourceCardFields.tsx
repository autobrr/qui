/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import { normalizeStringList } from "@/lib/cross-seed-utils"
import type { Category } from "@/types"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"

interface SourceTagsFieldProps {
  id: string
  /** Suggested tags beside the shared "cross-seed" default; the user can still type any tag. */
  suggestions: Array<{ label: string; value: string }>
  selected: string[]
  onChange: (tags: string[]) => void
  placeholder: string
  help?: string
  className?: string
}

/** The "Cross-seed tags" picker every source card carries. */
export function SourceTagsField({ id, suggestions, selected, onChange, placeholder, help, className }: SourceTagsFieldProps) {
  const { t } = useTranslation("crossseed")
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-1.5">
        <Label htmlFor={id}>{t("sourceCard.crossSeedTags")}</Label>
        {help && <FieldHelp>{help}</FieldHelp>}
      </div>
      <MultiSelect
        id={id}
        options={[{ label: t("rules.tagging.tagCrossSeed"), value: "cross-seed" }, ...suggestions]}
        selected={selected}
        onChange={values => onChange(normalizeStringList(values))}
        placeholder={placeholder}
        className={className}
        creatable
        onCreateOption={value => onChange(normalizeStringList([...selected, value]))}
      />
    </div>
  )
}

interface AutoResumeSwitchProps {
  id: string
  skip: boolean
  onSkipChange: (skip: boolean) => void
  /** Source-specific note appended to the shared help text. */
  help?: string
}

/** The "Auto-resume after injection" switch; the stored field is the inverse (skip). */
export function AutoResumeSwitch({ id, skip, onSkipChange, help }: AutoResumeSwitchProps) {
  const { t } = useTranslation("crossseed")
  return (
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-1.5">
        <Label htmlFor={id} className="font-medium">{t("sourceCard.autoResume")}</Label>
        <FieldHelp>{t("sourceCard.autoResumeHelp")}{help ? ` ${help}` : ""}</FieldHelp>
      </div>
      <Switch id={id} checked={!skip} onCheckedChange={value => onSkipChange(!value)} />
    </div>
  )
}

interface SourceFilters {
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
}

interface SourceFilterFieldsProps {
  filters: SourceFilters
  onChange: (filters: SourceFilters) => void
  metadata?: { categories: Record<string, Category>; tags: string[] }
  /** Greys the pickers out until the source has instances to load categories and tags from. */
  disabled?: boolean
}

/** The include/exclude category and tag pickers a source card filters its torrents with. */
export function SourceFilterFields({ filters, onChange, metadata, disabled = false }: SourceFilterFieldsProps) {
  const { t } = useTranslation("crossseed")

  const categoryOptions = useMemo(
    () => buildCategorySelectOptions(metadata?.categories ?? {}, filters.categories, filters.excludeCategories),
    [metadata?.categories, filters.categories, filters.excludeCategories]
  )
  const tagOptions = useMemo(
    () => buildTagSelectOptions(metadata?.tags ?? [], filters.tags, filters.excludeTags),
    [metadata?.tags, filters.tags, filters.excludeTags]
  )

  const categoryPlaceholder = (empty: string) =>
    disabled ? t("automation.selectInstancesToLoadCategories") : categoryOptions.length ? empty : t("automation.typeToAddCategories")
  const tagPlaceholder = (empty: string) =>
    disabled ? t("automation.selectInstancesToLoadTags") : tagOptions.length ? empty : t("automation.typeToAddTags")

  return (
    <>
      <div className="grid gap-4 md:grid-cols-2">
        <FilterPicker
          label={t("automation.includeCategories")}
          options={categoryOptions}
          selected={filters.categories}
          onChange={categories => onChange({ ...filters, categories })}
          placeholder={categoryPlaceholder(t("automation.allCategories"))}
          summary={filters.categories.length === 0 ? t("automation.allCategoriesIncluded") : t("automation.selectedCategoriesMatched", { count: filters.categories.length })}
          disabled={disabled}
        />
        <FilterPicker
          label={t("automation.includeTags")}
          options={tagOptions}
          selected={filters.tags}
          onChange={tags => onChange({ ...filters, tags })}
          placeholder={tagPlaceholder(t("automation.allTags"))}
          summary={filters.tags.length === 0 ? t("automation.allTagsIncluded") : t("automation.selectedTagsMatched", { count: filters.tags.length })}
          disabled={disabled}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <FilterPicker
          label={t("automation.excludeCategories")}
          options={categoryOptions}
          selected={filters.excludeCategories}
          onChange={excludeCategories => onChange({ ...filters, excludeCategories })}
          placeholder={categoryPlaceholder(t("automation.none"))}
          summary={filters.excludeCategories.length === 0 ? t("automation.noCategoriesExcluded") : t("automation.categoriesSkipped", { count: filters.excludeCategories.length })}
          disabled={disabled}
        />
        <FilterPicker
          label={t("automation.excludeTags")}
          options={tagOptions}
          selected={filters.excludeTags}
          onChange={excludeTags => onChange({ ...filters, excludeTags })}
          placeholder={tagPlaceholder(t("automation.none"))}
          summary={filters.excludeTags.length === 0 ? t("automation.noTagsExcluded") : t("automation.tagsSkipped", { count: filters.excludeTags.length })}
          disabled={disabled}
        />
      </div>
    </>
  )
}

interface FilterPickerProps {
  label: string
  options: Array<{ label: string; value: string }>
  selected: string[]
  onChange: (values: string[]) => void
  placeholder: string
  summary: string
  disabled: boolean
}

function FilterPicker({ label, options, selected, onChange, placeholder, summary, disabled }: FilterPickerProps) {
  return (
    <div className="space-y-3">
      <Label>{label}</Label>
      <MultiSelect options={options} selected={selected} onChange={onChange} placeholder={placeholder} creatable disabled={disabled} />
      <p className="text-xs text-muted-foreground">{summary}</p>
    </div>
  )
}
