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
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { api } from "@/lib/api"
import { useIncognitoMode } from "@/lib/incognito"
import { BYTES_PER_UNIT, unitLabel, type ByteUnit } from "@/lib/unit-format"
import { cn } from "@/lib/utils"
import type { PreferenceField } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useState, type ComponentType } from "react"
import { useTranslation } from "react-i18next"

import type { FieldLabels } from "./labels"
import { networkBindingKey } from "./preferenceView"

export interface WidgetProps {
  id: string
  instanceId: number
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
// A password box uses new-password, so that the browser does not fill in the password of qui.
function TextWidget({ id, field, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  const isPassword = field.control === "password"
  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <Input
        id={id}
        type={isPassword ? "password" : undefined}
        autoComplete={isPassword ? "new-password" : undefined}
        value={String(value ?? "")}
        disabled={disabled}
        placeholder={labels.placeholderKey ? t(labels.placeholderKey) : undefined}
        onChange={(e) => onChange(e.target.value)}
        className={incognitoMode ? "blur-sm select-none" : undefined}
      />
    </div>
  )
}

// qBittorrent 5.0 and later can list folders, so a folder field suggests them as the user types.
// A file field, such as the IP filter, has no option source and gets no suggestions. The same
// input stays mounted when the capabilities arrive, so a user who types early keeps the focus.
function PathWidget({ id, instanceId, field, labels, value, onChange, disabled }: WidgetProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const suggestsFolders = capabilities?.supportsPathAutocomplete && field.optionSource === "pathAutocomplete"
  const {
    suggestions,
    handleInputChange,
    handleSelect,
    handleKeyDown,
    handleBlur,
    highlightedIndex,
    showSuggestions,
    inputRef,
    listRef,
  } = usePathAutocomplete(onChange, instanceId)

  return (
    <div className="space-y-2">
      <FieldLabel id={id} labels={labels} />
      <Input
        ref={inputRef}
        id={id}
        autoComplete={suggestsFolders ? "off" : undefined}
        spellCheck={false}
        value={String(value ?? "")}
        disabled={disabled}
        placeholder={labels.placeholderKey ? t(labels.placeholderKey) : undefined}
        onChange={(e) => {
          onChange(e.target.value)
          if (suggestsFolders) handleInputChange(e.target.value)
        }}
        onKeyDown={handleKeyDown}
        onBlur={handleBlur}
        className={incognitoMode ? "blur-sm select-none" : undefined}
      />
      {showSuggestions && (
        <div className="relative">
          <div ref={listRef} className="absolute left-0 right-0 z-50 mt-1 rounded-md border bg-popover text-popover-foreground shadow-md">
            <div className="max-h-55 overflow-y-auto py-1">
              {suggestions.map((entry, index) => (
                <button
                  key={entry}
                  type="button"
                  title={incognitoMode ? undefined : entry}
                  className={cn(
                    "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                    highlightedIndex === index ? "bg-accent text-accent-foreground" : "hover:bg-accent/70",
                    incognitoMode && "blur-sm"
                  )}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => handleSelect(entry)}
                >
                  <span className="block truncate text-left">{entry}</span>
                </button>
              ))}
            </div>
          </div>
        </div>
      )}
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

// The address values that are not addresses of one interface.
const ALL_ADDRESSES = [
  { value: "", labelKey: "preferences.networkBinding.allAddresses" },
  { value: "0.0.0.0", labelKey: "preferences.networkBinding.allIPv4Addresses" },
  { value: "::", labelKey: "preferences.networkBinding.allIPv6Addresses" },
]

// Radix Select rejects an item with the value "", which is "Any interface" and "All addresses".
// So each item value gets a prefix.
const itemValue = (value: string) => `=${value}`

interface NetworkBindingProps {
  instanceId: number
  interfaceLabels: FieldLabels
  addressLabels: FieldLabels
  iface: string
  address: string
  // The readable name of the saved interface. qBittorrent before 4.6 does not send it.
  interfaceName: string
  onChange: (key: string, value: unknown) => void
}

// The network interface and its address. Until the network binding route answers, and when it
// fails, the widget shows the values as read-only text.
export function NetworkBindingWidget({ instanceId, interfaceLabels, addressLabels, iface, address, interfaceName, onChange }: NetworkBindingProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  const { data: interfaces, isError } = useQuery({
    queryKey: networkBindingKey(instanceId),
    queryFn: () => api.getNetworkBinding(instanceId),
    gcTime: 0,
  })
  const blur = incognitoMode ? "blur-sm select-none" : undefined
  const allAddresses = ALL_ADDRESSES.find(option => option.value === address)

  // A failed load after a save keeps the old list, so the error counts too.
  if (!interfaces || isError) {
    return (
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <FieldLabel id="preference-current_network_interface" labels={interfaceLabels} />
          <Input
            id="preference-current_network_interface"
            value={iface === "" ? t("preferences.networkBinding.anyInterface") : interfaceName || iface}
            disabled
            className={blur}
          />
        </div>
        <div className="space-y-2">
          <FieldLabel id="preference-current_interface_address" labels={addressLabels} />
          <Input
            id="preference-current_interface_address"
            value={allAddresses ? t(allAddresses.labelKey) : address}
            disabled
            className={blur}
          />
        </div>
      </div>
    )
  }

  const notAvailable = <span className="text-muted-foreground"> {t("preferences.networkBinding.notAvailable")}</span>
  const addresses = interfaces.find(option => option.value === iface)?.addresses ?? []

  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <div className="space-y-2">
        <FieldLabel id="preference-current_network_interface" labels={interfaceLabels} />
        <Select
          value={itemValue(iface)}
          onValueChange={(next) => {
            onChange("current_network_interface", next.slice(1))
            // An address of the old interface does not exist on the new one.
            onChange("current_interface_address", "")
          }}
        >
          <SelectTrigger id="preference-current_network_interface" className={cn("w-full", blur)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {interfaces.map(option => (
              <SelectItem key={option.value} value={itemValue(option.value)} className={blur}>
                {option.value === "" ? t("preferences.networkBinding.anyInterface") : option.name}
                {!option.available && notAvailable}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <FieldLabel id="preference-current_interface_address" labels={addressLabels} />
        <Select value={itemValue(address)} onValueChange={(next) => onChange("current_interface_address", next.slice(1))}>
          <SelectTrigger id="preference-current_interface_address" className={cn("w-full", blur)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ALL_ADDRESSES.map(option => (
              <SelectItem key={option.value} value={itemValue(option.value)}>{t(option.labelKey)}</SelectItem>
            ))}
            {addresses.map(option => (
              <SelectItem key={option.address} value={itemValue(option.address)} className={blur}>
                {option.address}
                {!option.available && notAvailable}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const preferenceWidgets: Record<PreferenceField["control"], ComponentType<WidgetProps>> = {
  switch: SwitchWidget,
  number: NumberWidget,
  text: TextWidget,
  password: TextWidget,
  path: PathWidget,
  select: SelectWidget,
  list: ListWidget,
  watchFolders: WatchFoldersWidget,
}
