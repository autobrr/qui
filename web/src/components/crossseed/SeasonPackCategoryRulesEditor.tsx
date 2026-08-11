/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import type { SeasonPackCategoryRule } from "@/types"
import { Plus, X } from "lucide-react"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"

// Resolution options for the routing rules (canonical lowercase rls values).
const RESOLUTION_OPTIONS = ["2160p", "1080p", "720p", "576p", "480p"] as const

// Source options. Stored value "" means any source; the rest are canonical uppercase.
const SOURCE_OPTIONS: Array<{ value: string; labelKey: string }> = [
  { value: "", labelKey: "rules.seasonPack.categoryRouting.anySource" },
  { value: "WEB", labelKey: "rules.seasonPack.categoryRouting.source.web" },
  { value: "BLURAY", labelKey: "rules.seasonPack.categoryRouting.source.bluray" },
  { value: "REMUX", labelKey: "rules.seasonPack.categoryRouting.source.remux" },
  { value: "HDTV", labelKey: "rules.seasonPack.categoryRouting.source.hdtv" },
]

// Select uses a non-empty sentinel for the "any source" entry because Radix
// Select reserves "" for clearing the value.
const ANY_SOURCE_VALUE = "__any__"

interface SeasonPackCategoryRulesEditorProps {
  value: SeasonPackCategoryRule[]
  onChange: (rules: SeasonPackCategoryRule[]) => void
  /** Aggregated qBittorrent category metadata used to suggest categories. */
  categoryMetadata: Record<string, { name: string; savePath: string }>
  disabled?: boolean
}

export function SeasonPackCategoryRulesEditor({
  value,
  onChange,
  categoryMetadata,
  disabled = false,
}: SeasonPackCategoryRulesEditorProps) {
  const { t } = useTranslation("crossseed")

  const categoryOptions = useMemo<Option[]>(
    () => buildCategorySelectOptions(categoryMetadata, value.map(rule => rule.category)),
    [categoryMetadata, value]
  )

  const updateRule = (index: number, patch: Partial<SeasonPackCategoryRule>) => {
    onChange(value.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)))
  }

  const removeRule = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  const addRule = () => {
    onChange([...value, { resolution: "1080p", source: "", category: "" }])
  }

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <p className="text-sm font-medium leading-none">{t("rules.seasonPack.categoryRouting.title")}</p>
        <p className="text-xs text-muted-foreground">{t("rules.seasonPack.categoryRouting.description")}</p>
      </div>

      <div className="space-y-2">
        {value.map((rule, index) => (
          <div key={index} className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">{t("rules.seasonPack.categoryRouting.whenResolution")}</span>
            <Select
              value={rule.resolution}
              onValueChange={resolution => updateRule(index, { resolution })}
              disabled={disabled}
            >
              <SelectTrigger className="h-9 w-[110px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {RESOLUTION_OPTIONS.map(resolution => (
                  <SelectItem key={resolution} value={resolution}>{resolution}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            <span className="text-xs text-muted-foreground">{t("rules.seasonPack.categoryRouting.fromSource")}</span>
            <Select
              value={rule.source === "" ? ANY_SOURCE_VALUE : rule.source}
              onValueChange={source => updateRule(index, { source: source === ANY_SOURCE_VALUE ? "" : source })}
              disabled={disabled}
            >
              <SelectTrigger className="h-9 w-[130px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SOURCE_OPTIONS.map(option => (
                  <SelectItem key={option.value || ANY_SOURCE_VALUE} value={option.value || ANY_SOURCE_VALUE}>
                    {t(option.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <span className="text-xs text-muted-foreground">{t("rules.seasonPack.categoryRouting.fileAs")}</span>
            <MultiSelect
              options={categoryOptions}
              selected={rule.category ? [rule.category] : []}
              onChange={values => updateRule(index, { category: values[0] ?? "" })}
              onCreateOption={category => updateRule(index, { category })}
              placeholder={t("rules.categories.selectOrTypeCategory")}
              className="w-[180px]"
              creatable
              disabled={disabled}
            />

            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-9 w-9 shrink-0"
              onClick={() => removeRule(index)}
              disabled={disabled}
              aria-label={t("rules.seasonPack.categoryRouting.removeRule")}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        ))}
      </div>

      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={addRule}
        disabled={disabled}
      >
        <Plus className="h-4 w-4" />
        {t("rules.seasonPack.categoryRouting.addRule")}
      </Button>
    </div>
  )
}
