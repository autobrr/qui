/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { TabsContent } from "@/components/ui/tabs"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { formatErrorMessage } from "@/lib/format-error-message"
import type { AppPreferences, PreferenceField } from "@/types"
import { useQueryClient } from "@tanstack/react-query"
import { memo, useCallback, useState, type FormEvent } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"

import { preferenceTabLabels } from "./labels"
import { labelsOf, preferenceTabsOf, preferenceViewKey } from "./preferenceView"
import { PreferencesFormShell } from "./PreferencesFormShell"
import { preferenceWidgets } from "./PreferenceWidgets"

const valuesOf = (fields: PreferenceField[]) => Object.fromEntries(fields.map(field => [field.key, field.value]))

const WIDE_CONTROLS = new Set<PreferenceField["control"]>(["switch", "list", "watchFolders"])

interface PreferencesFormProps {
  instanceId: number
  fields: PreferenceField[]
}

// The qBittorrent tabs of the preferences dialog, drawn from the catalog view. All tabs share
// one form: a save from any tab sends every changed key.
export function PreferencesForm({ instanceId, fields }: PreferencesFormProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()
  const { updatePreferences, isUpdating } = useInstancePreferences(instanceId, { fetchIfMissing: false })
  // The values that qBittorrent has. They do not follow a refresh of the view, so that a
  // refresh does not reset open edits.
  const [saved, setSaved] = useState(() => valuesOf(fields))
  const [values, setValues] = useState(saved)

  const setValue = useCallback((key: string, value: unknown) => setValues(previous => ({ ...previous, [key]: value })), [])

  const byKey = new Map(fields.map(field => [field.key, field]))
  const valueOf = (field: PreferenceField) => field.key in values ? values[field.key] : field.value
  const isEnabled = (field: PreferenceField): boolean => {
    const parent = field.dependsOn ? byKey.get(field.dependsOn) : undefined
    return !parent || (valueOf(parent) === true && isEnabled(parent))
  }

  const save = async (event: FormEvent) => {
    event.preventDefault()
    const changed = Object.fromEntries(Object.entries(values).filter(
      ([key, value]) => JSON.stringify(value) !== JSON.stringify(saved[key])
    ))
    if (Object.keys(changed).length > 0) {
      try {
        await updatePreferences(changed as Partial<AppPreferences>)
      } catch (error) {
        console.error("Failed to save preferences:", error)
        // The server names the setting that it rejected, for example a decimal in a whole-number field.
        toast.error(t("preferences.toast.saveFailed"), {
          description: error instanceof Error ? formatErrorMessage(error.message) : undefined,
        })
        return
      }
      setSaved(previous => ({ ...previous, ...changed }))
      void queryClient.invalidateQueries({ queryKey: preferenceViewKey(instanceId) })
    }
    toast.success(t("preferences.toast.saved"))
  }

  return preferenceTabsOf(fields).map(tab => {
    const tabLabels = preferenceTabLabels[tab]
    const sections = new Map<string, PreferenceField[]>()
    for (const field of fields) {
      if (field.tab === tab && labelsOf(field)) {
        sections.set(field.section, [...sections.get(field.section) ?? [], field])
      }
    }

    return (
      <TabsContent key={tab} value={tab} className="mt-6 flex min-h-0 flex-1 flex-col overflow-hidden">
        <PreferencesFormShell
          onSubmit={save}
          footer={(
            <Button type="submit" disabled={isUpdating} className="min-w-32">
              {isUpdating ? t("preferences.common.saving") : t("preferences.common.saveChanges")}
            </Button>
          )}
        >
          <div className="space-y-8">
            {[...sections].map(([section, sectionFields]) => {
              const sectionLabels = tabLabels.sections[section]
              return (
                <section key={section} className="space-y-4">
                  {sectionLabels && (
                    <div className="space-y-1">
                      <h3 className="text-base font-medium">{t(sectionLabels.labelKey)}</h3>
                      {sectionLabels.descriptionKey && (
                        <p className="text-sm text-muted-foreground">
                          <Trans ns="instances" i18nKey={sectionLabels.descriptionKey} components={{ code: <code className="font-mono" /> }} />
                        </p>
                      )}
                    </div>
                  )}
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    {sectionFields.map(field => (
                      <PreferenceRow
                        key={field.key}
                        field={field}
                        value={valueOf(field)}
                        disabled={field.readOnly === true || !isEnabled(field)}
                        onChange={setValue}
                      />
                    ))}
                  </div>
                  {section === "runExternalProgram" && (
                    <AutorunPlaceholders supportsTorrentAdded={byKey.has("autorun_on_torrent_added_enabled")} />
                  )}
                </section>
              )
            })}
          </div>
        </PreferencesFormShell>
      </TabsContent>
    )
  })
}

interface PreferenceRowProps {
  field: PreferenceField
  value: unknown
  disabled: boolean
  onChange: (key: string, value: unknown) => void
}

// Memoized, so that a keystroke re-renders only the field that changed and the fields that depend on it.
const PreferenceRow = memo(function PreferenceRow({ field, value, disabled, onChange }: PreferenceRowProps) {
  const { t } = useTranslation("instances")
  const Widget = preferenceWidgets[field.control]
  return (
    <div className={WIDE_CONTROLS.has(field.control) ? "md:col-span-2" : undefined}>
      <Widget
        id={`preference-${field.key}`}
        field={field}
        labels={labelsOf(field)!}
        value={value}
        onChange={next => onChange(field.key, next)}
        disabled={disabled}
      />
      {field.readOnly && <p className="mt-1 text-xs text-muted-foreground">{t("preferences.readOnlyNote")}</p>}
    </div>
  )
})

const LEGACY_AUTORUN_PLACEHOLDERS = [
  { token: "%N", labelKey: "preferences.fileManagement.placeholderLabels.torrentName" },
  { token: "%L", labelKey: "preferences.fileManagement.placeholderLabels.category" },
  { token: "%G", labelKey: "preferences.fileManagement.placeholderLabels.tags" },
  { token: "%F", labelKey: "preferences.fileManagement.placeholderLabels.contentPath" },
  { token: "%R", labelKey: "preferences.fileManagement.placeholderLabels.rootPath" },
  { token: "%D", labelKey: "preferences.fileManagement.placeholderLabels.savePath" },
  { token: "%C", labelKey: "preferences.fileManagement.placeholderLabels.numberOfFiles" },
  { token: "%Z", labelKey: "preferences.fileManagement.placeholderLabels.torrentSize" },
  { token: "%T", labelKey: "preferences.fileManagement.placeholderLabels.currentTracker" },
  { token: "%I", labelKey: "preferences.fileManagement.placeholderLabels.infoHashV1" },
]

const MODERN_AUTORUN_PLACEHOLDERS = [
  ...LEGACY_AUTORUN_PLACEHOLDERS.slice(0, -1),
  { token: "%I", labelKey: "preferences.fileManagement.placeholderLabels.infoHashV1Optional" },
  { token: "%J", labelKey: "preferences.fileManagement.placeholderLabels.infoHashV2Optional" },
  { token: "%K", labelKey: "preferences.fileManagement.placeholderLabels.torrentId" },
]

// qBittorrent 4.5.0 added the program that runs when a torrent is added, and the %J and %K placeholders.
function AutorunPlaceholders({ supportsTorrentAdded }: { supportsTorrentAdded: boolean }) {
  const { t } = useTranslation("instances")
  const placeholders = supportsTorrentAdded ? MODERN_AUTORUN_PLACEHOLDERS : LEGACY_AUTORUN_PLACEHOLDERS
  return (
    <div className="space-y-2">
      <p className="text-sm font-medium">{t("preferences.fileManagement.supportedPlaceholders")}</p>
      <div className="grid grid-cols-1 gap-x-6 gap-y-2 text-xs text-muted-foreground sm:grid-cols-2">
        {placeholders.map(item => (
          <div key={item.token}>
            <code className="font-mono text-foreground">{item.token}</code> {t(item.labelKey)}
          </div>
        ))}
      </div>
    </div>
  )
}
