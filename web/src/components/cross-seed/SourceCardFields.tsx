/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { normalizeStringList } from "@/components/cross-seed/cross-seed-settings"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import { Switch } from "@/components/ui/switch"
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
