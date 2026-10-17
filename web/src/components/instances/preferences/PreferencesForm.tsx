/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { TabsContent } from "@/components/ui/tabs"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstancePreferences, valuesOf } from "@/hooks/useInstancePreferences"
import { api } from "@/lib/api"
import { formatErrorMessage } from "@/lib/format-error-message"
import type { AppPreferences, PreferenceField } from "@/types"
import { useQueryClient } from "@tanstack/react-query"
import { memo, useCallback, useState, type FormEvent } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"

import { preferenceTabLabels } from "./labels"
import { labelsOf, networkBindingKey, preferenceTabsOf, preferenceViewKey } from "./preferenceView"
import { PreferencesFormShell } from "./PreferencesFormShell"
import { NetworkBindingWidget, preferenceWidgets } from "./PreferenceWidgets"

const WIDE_CONTROLS = new Set<PreferenceField["control"]>(["switch", "list", "watchFolders"])

// The network binding widget draws these keys together with current_network_interface.
const NETWORK_BINDING_PARTS = new Set(["current_interface_address", "current_interface_name"])

interface PreferencesFormProps {
  instanceId: number
  fields: PreferenceField[]
}

// The qBittorrent tabs of the preferences dialog, drawn from the catalog view. All tabs share
// one form: a save from any tab sends every changed key.
export function PreferencesForm({ instanceId, fields }: PreferencesFormProps) {
  const { t } = useTranslation("instances")
  const queryClient = useQueryClient()
  // The dialog reads the view itself, so this hook only saves.
  const { updatePreferences, isUpdating } = useInstancePreferences(instanceId, { enabled: false })
  // The values that qBittorrent has. They do not follow a refresh of the view, so that a
  // refresh does not reset open edits.
  const [saved, setSaved] = useState(() => valuesOf(fields))
  const [values, setValues] = useState(saved)

  const setValue = useCallback((key: string, value: unknown) => setValues(previous => {
    const next = { ...previous, [key]: value }
    // qBittorrent turns a seeding limit on when it gets a limit without the switch. So a switch
    // that turns off empties the box, and a switch that turns on fills an empty box with the
    // saved limit or the starting value of the qBittorrent WebUI.
    for (const field of fields) {
      const labels = field.dependsOn === key ? labelsOf(field) : undefined
      if (labels?.whenEnabled === undefined) continue
      if (value === false) {
        next[field.key] = labels.unlimited
      } else if (next[field.key] === labels.unlimited) {
        next[field.key] = saved[field.key] !== labels.unlimited ? saved[field.key] : labels.whenEnabled
      }
    }
    return next
  }), [fields, saved])

  const byKey = new Map(fields.map(field => [field.key, field]))
  // A read-only field has no edits to keep, so it shows the latest view.
  const valueOf = (field: PreferenceField) => !field.readOnly && field.key in values ? values[field.key] : field.value
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
      // The saved binding decides which interface and address are "not available".
      void queryClient.invalidateQueries({ queryKey: networkBindingKey(instanceId) })
    }
    toast.success(t("preferences.toast.saved"))
  }

  return preferenceTabsOf(fields).map(tab => {
    const tabLabels = preferenceTabLabels[tab]
    const sections = new Map<string, PreferenceField[]>()
    for (const field of fields) {
      if (field.tab === tab && labelsOf(field) && !NETWORK_BINDING_PARTS.has(field.key)) {
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
                    {sectionFields.map(field => field.optionSource === "networkInterfaces" ? (
                      <div key={field.key} className="md:col-span-2">
                        <NetworkBindingWidget
                          instanceId={instanceId}
                          interfaceLabels={labelsOf(field)!}
                          addressLabels={preferenceTabLabels.advanced.fields.current_interface_address}
                          iface={String(values.current_network_interface ?? "")}
                          address={String(values.current_interface_address ?? "")}
                          // The name belongs to the saved interface, not to an interface selected in the form.
                          interfaceName={values.current_network_interface === byKey.get("current_network_interface")?.value
                            ? String(byKey.get("current_interface_name")?.value ?? "")
                            : ""}
                          onChange={setValue}
                        />
                      </div>
                    ) : (
                      <PreferenceRow
                        key={field.key}
                        instanceId={instanceId}
                        field={field}
                        value={valueOf(field)}
                        disabled={field.readOnly === true || !isEnabled(field)}
                        onChange={setValue}
                      />
                    ))}
                  </div>
                  {section === "emailNotification" && (
                    <SendTestEmailButton instanceId={instanceId} disabled={saved.mail_notification_enabled !== true} />
                  )}
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
  instanceId: number
  field: PreferenceField
  value: unknown
  disabled: boolean
  onChange: (key: string, value: unknown) => void
}

// Memoized, so that a keystroke re-renders only the field that changed and the fields that depend on it.
const PreferenceRow = memo(function PreferenceRow({ instanceId, field, value, disabled, onChange }: PreferenceRowProps) {
  const { t } = useTranslation("instances")
  const Widget = preferenceWidgets[field.control]
  const labels = labelsOf(field)!
  return (
    <div className={WIDE_CONTROLS.has(field.control) ? "md:col-span-2" : undefined}>
      <Widget
        id={`preference-${field.key}`}
        instanceId={instanceId}
        field={field}
        labels={labels}
        value={value}
        onChange={next => onChange(field.key, next)}
        disabled={disabled}
      />
      {field.readOnly && labels.readOnlyNoteKey && <p className="mt-1 text-xs text-muted-foreground">{t(labels.readOnlyNoteKey)}</p>}
    </div>
  )
})

// qBittorrent sends the test email with its saved settings, and sends nothing while email
// notification is off. So the button follows the saved switch, not the switch in the form.
function SendTestEmailButton({ instanceId, disabled }: { instanceId: number; disabled: boolean }) {
  const { t } = useTranslation("instances")
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const [isSending, setIsSending] = useState(false)
  if (!capabilities?.supportsTestEmail) return null

  const send = async () => {
    setIsSending(true)
    try {
      await api.sendTestEmail(instanceId)
      toast.success(t("preferences.toast.testEmailQueued"), { description: t("preferences.toast.testEmailQueuedDescription") })
    } catch (error) {
      toast.error(t("preferences.toast.testEmailFailed"), {
        description: error instanceof Error ? formatErrorMessage(error.message) : undefined,
      })
    } finally {
      setIsSending(false)
    }
  }

  return (
    <Button type="button" variant="outline" disabled={disabled || isSending} onClick={send}>
      {t("preferences.fields.sendTestEmail")}
    </Button>
  )
}

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
