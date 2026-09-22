/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { MultiSelect, type Option } from "@/components/ui/multi-select"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import type { CategoryMappingRule } from "@/types"
import { Plus, X } from "lucide-react"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"

// Content types a rule may force (mirrors the backend list).
const CONTENT_TYPE_OPTIONS = ["movie", "tv", "music", "audiobook", "book", "comic", "game", "app"] as const

interface CategoryMappingRulesEditorProps {
  value: CategoryMappingRule[]
  onChange: (rules: CategoryMappingRule[]) => void
  /** Aggregated qBittorrent category metadata used to suggest categories. */
  categoryMetadata: Record<string, { name: string; savePath: string }>
}

export function CategoryMappingRulesEditor({
  value,
  onChange,
  categoryMetadata,
}: CategoryMappingRulesEditorProps) {
  const { t } = useTranslation("crossseed")

  const categoryOptions = useMemo<Option[]>(
    () => buildCategorySelectOptions(categoryMetadata, value.flatMap(rule => rule.categories)),
    [categoryMetadata, value]
  )

  const updateRule = (index: number, patch: Partial<CategoryMappingRule>) => {
    onChange(value.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)))
  }

  const removeRule = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  const addRule = () => {
    onChange([...value, { categories: [], contentType: "music" }])
  }

  return (
    <div className="space-y-3">
      <div className="space-y-2">
        {value.map((rule, index) => (
          <div key={index} className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-muted-foreground">{t("rules.matching.categoryMapping.whenCategory")}</span>
            <MultiSelect
              options={categoryOptions}
              selected={rule.categories}
              onChange={categories => updateRule(index, { categories })}
              onCreateOption={category => updateRule(index, { categories: [...rule.categories, category] })}
              placeholder={t("rules.categories.selectOrTypeCategory")}
              className="w-[240px]"
              creatable
            />

            <span className="text-xs text-muted-foreground">{t("rules.matching.categoryMapping.searchAs")}</span>
            <Select
              value={rule.contentType}
              onValueChange={contentType => updateRule(index, { contentType })}
            >
              <SelectTrigger className="h-9 w-[140px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {CONTENT_TYPE_OPTIONS.map(contentType => (
                  <SelectItem key={contentType} value={contentType}>
                    {t(`dirScan.contentTypeLabels.${contentType}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-9 w-9 shrink-0"
              onClick={() => removeRule(index)}
              aria-label={t("rules.matching.categoryMapping.removeRule")}
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
      >
        <Plus className="h-4 w-4" />
        {t("rules.matching.categoryMapping.addRule")}
      </Button>
    </div>
  )
}
