/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PooledCompletionSetting } from "@/components/cross-seed/PooledCompletionSetting"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { useInstances } from "@/hooks/useInstances"
import type { Instance } from "@/types"
import { ChevronDown, Loader2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

/** Per-instance hardlink/reflink mode settings plus the global pooled-completion checkbox. */
export function HardlinkModeSettings({
  pooledPartialCompletionEnabled,
  onPooledPartialCompletionEnabledChange,
}: {
  pooledPartialCompletionEnabled: boolean
  onPooledPartialCompletionEnabledChange: (checked: boolean) => void
}) {
  const { t } = useTranslation("crossseed")
  const { instances, updateInstance, isUpdating } = useInstances()
  const [expandedInstances, setExpandedInstances] = useState<string[]>([])
  const [dirtyMap, setDirtyMap] = useState<Record<number, boolean>>({})
  type InstanceFormState = {
    useHardlinks: boolean
    useReflinks: boolean
    hardlinkBaseDir: string
    hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
    fallbackToRegularMode: boolean
  }
  const [formMap, setFormMap] = useState<Record<number, InstanceFormState>>({})
  const [isOpen, setIsOpen] = useState<boolean | undefined>(undefined)

  const activeInstances = useMemo(
    () => (instances ?? []).filter((inst) => inst.isActive),
    [instances]
  )

  // Auto-expand when 3 or fewer instances (only on first load)
  useEffect(() => {
    if (isOpen === undefined && instances !== undefined) {
      const activeCount = (instances ?? []).filter((inst) => inst.isActive).length
      setIsOpen(activeCount <= 3)
    }
  }, [instances, isOpen])

  const getForm = useCallback((instance: Instance) => {
    return formMap[instance.id] ?? {
      useHardlinks: instance.useHardlinks,
      useReflinks: instance.useReflinks,
      hardlinkBaseDir: instance.hardlinkBaseDir || "",
      hardlinkDirPreset: instance.hardlinkDirPreset || "flat",
      fallbackToRegularMode: instance.fallbackToRegularMode ?? false,
    }
  }, [formMap])

  const handleFormChange = <K extends keyof InstanceFormState>(
    instanceId: number,
    field: K,
    value: InstanceFormState[K],
    currentForm: InstanceFormState
  ) => {
    setFormMap((prev) => ({
      ...prev,
      [instanceId]: {
        ...currentForm,
        [field]: value,
      },
    }))
    setDirtyMap((prev) => ({ ...prev, [instanceId]: true }))
  }

  const handleModeChange = (
    instanceId: number,
    mode: "regular" | "hardlink" | "reflink",
    currentForm: InstanceFormState
  ) => {
    setFormMap((prev) => ({
      ...prev,
      [instanceId]: {
        ...currentForm,
        useHardlinks: mode === "hardlink",
        useReflinks: mode === "reflink",
      },
    }))
    setDirtyMap((prev) => ({ ...prev, [instanceId]: true }))
  }

  const handleSave = (instance: Instance) => {
    const form = getForm(instance)

    // Validate before saving
    if ((form.useHardlinks || form.useReflinks) && !instance.hasLocalFilesystemAccess) {
      const mode = form.useReflinks ? "reflink" : "hardlink"
      toast.error(t("toast.cannotEnableMode", { mode }), {
        description: t("toast.noLocalFilesystemAccess", { name: instance.name }),
      })
      return
    }

    if ((form.useHardlinks || form.useReflinks) && !form.hardlinkBaseDir.trim()) {
      const mode = form.useReflinks ? "reflink" : "hardlink"
      toast.error(t("toast.cannotEnableMode", { mode }), {
        description: t("toast.baseDirRequired"),
      })
      return
    }

    updateInstance({
      id: instance.id,
      data: {
        name: instance.name,
        host: instance.host,
        username: instance.username,
        useHardlinks: form.useHardlinks,
        useReflinks: form.useReflinks,
        hardlinkBaseDir: form.hardlinkBaseDir,
        hardlinkDirPreset: form.hardlinkDirPreset,
        fallbackToRegularMode: form.fallbackToRegularMode,
      },
    }, {
      onSuccess: () => {
        toast.success(t("toast.settingsSaved"), {
          description: instance.name,
        })
        setDirtyMap((prev) => ({ ...prev, [instance.id]: false }))
      },
      onError: (error) => {
        toast.error(t("toast.failedToSaveSettings"), {
          description: error instanceof Error ? error.message : t("toast.unknownError"),
        })
      },
    })
  }

  const pooledCompletionSetting = (
    <PooledCompletionSetting
      id="pooled-partial-completion"
      checked={pooledPartialCompletionEnabled}
      onCheckedChange={onPooledPartialCompletionEnabledChange}
    />
  )

  if (!activeInstances.length) {
    return (
      <Collapsible className="rounded-lg border border-border/70 bg-muted/40">
        <CollapsibleTrigger className="flex w-full items-center justify-between p-4 font-medium [&[data-state=open]>svg]:rotate-180">
          <span>{t("rules.hardlinkReflink")}</span>
          <ChevronDown className="h-4 w-4 transition-transform duration-200" />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="border-t border-border/70 p-4 pt-4 space-y-4">
            <p className="text-sm text-muted-foreground">{t("rules.noActiveInstances")}</p>
            {pooledCompletionSetting}
          </div>
        </CollapsibleContent>
      </Collapsible>
    )
  }

  return (
    <Collapsible open={isOpen} onOpenChange={setIsOpen} className="rounded-lg border border-border/70 bg-muted/40">
      <CollapsibleTrigger className="flex w-full items-center justify-between p-4 font-medium [&[data-state=open]>svg]:rotate-180">
        <span>{t("rules.hardlinkReflink")}</span>
        <ChevronDown className="h-4 w-4 transition-transform duration-200" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <p className="text-xs text-muted-foreground px-4">
          {t("rules.hardlinkDescription")}
          <strong>{t("rules.reflinkNote")}</strong>
        </p>
        <div className="border-t border-border/70 p-4 space-y-4">

          <Accordion
            type="multiple"
            value={expandedInstances}
            onValueChange={setExpandedInstances}
            className="space-y-2"
          >
            {activeInstances.map((instance) => {
              const form = getForm(instance)
              const isDirty = dirtyMap[instance.id] ?? false
              const canEnableModes = instance.hasLocalFilesystemAccess

              return (
                <AccordionItem
                  key={instance.id}
                  value={String(instance.id)}
                  className="border border-border/70 rounded-lg bg-background/50"
                >
                  <AccordionTrigger className="px-4 py-3 hover:no-underline">
                    <div className="flex items-center gap-3 flex-1 min-w-0">
                      <span className="font-medium truncate">{instance.name}</span>
                      {form.useHardlinks && (
                        <Badge variant="outline" className="shrink-0 bg-primary/10 text-primary border-primary/30 text-xs">
                          {t("rules.hardlink")}
                        </Badge>
                      )}
                      {form.useReflinks && (
                        <Badge variant="outline" className="shrink-0 bg-blue-500/10 text-blue-500 border-blue-500/30 text-xs">
                          {t("rules.reflink")}
                        </Badge>
                      )}
                      {!canEnableModes && (
                        <Badge variant="outline" className="shrink-0 bg-muted text-muted-foreground border-muted-foreground/30 text-xs">
                          {t("rules.noLocalAccess")}
                        </Badge>
                      )}
                    </div>
                  </AccordionTrigger>
                  <AccordionContent className="px-4 pb-4">
                    <div className="space-y-4 pt-2">
                      {/* Link mode selection */}
                      <div className="space-y-2">
                        <Label className="font-medium">{t("rules.crossSeedMode")}</Label>
                        {!canEnableModes && (
                          <p className="text-xs text-muted-foreground">
                            {t("rules.enableLocalFilesystem")}
                          </p>
                        )}
                        <RadioGroup
                          value={form.useReflinks ? "reflink" : form.useHardlinks ? "hardlink" : "regular"}
                          onValueChange={(value) => handleModeChange(instance.id, value as "regular" | "hardlink" | "reflink", form)}
                          disabled={isUpdating}
                          className="space-y-2"
                        >
                          <div className="flex items-start gap-3">
                            <RadioGroupItem value="regular" id={`mode-regular-${instance.id}`} className="mt-0.5" />
                            <div className="space-y-0.5 flex-1">
                              <Label htmlFor={`mode-regular-${instance.id}`} className="font-medium cursor-pointer">{t("rules.regular")}</Label>
                              <p className="text-xs text-muted-foreground">{t("rules.regularDescription")}</p>
                            </div>
                          </div>
                          <div className="flex items-start gap-3">
                            <RadioGroupItem
                              value="hardlink"
                              id={`mode-hardlink-${instance.id}`}
                              className="mt-0.5"
                              disabled={!canEnableModes}
                            />
                            <div className="space-y-0.5 flex-1">
                              <Label htmlFor={`mode-hardlink-${instance.id}`} className={`font-medium cursor-pointer ${!canEnableModes ? "text-muted-foreground" : ""}`}>{t("rules.hardlink")}</Label>
                              <p className="text-xs text-muted-foreground">{t("rules.hardlinkDescription2")}</p>
                            </div>
                          </div>
                          <div className="flex items-start gap-3">
                            <RadioGroupItem
                              value="reflink"
                              id={`mode-reflink-${instance.id}`}
                              className="mt-0.5"
                              disabled={!canEnableModes}
                            />
                            <div className="space-y-0.5 flex-1">
                              <Label htmlFor={`mode-reflink-${instance.id}`} className={`font-medium cursor-pointer ${!canEnableModes ? "text-muted-foreground" : ""}`}>{t("rules.reflink")}</Label>
                              <p className="text-xs text-muted-foreground">{t("rules.reflinkDescription")}</p>
                            </div>
                          </div>
                        </RadioGroup>
                      </div>

                      {(form.useHardlinks || form.useReflinks) && (
                        <>
                          <Separator />

                          <div className="space-y-4">
                            <div className="space-y-2">
                              <div className="flex items-center gap-1.5">
                                <Label>{t("rules.baseDirectories")}</Label>
                                <FieldHelp>{t("rules.baseDirectoriesDescription")}</FieldHelp>
                              </div>
                              <Input
                                placeholder={t("rules.baseDirectoriesPlaceholder")}
                                value={form.hardlinkBaseDir}
                                onChange={(e) => handleFormChange(instance.id, "hardlinkBaseDir", e.target.value, form)}
                              />
                            </div>

                            <div className="space-y-2">
                              <Label>{t("rules.directoryOrganization")}</Label>
                              <Select
                                value={form.hardlinkDirPreset}
                                onValueChange={(value: "flat" | "by-tracker" | "by-instance") =>
                                  handleFormChange(instance.id, "hardlinkDirPreset", value, form)
                                }
                              >
                                <SelectTrigger>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value="flat">{t("rules.flat")}</SelectItem>
                                  <SelectItem value="by-tracker">{t("rules.byTracker")}</SelectItem>
                                  <SelectItem value="by-instance">{t("rules.byInstance")}</SelectItem>
                                </SelectContent>
                              </Select>
                            </div>

                            <div className="flex items-center gap-3">
                              <Checkbox
                                id={`fallback-${instance.id}`}
                                checked={form.fallbackToRegularMode}
                                onCheckedChange={(checked) =>
                                  handleFormChange(instance.id, "fallbackToRegularMode", checked === true, form)
                                }
                              />
                              <div className="flex items-center gap-1.5">
                                <Label htmlFor={`fallback-${instance.id}`} className="font-medium cursor-pointer">
                                  {t("rules.fallbackToRegular")}
                                </Label>
                                <FieldHelp>
                                  {t("rules.fallbackDescription", { mode: form.useReflinks ? t("rules.reflink") : t("rules.hardlink") })}
                                </FieldHelp>
                              </div>
                            </div>
                          </div>
                        </>
                      )}

                      {isDirty && (
                        <Button
                          size="sm"
                          onClick={() => handleSave(instance)}
                          disabled={isUpdating}
                        >
                          {isUpdating && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                          {t("rules.saveChanges")}
                        </Button>
                      )}
                    </div>
                  </AccordionContent>
                </AccordionItem>
              )
            })}
          </Accordion>

          {pooledCompletionSetting}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
