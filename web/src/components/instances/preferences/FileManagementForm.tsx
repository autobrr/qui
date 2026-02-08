/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { usePersistedStartPaused } from "@/hooks/usePersistedStartPaused"
import { useIncognitoMode } from "@/lib/incognito"
import { useForm } from "@tanstack/react-form"
import React from "react"
import { toast } from "sonner"

const LEGACY_AUTORUN_PLACEHOLDERS: Array<{ token: string; label: string }> = [
  { token: "%N", label: "Torrent name" },
  { token: "%L", label: "Category" },
  { token: "%G", label: "Tags (comma-separated)" },
  { token: "%F", label: "Content path" },
  { token: "%R", label: "Root path" },
  { token: "%D", label: "Save path" },
  { token: "%C", label: "Number of files" },
  { token: "%Z", label: "Torrent size (bytes)" },
  { token: "%T", label: "Current tracker" },
  { token: "%I", label: "Info hash v1" },
]

const MODERN_AUTORUN_PLACEHOLDERS: Array<{ token: string; label: string }> = [
  { token: "%N", label: "Torrent name" },
  { token: "%L", label: "Category" },
  { token: "%G", label: "Tags (comma-separated)" },
  { token: "%F", label: "Content path" },
  { token: "%R", label: "Root path" },
  { token: "%D", label: "Save path" },
  { token: "%C", label: "Number of files" },
  { token: "%Z", label: "Torrent size (bytes)" },
  { token: "%T", label: "Current tracker" },
  { token: "%I", label: "Info hash v1 (or \"-\")" },
  { token: "%J", label: "Info hash v2 (or \"-\")" },
  { token: "%K", label: "Torrent ID" },
]

const LEGACY_AUTORUN_PROGRAM_PLACEHOLDER = "/path/to/script \"%N\" \"%I\""
const MODERN_AUTORUN_PROGRAM_PLACEHOLDER = "/path/to/script \"%N\" \"%K\""
const AUTORUN_PROGRAM_TIP = "Tip: wrap placeholders in quotes, e.g. \"%N\", to preserve spaces."
const AUTORUN_ON_ADDED_MIN_WEBAPI_VERSION = "2.8.18" // qBittorrent 4.5.0+

function isWebAPIVersionAtLeast(version: string, minimum: string): boolean {
  // WebAPI versions are "x.y.z". Compare each numeric part.
  const parse = (value: string) => value.trim().split(".").map(part => Number.parseInt(part, 10))
  const a = parse(version)
  const b = parse(minimum)

  if (a.some(Number.isNaN) || b.some(Number.isNaN)) return false

  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const left = a[i] ?? 0
    const right = b[i] ?? 0
    if (left > right) return true
    if (left < right) return false
  }

  return true
}

function SwitchSetting({
  label,
  checked,
  onCheckedChange,
  description,
}: {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  description?: string
}) {
  const switchId = React.useId()
  const descriptionId = description ? `${switchId}-desc` : undefined

  return (
    <label
      htmlFor={switchId}
      className="flex items-center gap-3 cursor-pointer"
    >
      <Switch
        id={switchId}
        checked={checked}
        onCheckedChange={onCheckedChange}
        aria-describedby={descriptionId}
      />
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{label}</span>
        {description && (
          <p id={descriptionId} className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
    </label>
  )
}

interface FileManagementFormProps {
  instanceId: number
  onSuccess?: () => void
}

export function FileManagementForm({ instanceId, onSuccess }: FileManagementFormProps) {
  const { preferences, isLoading, updatePreferences, isUpdating } = useInstancePreferences(instanceId)
  const [startPausedEnabled, setStartPausedEnabled] = usePersistedStartPaused(instanceId, false)
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const [incognitoMode] = useIncognitoMode()
  const supportsSubcategories = capabilities?.supportsSubcategories ?? false
  const webAPIVersion = capabilities?.webAPIVersion?.trim() ?? ""
  const supportsAutorunOnTorrentAdded = isWebAPIVersionAtLeast(webAPIVersion, AUTORUN_ON_ADDED_MIN_WEBAPI_VERSION)
  const autorunPlaceholders = supportsAutorunOnTorrentAdded ? MODERN_AUTORUN_PLACEHOLDERS : LEGACY_AUTORUN_PLACEHOLDERS
  const autorunProgramPlaceholder = supportsAutorunOnTorrentAdded ? MODERN_AUTORUN_PROGRAM_PLACEHOLDER : LEGACY_AUTORUN_PROGRAM_PLACEHOLDER

  const form = useForm({
    defaultValues: {
      auto_tmm_enabled: false,
      torrent_changed_tmm_enabled: true,
      save_path_changed_tmm_enabled: true,
      category_changed_tmm_enabled: true,
      start_paused_enabled: false,
      use_subcategories: false,
      save_path: "",
      temp_path_enabled: false,
      temp_path: "",
      torrent_content_layout: "Original",
      autorun_on_torrent_added_enabled: false,
      autorun_on_torrent_added_program: "",
      autorun_enabled: false,
      autorun_program: "",
    },
    onSubmit: async ({ value }) => {
      try {
        // NOTE: Save start_paused_enabled to localStorage instead of qBittorrent
        // This is a workaround because qBittorrent's API rejects this preference
        setStartPausedEnabled(value.start_paused_enabled)

        // Update other preferences to qBittorrent (excluding start_paused_enabled)
        const qbittorrentPrefs: Record<string, unknown> = {
          auto_tmm_enabled: value.auto_tmm_enabled,
          torrent_changed_tmm_enabled: value.torrent_changed_tmm_enabled,
          save_path_changed_tmm_enabled: value.save_path_changed_tmm_enabled,
          category_changed_tmm_enabled: value.category_changed_tmm_enabled,
          save_path: value.save_path,
          temp_path_enabled: value.temp_path_enabled,
          temp_path: value.temp_path,
          torrent_content_layout: value.torrent_content_layout ?? "Original",
          autorun_enabled: value.autorun_enabled,
          autorun_program: value.autorun_program,
        }
        if (supportsAutorunOnTorrentAdded) {
          qbittorrentPrefs.autorun_on_torrent_added_enabled = value.autorun_on_torrent_added_enabled
          qbittorrentPrefs.autorun_on_torrent_added_program = value.autorun_on_torrent_added_program
        }
        if (supportsSubcategories) {
          qbittorrentPrefs.use_subcategories = Boolean(value.use_subcategories)
        }
        updatePreferences(qbittorrentPrefs)
        toast.success("File management settings updated successfully")
        onSuccess?.()
      } catch {
        toast.error("Failed to update file management settings")
      }
    },
  })

  // Update form when preferences change
  React.useEffect(() => {
    if (preferences) {
      form.setFieldValue("auto_tmm_enabled", preferences.auto_tmm_enabled)
      form.setFieldValue("torrent_changed_tmm_enabled", preferences.torrent_changed_tmm_enabled ?? true)
      form.setFieldValue("save_path_changed_tmm_enabled", preferences.save_path_changed_tmm_enabled ?? true)
      form.setFieldValue("category_changed_tmm_enabled", preferences.category_changed_tmm_enabled ?? true)
      if (supportsSubcategories) {
        form.setFieldValue("use_subcategories", Boolean(preferences.use_subcategories))
      } else {
        form.setFieldValue("use_subcategories", false)
      }
      form.setFieldValue("save_path", preferences.save_path)
      form.setFieldValue("temp_path_enabled", preferences.temp_path_enabled)
      form.setFieldValue("temp_path", preferences.temp_path)
      form.setFieldValue("torrent_content_layout", preferences.torrent_content_layout ?? "Original")
      form.setFieldValue("autorun_on_torrent_added_enabled", preferences.autorun_on_torrent_added_enabled)
      form.setFieldValue("autorun_on_torrent_added_program", preferences.autorun_on_torrent_added_program ?? "")
      form.setFieldValue("autorun_enabled", preferences.autorun_enabled)
      form.setFieldValue("autorun_program", preferences.autorun_program ?? "")
    }
  }, [preferences, form, supportsSubcategories])

  // Update form when localStorage start_paused_enabled changes
  React.useEffect(() => {
    form.setFieldValue("start_paused_enabled", startPausedEnabled)
  }, [startPausedEnabled, form])

  if (isLoading) {
    return (
      <div className="text-center py-8" role="status" aria-live="polite">
        <p className="text-sm text-muted-foreground">Loading file management settings...</p>
      </div>
    )
  }

  if (!preferences) {
    return (
      <div className="text-center py-8" role="alert">
        <p className="text-sm text-muted-foreground">Failed to load preferences</p>
      </div>
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-6"
    >
      <div className="space-y-6">
        <form.Field name="auto_tmm_enabled">
          {(field) => (
            <SwitchSetting
              label="Automatic Torrent Management"
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description="Use category-based paths for downloads"
            />
          )}
        </form.Field>

        <form.Subscribe selector={(state) => state.values.auto_tmm_enabled}>
          {(autoTmmEnabled) =>
            autoTmmEnabled && (
              <div className="ml-6 pl-4 border-l-2 border-muted space-y-4">
                <form.Field name="torrent_changed_tmm_enabled">
                  {(field) => (
                    <SwitchSetting
                      label="Relocate on Category Change"
                      checked={field.state.value as boolean}
                      onCheckedChange={field.handleChange}
                      description="Relocate torrent when its category changes (disable to switch to Manual Mode instead)"
                    />
                  )}
                </form.Field>

                <form.Field name="save_path_changed_tmm_enabled">
                  {(field) => (
                    <SwitchSetting
                      label="Relocate on Default Save Path Change"
                      checked={field.state.value as boolean}
                      onCheckedChange={field.handleChange}
                      description="Relocate affected torrents when default save path changes (disable to switch to Manual Mode instead)"
                    />
                  )}
                </form.Field>

                <form.Field name="category_changed_tmm_enabled">
                  {(field) => (
                    <SwitchSetting
                      label="Relocate on Category Save Path Change"
                      checked={field.state.value as boolean}
                      onCheckedChange={field.handleChange}
                      description="Relocate affected torrents when category save path changes (disable to switch to Manual Mode instead)"
                    />
                  )}
                </form.Field>
              </div>
            )
          }
        </form.Subscribe>

        {supportsSubcategories && (
          <form.Field name="use_subcategories">
            {(field) => (
              <SwitchSetting
                label="Enable Subcategories"
                checked={field.state.value as boolean}
                onCheckedChange={field.handleChange}
                description="Allow creating nested categories using slash separator (e.g., Movies/4K)"
              />
            )}
          </form.Field>
        )}

        <form.Field name="start_paused_enabled">
          {(field) => (
            <SwitchSetting
              label="Start Torrents Paused"
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description="New torrents start in paused state"
            />
          )}
        </form.Field>

        <form.Field name="save_path">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Default Save Path</Label>
              <p className="text-xs text-muted-foreground">
                Default directory for downloading files
              </p>
              <Input
                value={field.state.value as string}
                onChange={(e) => field.handleChange(e.target.value)}
                placeholder="/downloads"
                className={incognitoMode ? "blur-sm select-none" : ""}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="temp_path_enabled">
          {(field) => (
            <SwitchSetting
              label="Use Temporary Path"
              checked={field.state.value as boolean}
              onCheckedChange={field.handleChange}
              description="Download to temporary path before moving to final location"
            />
          )}
        </form.Field>

        <form.Field name="temp_path">
          {(field) => (
            <form.Subscribe selector={(state) => state.values.temp_path_enabled}>
              {(tempPathEnabled) => (
                <div className="space-y-2">
                  <Label className="text-sm font-medium">Temporary Download Path</Label>
                  <p className="text-xs text-muted-foreground">
                    Directory where torrents are downloaded before moving to save path
                  </p>
                  <Input
                    value={field.state.value as string}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="/temp-downloads"
                    disabled={!tempPathEnabled}
                    className={incognitoMode ? "blur-sm select-none" : ""}
                  />
                </div>
              )}
            </form.Subscribe>
          )}
        </form.Field>

        <form.Field name="torrent_content_layout">
          {(field) => (
            <div className="space-y-2">
              <Label className="text-sm font-medium">Default Content Layout</Label>
              <p className="text-xs text-muted-foreground">
                How torrent files are organized within the save directory
              </p>
              <Select
                value={field.state.value as string}
                onValueChange={field.handleChange}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select content layout" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Original">Original</SelectItem>
                  <SelectItem value="Subfolder">Create subfolder</SelectItem>
                  <SelectItem value="NoSubfolder">Don't create subfolder</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
        </form.Field>

        <Card className="bg-muted/20 border-muted/60">
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Run External Program</CardTitle>
            <CardDescription>
              Run a command when a torrent finishes. Some instances also support running on torrent added. qBittorrent will expand placeholders like <code className="font-mono">%N</code>.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            {supportsAutorunOnTorrentAdded ? (
              <form.Field name="autorun_on_torrent_added_enabled">
                {(enabledField) => (
                  <div className="space-y-3">
                    <SwitchSetting
                      label="Run on Torrent Added"
                      checked={enabledField.state.value as boolean}
                      onCheckedChange={enabledField.handleChange}
                      description="Triggered right after a torrent is added to the client"
                    />

                    <form.Field name="autorun_on_torrent_added_program">
                      {(programField) => (
                        <div className="space-y-2 ml-6 pl-4 border-l-2 border-muted">
                          <Label className="text-sm font-medium">Command</Label>
                          <Input
                            value={programField.state.value as string}
                            onChange={(e) => programField.handleChange(e.target.value)}
                            placeholder={autorunProgramPlaceholder}
                            disabled={!(enabledField.state.value as boolean)}
                            className={incognitoMode ? "blur-sm select-none" : ""}
                          />
                          <p className="text-xs text-muted-foreground">
                            {AUTORUN_PROGRAM_TIP}
                          </p>
                        </div>
                      )}
                    </form.Field>
                  </div>
                )}
              </form.Field>
            ) : (
              <div className="space-y-1 rounded-md border border-muted bg-background/40 p-3">
                <p className="text-sm font-medium">Run on Torrent Added</p>
                <p className="text-xs text-muted-foreground">
                  Requires qBittorrent 4.5.0+ (Web API {AUTORUN_ON_ADDED_MIN_WEBAPI_VERSION}+). This instance reports {webAPIVersion || "no Web API version"}.
                </p>
              </div>
            )}

            <form.Field name="autorun_enabled">
              {(enabledField) => (
                <div className="space-y-3">
                  <SwitchSetting
                    label="Run on Torrent Finished"
                    checked={enabledField.state.value as boolean}
                    onCheckedChange={enabledField.handleChange}
                    description="Triggered when a torrent completes"
                  />

                  <form.Field name="autorun_program">
                    {(programField) => (
                      <div className="space-y-2 ml-6 pl-4 border-l-2 border-muted">
                        <Label className="text-sm font-medium">Command</Label>
                        <Input
                          value={programField.state.value as string}
                          onChange={(e) => programField.handleChange(e.target.value)}
                          placeholder={autorunProgramPlaceholder}
                          disabled={!(enabledField.state.value as boolean)}
                          className={incognitoMode ? "blur-sm select-none" : ""}
                        />
                        <p className="text-xs text-muted-foreground">
                          {AUTORUN_PROGRAM_TIP}
                        </p>
                      </div>
                    )}
                  </form.Field>
                </div>
              )}
            </form.Field>

            <div className="space-y-2">
              <Label className="text-sm font-medium">Supported Placeholders (case sensitive)</Label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-2 text-xs text-muted-foreground">
                {autorunPlaceholders.map((item) => (
                  <div key={item.token}>
                    <code className="font-mono text-foreground">{item.token}</code> {item.label}
                  </div>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="flex justify-end pt-4">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button
              type="submit"
              disabled={!canSubmit || isSubmitting || isUpdating}
              className="min-w-32"
            >
              {isSubmitting || isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
