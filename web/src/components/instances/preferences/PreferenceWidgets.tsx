/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useIncognitoMode } from "@/lib/incognito"
import { BYTES_PER_UNIT, unitLabel, type ByteUnit } from "@/lib/unit-format"
import type { PreferenceField } from "@/types"
import { useState, type ComponentType } from "react"
import { useTranslation } from "react-i18next"

import type { FieldLabels } from "./labels"

export interface WidgetProps {
  id: string
  field: PreferenceField
  labels: FieldLabels
  value: unknown
  onChange: (value: unknown) => void
  disabled: boolean
}

function FieldLabel({ id, labels }: { id?: string; labels: FieldLabels }) {
  const { t } = useTranslation("instances")
  return (
    <Label htmlFor={id} className="flex items-center gap-2 text-sm font-medium">
      {t(labels.labelKey)}
      {labels.descriptionKey && <FieldHelp>{t(labels.descriptionKey)}</FieldHelp>}
    </Label>
  )
}

function isByteUnit(unit: string): unit is ByteUnit {
  return unit in BYTES_PER_UNIT
}

// "KiB/s" -> "KiB"
const withoutPerSecond = (unit: string) => unit.replace(/\/s$/, "")

// The factor from the wire unit to the display unit, for example 1/1024 from B/s to KiB/s.
function displayScale(field: PreferenceField): number {
  const wire = withoutPerSecond(field.wireUnit ?? "")
  const display = withoutPerSecond(field.displayUnit ?? "")
  return isByteUnit(wire) && isByteUnit(display) ? BYTES_PER_UNIT[wire] / BYTES_PER_UNIT[display] : 1
}

function useUnitLabel(unit: string | undefined): string | undefined {
  const { t } = useTranslation("instances")
  if (!unit) return undefined
  const base = withoutPerSecond(unit)
  if (isByteUnit(base)) return unitLabel(base, base !== unit)
  switch (unit) {
    case "s": return t("preferences.advancedNetwork.unitSeconds")
    case "min": return t("preferences.units.minutes")
    case "%": return "%"
    default: return undefined
  }
}

function SwitchWidget({ id, labels, value, onChange, disabled }: WidgetProps) {
  return (
    <div className="flex items-center gap-3">
      <Switch id={id} checked={value === true} onCheckedChange={onChange} disabled={disabled} />
      <FieldLabel id={id} labels={labels} />
    </div>
  )
}

function NumberWidget({ id, field, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const unit = useUnitLabel(field.displayUnit)
  const scale = displayScale(field)
  // The text while the user types, so that a partial number such as "1." stays in the box.
  const [text, setText] = useState<string | null>(null)
  const isUnlimited = labels.unlimited !== undefined && value === labels.unlimited
  // A converted value keeps two decimals: 1500 B/s shows as 1.46 KiB/s.
  const shown = text ?? (isUnlimited ? "" : String(scale === 1 ? Number(value) : Number((Number(value) * scale).toFixed(2))))

  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <div className="flex items-center gap-2">
        <Input
          id={id}
          type="number"
          step="any"
          value={shown}
          disabled={disabled}
          placeholder={labels.unlimited !== undefined ? t("common:numberInput.unlimited") : undefined}
          onChange={(e) => {
            const next = e.target.value
            setText(next)
            if (next === "") {
              if (labels.unlimited !== undefined) onChange(labels.unlimited)
              return
            }
            const parsed = Number(next)
            if (Number.isFinite(parsed)) {
              onChange(scale === 1 ? parsed : Math.round(parsed / scale))
            }
          }}
          onBlur={() => setText(null)}
        />
        {unit && <span className="min-w-12 text-sm text-muted-foreground">{unit}</span>}
      </div>
    </div>
  )
}

// Incognito mode blurs text, paths, and lists: they can hold paths, addresses, and commands.
function TextWidget({ id, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <Input
        id={id}
        value={String(value ?? "")}
        disabled={disabled}
        placeholder={labels.placeholderKey ? t(labels.placeholderKey) : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={incognitoMode ? "blur-sm select-none" : undefined}
      />
    </div>
  )
}

function ListWidget({ id, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <Textarea
        id={id}
        rows={4}
        value={String(value ?? "")}
        disabled={disabled}
        placeholder={labels.placeholderKey ? t(labels.placeholderKey) : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={incognitoMode ? "font-mono blur-sm select-none" : "font-mono"}
      />
    </div>
  )
}

function SelectWidget({ id, field, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const options = field.options ?? []
  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <Select
        value={String(value)}
        disabled={disabled}
        onValueChange={(next) => onChange(options.find(option => String(option) === next))}
      >
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {options.map(option => {
            const optionLabel = labels.options?.[String(option)]
            return (
              <SelectItem key={String(option)} value={String(option)}>
                {optionLabel ? t(optionLabel.labelKey) : t("preferences.fields.optionValue", { value: String(option) })}
              </SelectItem>
            )
          })}
        </SelectContent>
      </Select>
    </div>
  )
}

// scan_dirs maps each watched folder to 0 (save to the watched folder), 1 (save to the
// default save path), or a path to save to.
const SAVE_TO_WATCHED_FOLDER = 0
const SAVE_TO_DEFAULT_PATH = 1
type WatchFolderDestination = "monitored-folder" | "default-save-location" | "other"
interface WatchFolder {
  path: string
  destination: WatchFolderDestination
  otherPath: string
}

function watchFoldersOf(scanDirs: unknown): WatchFolder[] {
  if (!scanDirs || typeof scanDirs !== "object") return []
  return Object.entries(scanDirs).map(([path, target]) => {
    if (typeof target === "string") return { path, destination: "other", otherPath: target }
    if (target === SAVE_TO_DEFAULT_PATH) return { path, destination: "default-save-location", otherPath: "" }
    return { path, destination: "monitored-folder", otherPath: "" }
  })
}

function scanDirsOf(folders: WatchFolder[]): Record<string, number | string> {
  const scanDirs: Record<string, number | string> = {}
  for (const folder of folders) {
    const path = folder.path.trim()
    if (!path) continue
    scanDirs[path] = folder.destination === "default-save-location" ? SAVE_TO_DEFAULT_PATH : folder.destination === "other" ? folder.otherPath : SAVE_TO_WATCHED_FOLDER
  }
  return scanDirs
}

function WatchFoldersWidget({ labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  // The rows live here, so that a row with an empty path stays while the user fills it in.
  const [folders, setFolders] = useState(() => watchFoldersOf(value))
  const update = (next: WatchFolder[]) => {
    setFolders(next)
    onChange(scanDirsOf(next))
  }
  const updateRow = (index: number, change: Partial<WatchFolder>) =>
    update(folders.map((folder, i) => i === index ? { ...folder, ...change } : folder))
  const blur = incognitoMode ? "blur-sm select-none" : undefined

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        {/* The label points at no control: a click on it must not add a folder. */}
        <FieldLabel labels={labels} />
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          onClick={() => update([...folders, { path: "", destination: "default-save-location", otherPath: "" }])}
        >
          {t("preferences.fileManagement.watchFolders.addFolder")}
        </Button>
      </div>

      {folders.length === 0 && (
        <p className="text-xs text-muted-foreground">{t("preferences.fileManagement.watchFolders.noFolders")}</p>
      )}

      {folders.map((folder, index) => (
        <div key={index} className="space-y-3 rounded-md border p-3">
          <div className="grid grid-cols-1 items-start gap-3 md:grid-cols-2">
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("preferences.fileManagement.watchFolders.monitoredFolderLabel")}</Label>
              <Input
                value={folder.path}
                disabled={disabled}
                onChange={(e) => updateRow(index, { path: e.target.value })}
                placeholder={t("preferences.fileManagement.watchFolders.monitoredFolderPlaceholder")}
                className={blur}
              />
            </div>
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("preferences.fileManagement.watchFolders.destinationLabel")}</Label>
              <Select
                value={folder.destination}
                disabled={disabled || !folder.path}
                onValueChange={(destination) => updateRow(index, { destination: destination as WatchFolderDestination })}
              >
                <SelectTrigger>
                  <SelectValue placeholder={t("preferences.fileManagement.watchFolders.selectDestination")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="monitored-folder">{t("preferences.fileManagement.watchFolders.destinationMonitored")}</SelectItem>
                  <SelectItem value="default-save-location">{t("preferences.fileManagement.watchFolders.destinationDefault")}</SelectItem>
                  <SelectItem value="other">{t("preferences.fileManagement.watchFolders.destinationOther")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {folder.destination === "other" && (
            <div className="space-y-2">
              <Label className="text-sm font-medium">{t("preferences.fileManagement.watchFolders.customSavePathLabel")}</Label>
              <Input
                value={folder.otherPath}
                disabled={disabled || !folder.path}
                onChange={(e) => updateRow(index, { otherPath: e.target.value })}
                placeholder={t("preferences.fileManagement.watchFolders.customSavePathPlaceholder")}
                className={blur}
              />
            </div>
          )}

          <div className="flex justify-end">
            <Button type="button" variant="ghost" disabled={disabled} onClick={() => update(folders.filter((_, i) => i !== index))}>
              {t("preferences.fileManagement.watchFolders.remove")}
            </Button>
          </div>
        </div>
      ))}
    </div>
  )
}

export const preferenceWidgets: Record<PreferenceField["control"], ComponentType<WidgetProps>> = {
  switch: SwitchWidget,
  number: NumberWidget,
  text: TextWidget,
  path: TextWidget,
  select: SelectWidget,
  list: ListWidget,
  watchFolders: WatchFoldersWidget,
}
