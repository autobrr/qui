/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SaveFooter } from "@/components/ui/save-footer"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Switch } from "@/components/ui/switch"
import {
  useActiveInstances,
  useAggregatedInstanceMetadata,
  usePatchCrossSeedSettings
} from "@/hooks/useCrossSeedSettings"
import { buildCategorySelectOptions } from "@/lib/category-utils"
import type { CrossSeedAutomationSettings, CrossSeedAutomationSettingsPatch } from "@/types"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

type CategoryMode = "reuse" | "affix" | "indexer" | "custom"

interface CategoriesFormState {
  categoryMode: CategoryMode
  categoryAffixMode: "prefix" | "suffix"
  categoryAffix: string
  customCategory: string
  inheritSourceTags: boolean
}

// Exactly one category mode is active; priority when the flags disagree: custom > indexer > affix > reuse.
function categoryModeOf(settings: CrossSeedAutomationSettings): CategoryMode {
  if (settings.useCustomCategory) return "custom"
  if (settings.useCategoryFromIndexer) return "indexer"
  if (settings.useCrossCategoryAffix) return "affix"
  return "reuse"
}

function categoriesPatch({ categoryMode, ...rest }: CategoriesFormState): CrossSeedAutomationSettingsPatch {
  return {
    ...rest,
    useCrossCategoryAffix: categoryMode === "affix",
    useCategoryFromIndexer: categoryMode === "indexer",
    useCustomCategory: categoryMode === "custom",
  }
}

export function CategoriesTab({ settings }: { settings: CrossSeedAutomationSettings }) {
  const { t } = useTranslation("crossseed")
  const patchSettings = usePatchCrossSeedSettings()
  const { activeInstanceIds } = useActiveInstances()
  const { data: metadata } = useAggregatedInstanceMetadata(activeInstanceIds)

  const [form, setForm] = useState<CategoriesFormState>(() => ({
    categoryMode: categoryModeOf(settings),
    categoryAffixMode: settings.categoryAffixMode,
    categoryAffix: settings.categoryAffix,
    customCategory: settings.customCategory ?? "",
    inheritSourceTags: settings.inheritSourceTags,
  }))
  const [customCategoryError, setCustomCategoryError] = useState("")

  const customCategorySelectOptions = useMemo(
    () => buildCategorySelectOptions(metadata?.categories ?? {}, form.customCategory ? [form.customCategory] : []),
    [form.customCategory, metadata?.categories]
  )

  const handleSave = () => {
    setCustomCategoryError("")
    if (form.categoryMode === "custom" && !form.customCategory.trim()) {
      setCustomCategoryError(t("toast.customCategoryRequired"))
      return
    }
    patchSettings.mutate(categoriesPatch(form))
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("tabs.categories")}</CardTitle>
        <CardDescription>{t("rules.tabDescriptions.categories")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Categories */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.categories.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.categories.description")}</p>
          </div>
          <RadioGroup
            value={form.categoryMode}
            onValueChange={(value) => setForm(prev => ({ ...prev, categoryMode: value as CategoryMode }))}
            className="space-y-3"
          >
            <div className="flex items-start gap-3">
              <RadioGroupItem value="reuse" id="category-reuse" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-reuse" className="font-medium cursor-pointer">{t("rules.categories.reuseCategory")}</Label>
                  <FieldHelp>{t("rules.categories.reuseCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.reuseCategoryDescription")}</p>
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="affix" id="category-affix" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-affix" className="font-medium cursor-pointer">{t("rules.categories.categoryAffix")}</Label>
                  <FieldHelp>{t("rules.categories.categoryAffixHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.categoryAffixDescription")}</p>
                {form.categoryMode === "affix" && (
                  <div className="flex flex-wrap items-center gap-3 mt-2">
                    <div className="inline-flex h-9 items-center justify-center rounded-lg bg-muted p-1 text-muted-foreground">
                      <button
                        type="button"
                        onClick={() => setForm(prev => ({ ...prev, categoryAffixMode: "prefix" }))}
                        className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium transition-all ${form.categoryAffixMode === "prefix" ? "bg-background text-primary shadow-sm" : "hover:bg-background/50 hover:text-foreground"}`}
                      >
                        {t("rules.categories.prefix")}
                      </button>
                      <button
                        type="button"
                        onClick={() => setForm(prev => ({ ...prev, categoryAffixMode: "suffix" }))}
                        className={`inline-flex items-center justify-center whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium transition-all ${form.categoryAffixMode === "suffix" ? "bg-background text-primary shadow-sm" : "hover:bg-background/50 hover:text-foreground"}`}
                      >
                        {t("rules.categories.suffix")}
                      </button>
                    </div>
                    <Input
                      value={form.categoryAffix}
                      onChange={e => setForm(prev => ({ ...prev, categoryAffix: e.target.value }))}
                      placeholder={form.categoryAffixMode === "prefix" ? "cross-seed/" : ".cross"}
                      className="max-w-[140px] h-9"
                    />
                  </div>
                )}
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="indexer" id="category-indexer" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-indexer" className="font-medium cursor-pointer">{t("rules.categories.indexerCategory")}</Label>
                  <FieldHelp>{t("rules.categories.indexerCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.indexerCategoryDescription")}</p>
              </div>
            </div>
            <div className="flex items-start gap-3">
              <RadioGroupItem value="custom" id="category-custom" className="mt-0.5" />
              <div className="space-y-0.5 flex-1">
                <div className="flex items-center gap-1.5">
                  <Label htmlFor="category-custom" className="font-medium cursor-pointer">{t("rules.categories.customCategory")}</Label>
                  <FieldHelp>{t("rules.categories.customCategoryHelp")}</FieldHelp>
                </div>
                <p className="text-xs text-muted-foreground">{t("rules.categories.customCategoryDescription")}</p>
                {form.categoryMode === "custom" && (
                  <>
                    <MultiSelect
                      options={customCategorySelectOptions}
                      selected={form.customCategory ? [form.customCategory] : []}
                      onChange={values => {
                        setForm(prev => ({ ...prev, customCategory: values[0] ?? "" }))
                        setCustomCategoryError("")
                      }}
                      placeholder={t("rules.categories.selectOrTypeCategory")}
                      className={`mt-2 max-w-xs ${customCategoryError ? "border-destructive" : ""}`}
                      creatable
                      single
                      onCreateOption={value => {
                        setForm(prev => ({ ...prev, customCategory: value }))
                        setCustomCategoryError("")
                      }}
                    />
                    {customCategoryError && (
                      <p className="text-sm text-destructive">{customCategoryError}</p>
                    )}
                  </>
                )}
              </div>
            </div>
          </RadioGroup>
        </div>

        {/* Tagging */}
        <div className="rounded-lg border border-border/70 bg-muted/40 p-4 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium leading-none">{t("rules.tagging.title")}</p>
            <p className="text-xs text-muted-foreground">{t("rules.tagging.description")}</p>
          </div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="inherit-source-tags" className="font-medium">{t("rules.tagging.inheritSourceTags")}</Label>
              <FieldHelp>{t("rules.tagging.inheritSourceTagsDescription")}</FieldHelp>
            </div>
            <Switch
              id="inherit-source-tags"
              checked={form.inheritSourceTags}
              onCheckedChange={value => setForm(prev => ({ ...prev, inheritSourceTags: !!value }))}
            />
          </div>
        </div>
      </CardContent>
      <SaveFooter pending={patchSettings.isPending} onSave={handleSave} label={t("rules.saveChanges")} />
    </Card>
  )
}
