/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQueries } from "@tanstack/react-query"
import { AlertTriangle, Download, Link2, Pencil, Trash2, Upload, X } from "lucide-react"
import { useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { instanceTrackersQueryOptions } from "@/hooks/useInstanceTrackers"
import { useInstances } from "@/hooks/useInstances"
import { useCreateTrackerCustomization, useDeleteTrackerCustomization, useTrackerCustomizations, useUpdateTrackerCustomization } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import {
  buildTrackerRows,
  exportTrackerCustomizations,
  parseTrackerCustomizationsImport,
  toCustomizationInput,
  type ImportError
} from "@/lib/tracker-customizations-manage"
import { copyTextToClipboard } from "@/lib/utils"
import type { TrackerCustomization } from "@/types"

type CustomizeMode = "edit" | "rename" | "merge"

export function TrackerCustomizationsManager() {
  const { t } = useTranslation("settings")
  const { data: trackerIcons } = useTrackerIcons()
  const [incognitoMode] = useIncognitoMode()
  const { instances } = useInstances()

  const { data: customizations, isLoading: customizationsLoading } = useTrackerCustomizations()
  const createCustomization = useCreateTrackerCustomization()
  const updateCustomization = useUpdateTrackerCustomization()
  const deleteCustomization = useDeleteTrackerCustomization()

  const activeInstances = useMemo(() => (instances ?? []).filter(instance => instance.isActive), [instances])
  const trackerQueries = useQueries({ queries: activeInstances.map(instance => instanceTrackersQueryOptions(instance.id)) })
  const trackersLoading = trackerQueries.some(query => query.isLoading)
  const domainsKey = [...new Set(trackerQueries.flatMap(query => Object.keys(query.data ?? {})))].sort().join("\n")

  const rows = useMemo(
    () => buildTrackerRows(domainsKey ? domainsKey.split("\n") : [], customizations ?? []),
    [domainsKey, customizations]
  )

  const [selectedDomainState, setSelectedDomains] = useState<Set<string>>(new Set())
  // A selected domain counts only while it has its own row. A save, an import, or another tab can give it a customization.
  const selectedDomains = useMemo(() => {
    const standalone = new Set(rows.filter(row => row.customizationId === undefined).map(row => row.domain))
    return new Set([...selectedDomainState].filter(domain => standalone.has(domain)))
  }, [selectedDomainState, rows])
  // Drop the domains for good, so a later delete of their customization does not select them again.
  if (selectedDomains.size !== selectedDomainState.size) setSelectedDomains(selectedDomains)
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null)
  const [customizeOpen, setCustomizeOpen] = useState(false)
  const [editingCustomization, setEditingCustomization] = useState<{ id: number; displayName: string; domains: string[] } | null>(null)
  const [importOpen, setImportOpen] = useState(false)

  const toggleSelection = (domain: string) => {
    const next = new Set(selectedDomains)
    if (next.has(domain)) {
      next.delete(domain)
    } else {
      next.add(domain)
    }
    setSelectedDomains(next)
  }

  const toggleGroupSelection = (customizationId: number) => {
    setSelectedGroupId(prev => prev === customizationId ? null : customizationId)
  }

  const clearSelection = () => {
    setSelectedDomains(new Set())
    setSelectedGroupId(null)
  }

  const handleMergeIntoGroup = (targetGroupId: number, domain?: string) => {
    if (updateCustomization.isPending) return
    const group = customizations?.find(c => c.id === targetGroupId)
    if (!group) return

    const domainsSet = new Set(selectedDomains)
    if (domain) domainsSet.add(domain)
    const domainsToMerge = Array.from(domainsSet)
    if (domainsToMerge.length === 0) return

    updateCustomization.mutate({
      id: targetGroupId,
      data: { displayName: group.displayName, domains: [...new Set([...group.domains, ...domainsToMerge])] },
    }, {
      onSuccess: clearSelection,
    })
  }

  const handleDelete = (customizationId: number) => {
    deleteCustomization.mutate(customizationId, {
      onSuccess: () => setSelectedGroupId(prev => prev === customizationId ? null : prev),
    })
  }

  const closeCustomizeDialog = () => {
    setCustomizeOpen(false)
    setEditingCustomization(null)
  }

  const handleSaveCustomization = (displayName: string) => {
    const domains = editingCustomization ? editingCustomization.domains : Array.from(selectedDomains)
    if (domains.length === 0) return

    if (editingCustomization) {
      updateCustomization.mutate(
        { id: editingCustomization.id, data: { displayName, domains } },
        { onSuccess: closeCustomizeDialog }
      )
      return
    }

    // A new name that equals an existing one merges into that customization.
    const existing = customizations?.find(c => c.displayName.toLowerCase() === displayName.toLowerCase())
    const onSuccess = () => {
      closeCustomizeDialog()
      clearSelection()
    }
    if (existing) {
      updateCustomization.mutate(
        { id: existing.id, data: { displayName: existing.displayName, domains: [...existing.domains, ...domains] } },
        { onSuccess }
      )
    } else {
      createCustomization.mutate({ displayName, domains }, { onSuccess })
    }
  }

  // Put the domains that have an icon first, so the first domain gives the row its icon.
  const reorderDomainsForIcons = (domains: string[]): string[] => {
    if (!trackerIcons || domains.length <= 1) return domains
    const hasIcon = (d: string) => Boolean(trackerIcons[d.toLowerCase()] || trackerIcons[d])
    return [...domains.filter(hasIcon), ...domains.filter(d => !hasIcon(d))]
  }

  const openEditDialog = (customizationId: number, displayName: string, domains: string[]) => {
    setEditingCustomization({ id: customizationId, displayName, domains })
    setCustomizeOpen(true)
  }

  // With other domains selected, this adds the domain to the merge.
  const openRenameDialog = (domain: string) => {
    setEditingCustomization(null)
    if (selectedDomains.size >= 2 || (selectedDomains.size === 1 && !selectedDomains.has(domain))) {
      setSelectedDomains(new Set(reorderDomainsForIcons([...selectedDomains, domain])))
    } else {
      setSelectedDomains(new Set([domain]))
    }
    setCustomizeOpen(true)
  }

  const handleRemoveDomainFromDialog = (domainToRemove: string) => {
    if (editingCustomization) {
      const domains = editingCustomization.domains.filter(d => d !== domainToRemove)
      if (domains.length > 0) {
        setEditingCustomization({ ...editingCustomization, domains })
      }
      return
    }
    const next = new Set(selectedDomains)
    next.delete(domainToRemove)
    if (next.size > 0) {
      setSelectedDomains(next)
    }
  }

  const handleExport = async () => {
    if (!customizations || customizations.length === 0) {
      toast.error(t("trackerCustomizations.toasts.noCustomizationsToExport"))
      return
    }
    try {
      await copyTextToClipboard(exportTrackerCustomizations(customizations))
      toast.success(t("trackerCustomizations.toasts.copiedToClipboard"))
    } catch (error) {
      console.error("[Export] Failed to copy to clipboard:", error)
      toast.error(t("trackerCustomizations.toasts.failedToCopy"))
    }
  }

  const saving = createCustomization.isPending || updateCustomization.isPending
  const customizeMode: CustomizeMode = editingCustomization ? "edit" : selectedDomains.size === 1 ? "rename" : "merge"

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 space-y-1.5">
            <CardTitle>{t("trackerCustomizations.cardTitle")}</CardTitle>
            <CardDescription>{t("trackerCustomizations.cardDescription")}</CardDescription>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <IconAction label={t("trackerCustomizations.importTooltip")} onClick={() => setImportOpen(true)} disabled={saving}>
              <Download className="h-4 w-4" />
            </IconAction>
            <IconAction label={t("trackerCustomizations.exportTooltip")} onClick={handleExport} disabled={!customizations || customizations.length === 0}>
              <Upload className="h-4 w-4" />
            </IconAction>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {customizationsLoading || trackersLoading ? t("trackerCustomizations.loading") : t("trackerCustomizations.empty")}
          </p>
        ) : (
          <div className="divide-y rounded-md border">
            {rows.map(row => {
              const { domain, displayName, domains, customizationId } = row
              const hasCustomization = customizationId !== undefined
              const isSelected = !hasCustomization && selectedDomains.has(domain)
              const isGroupSelected = hasCustomization && selectedGroupId === customizationId
              const isMerged = domains.length > 1
              const shownName = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
              const shownDomains = incognitoMode ? domains.map(getLinuxTrackerDomain) : domains
              const subtitle = isMerged ? t("trackerCustomizations.mergedFrom", { domains: shownDomains.join(", ") }) : hasCustomization && displayName !== domain ? t("trackerCustomizations.original", { domain: shownDomains[0] }) : ""
              const showCheckbox = !hasCustomization || selectedGroupId === null || isGroupSelected
              const canMergeIntoGroup = hasCustomization && selectedDomains.size > 0 && (selectedGroupId === null || selectedGroupId === customizationId)

              return (
                <div
                  key={hasCustomization ? `c-${customizationId}` : `d-${domain}`}
                  className={`group flex min-h-11 items-center gap-2 px-2 ${isSelected || isGroupSelected ? "bg-primary/5" : ""} hover:bg-muted/50`}
                >
                  <div className="flex h-11 w-8 shrink-0 items-center justify-center">
                    {showCheckbox && (
                      <Checkbox
                        checked={hasCustomization ? isGroupSelected : isSelected}
                        onCheckedChange={() => hasCustomization ? toggleGroupSelection(customizationId) : toggleSelection(domain)}
                        aria-label={shownName}
                        // the box is 16px; the pseudo-element grows the tap target to fill the 44px cell
                        className="relative before:absolute before:-inset-3.5 before:content-[''] sm:opacity-0 sm:group-hover:opacity-100 sm:data-[state=checked]:opacity-100"
                      />
                    )}
                  </div>
                  <TrackerIconImage tracker={incognitoMode ? getLinuxTrackerDomain(domain) : domain} trackerIcons={trackerIcons} />
                  <div className="min-w-0 flex-1 py-1.5">
                    <div className="flex items-center gap-1">
                      <span className="truncate text-sm font-medium">{shownName}</span>
                      {isMerged && <Link2 className="h-3 w-3 shrink-0 text-muted-foreground" />}
                    </div>
                    {subtitle && <div className="truncate text-xs text-muted-foreground">{subtitle}</div>}
                  </div>
                  <div className="flex shrink-0 items-center gap-0.5 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-within:opacity-100">
                    {hasCustomization ? (
                      canMergeIntoGroup ? (
                        <IconAction label={t("trackerCustomizations.mergeTooltip")} onClick={() => handleMergeIntoGroup(customizationId)} disabled={saving}>
                          <Link2 className="h-3.5 w-3.5 text-primary" />
                        </IconAction>
                      ) : (
                        <>
                          <IconAction label={t("trackerCustomizations.edit")} onClick={() => openEditDialog(customizationId, displayName, domains)}>
                            <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
                          </IconAction>
                          <IconAction label={t("trackerCustomizations.delete")} onClick={() => handleDelete(customizationId)}>
                            <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
                          </IconAction>
                        </>
                      )
                    ) : (
                      <IconAction
                        label={selectedGroupId !== null ? t("trackerCustomizations.mergeIntoGroup") : selectedDomains.size > 0 ? t("trackerCustomizations.addToMerge") : t("trackerCustomizations.rename")}
                        onClick={() => selectedGroupId !== null ? handleMergeIntoGroup(selectedGroupId, domain) : openRenameDialog(domain)}
                        disabled={saving && (selectedGroupId !== null || selectedDomains.size > 0)}
                      >
                        {selectedGroupId !== null || selectedDomains.size > 0 ? <Link2 className="h-3.5 w-3.5 text-primary" /> : <Pencil className="h-3.5 w-3.5 text-muted-foreground" />}
                      </IconAction>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>

      <Dialog open={customizeOpen} onOpenChange={(open) => !open && closeCustomizeDialog()}>
        {customizeOpen && (
          <CustomizeDialogContent
            mode={customizeMode}
            initialName={editingCustomization?.displayName ?? ""}
            domains={editingCustomization ? editingCustomization.domains : Array.from(selectedDomains)}
            incognitoMode={incognitoMode}
            saving={saving}
            onRemoveDomain={handleRemoveDomainFromDialog}
            onSave={handleSaveCustomization}
            onCancel={closeCustomizeDialog}
          />
        )}
      </Dialog>

      <Dialog open={importOpen} onOpenChange={setImportOpen}>
        {importOpen && (
          <ImportDialogContent
            customizations={customizations ?? []}
            createCustomization={createCustomization}
            updateCustomization={updateCustomization}
            onDone={() => setImportOpen(false)}
          />
        )}
      </Dialog>
    </Card>
  )
}

interface CustomizeDialogContentProps {
  mode: CustomizeMode
  initialName: string
  domains: string[]
  incognitoMode: boolean
  saving: boolean
  onRemoveDomain: (domain: string) => void
  onSave: (displayName: string) => void
  onCancel: () => void
}

function CustomizeDialogContent({ mode, initialName, domains, incognitoMode, saving, onRemoveDomain, onSave, onCancel }: CustomizeDialogContentProps) {
  const { t } = useTranslation("settings")
  const [displayName, setDisplayName] = useState(initialName)
  const title = { edit: t("trackerCustomizations.customizeDialog.editTitle"), rename: t("trackerCustomizations.customizeDialog.renameTitle"), merge: t("trackerCustomizations.customizeDialog.mergeTitle") }[mode]
  const description = { edit: t("trackerCustomizations.customizeDialog.editDescription"), rename: t("trackerCustomizations.customizeDialog.renameDescription"), merge: t("trackerCustomizations.customizeDialog.mergeDescription") }[mode]
  const action = { edit: t("trackerCustomizations.customizeDialog.save"), rename: t("trackerCustomizations.customizeDialog.renameAction"), merge: t("trackerCustomizations.customizeDialog.mergeAction") }[mode]

  return (
    <DialogContent className="max-h-[90dvh] flex flex-col">
      <DialogHeader className="flex-shrink-0">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <div className="space-y-4 py-4 min-h-0 flex-1 flex flex-col">
        <div className="space-y-2">
          <Label htmlFor="customize-name">{t("trackerCustomizations.customizeDialog.displayName")}</Label>
          <Input
            id="customize-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t("trackerCustomizations.customizeDialog.displayNamePlaceholder")}
          />
        </div>
        <div className="space-y-2 min-h-0 flex-1 flex flex-col overflow-hidden">
          <Label>{mode === "edit" ? t("trackerCustomizations.customizeDialog.domains") : t("trackerCustomizations.customizeDialog.selectedTrackers")}</Label>
          <ScrollArea className="h-[300px]">
            <div className="text-sm text-muted-foreground space-y-1.5 pr-4">
              {domains.map((domain, index) => {
                const hasMultiple = domains.length > 1
                const isPrimary = index === 0
                const shownDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
                return (
                  <div key={domain} className={`grid items-center gap-2 ${hasMultiple ? "grid-cols-[1fr_auto_auto]" : "grid-cols-[1fr]"}`}>
                    <span className={`truncate${isPrimary ? " font-medium" : ""}`} title={shownDomain}>{shownDomain}</span>
                    {hasMultiple && (isPrimary ? <Badge variant="secondary" className="text-[10px]">{t("trackerCustomizations.customizeDialog.primary")}</Badge> : <span />)}
                    {hasMultiple && (
                      <button
                        type="button"
                        onClick={() => onRemoveDomain(domain)}
                        aria-label={t("trackerCustomizations.customizeDialog.removeDomain", { domain: shownDomain })}
                        className="text-muted-foreground hover:text-destructive cursor-pointer"
                      >
                        <X className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          </ScrollArea>
        </div>
      </div>
      <DialogFooter className="flex-shrink-0">
        <Button variant="outline" onClick={onCancel}>
          {t("trackerCustomizations.customizeDialog.cancel")}
        </Button>
        <Button onClick={() => onSave(displayName.trim())} disabled={!displayName.trim() || saving}>
          {saving ? t("trackerCustomizations.customizeDialog.saving") : action}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}

interface ImportDialogContentProps {
  customizations: TrackerCustomization[]
  // The mutations live in the parent, so a reopened dialog still sees a running import.
  createCustomization: ReturnType<typeof useCreateTrackerCustomization>
  updateCustomization: ReturnType<typeof useUpdateTrackerCustomization>
  onDone: () => void
}

function ImportDialogContent({ customizations, createCustomization, updateCustomization, onDone }: ImportDialogContentProps) {
  const { t } = useTranslation("settings")
  const [importJson, setImportJson] = useState("")
  const [conflictChoices, setConflictChoices] = useState<Map<number, "skip" | "overwrite">>(new Map())

  const parse = useMemo(() => parseTrackerCustomizationsImport(importJson, customizations), [importJson, customizations])
  const errorText: Record<ImportError, string> = {
    invalidJson: t("trackerCustomizations.importDialog.invalidJson"),
    invalidFormat: t("trackerCustomizations.importDialog.invalidFormat"),
    missingDisplayName: t("trackerCustomizations.importDialog.missingDisplayName"),
    invalidDomains: t("trackerCustomizations.importDialog.invalidDomains"),
  }
  const entries = parse.valid ? parse.entries : []
  const conflicts = entries.filter(e => e.conflict && !e.isIdentical)
  const newCount = entries.filter(e => !e.conflict).length
  const unchangedCount = entries.filter(e => e.isIdentical).length
  const allConflictsResolved = parse.valid && conflicts.every(e => conflictChoices.has(e.index))
  const importing = createCustomization.isPending || updateCustomization.isPending

  const handleImport = async () => {
    let imported = 0
    let skipped = 0
    const failed: string[] = []

    for (const entry of entries) {
      if (entry.isIdentical || (entry.conflict && conflictChoices.get(entry.index) !== "overwrite")) {
        skipped++
        continue
      }
      try {
        if (entry.conflict) {
          await updateCustomization.mutateAsync({ id: entry.conflict.id, data: toCustomizationInput(entry) })
        } else {
          await createCustomization.mutateAsync(toCustomizationInput(entry))
        }
        imported++
      } catch (error) {
        console.error(`[Import] Failed to import "${entry.displayName}":`, error)
        failed.push(entry.displayName)
      }
    }

    onDone()

    if (failed.length > 0) {
      toast.error(t("trackerCustomizations.toasts.failedToImport", { names: failed.join(", ") }))
    } else if (imported > 0 && skipped > 0) {
      toast.success(t("trackerCustomizations.toasts.importedAndSkipped", { imported, skipped }))
    } else if (imported > 0) {
      toast.success(t("trackerCustomizations.toasts.importedCount", { count: imported }))
    } else {
      toast.info(t("trackerCustomizations.toasts.noCustomizationsImported"))
    }
  }

  const setChoice = (index: number, choice: "skip" | "overwrite") => setConflictChoices(prev => new Map(prev).set(index, choice))

  return (
    <DialogContent className="sm:max-w-lg max-h-[90dvh] flex flex-col">
      <DialogHeader className="flex-shrink-0">
        <DialogTitle>{t("trackerCustomizations.importDialog.title")}</DialogTitle>
        <DialogDescription>{t("trackerCustomizations.importDialog.description")}</DialogDescription>
      </DialogHeader>
      <div className="flex-1 overflow-y-auto min-h-0 space-y-4 py-4">
        <div className="space-y-2">
          <Label htmlFor="import-json">{t("trackerCustomizations.importDialog.jsonData")}</Label>
          <Textarea
            id="import-json"
            value={importJson}
            onChange={(e) => {
              setImportJson(e.target.value)
              setConflictChoices(new Map())
            }}
            placeholder={"{\n  \"trackerCustomizations\": [\n    { \"displayName\": \"Name\", \"domains\": [\"domain.com\"] }\n  ]\n}"}
            className="font-mono text-xs h-32"
          />
        </div>

        {!parse.valid && parse.error && (
          <div className="flex items-center gap-2 text-destructive text-sm">
            <AlertTriangle className="h-4 w-4" />
            {errorText[parse.error]}
          </div>
        )}
        {parse.valid && (
          <div className="space-y-2">
            <div className="text-sm text-muted-foreground">
              {newCount > 0 && <span>{t("trackerCustomizations.importDialog.newCount", { count: newCount })}</span>}
              {newCount > 0 && (conflicts.length > 0 || unchangedCount > 0) && <span>, </span>}
              {conflicts.length > 0 && <span className="text-yellow-600">{t("trackerCustomizations.importDialog.conflictCount", { count: conflicts.length })}</span>}
              {conflicts.length > 0 && unchangedCount > 0 && <span>, </span>}
              {unchangedCount > 0 && <span>{t("trackerCustomizations.importDialog.unchangedCount", { count: unchangedCount })}</span>}
            </div>
            {conflicts.length > 0 && (
              <>
                <Label>{t("trackerCustomizations.importDialog.resolveConflicts")}</Label>
                <div className="border rounded-md max-h-48 overflow-y-auto">
                  {conflicts.map(entry => (
                    <div key={entry.index} className="px-3 py-2 text-sm border-b last:border-b-0 bg-yellow-500/10">
                      <div className="flex items-center justify-between gap-2">
                        <div className="min-w-0 flex-1">
                          <div className="font-medium truncate">{entry.displayName}</div>
                          <div className="text-xs text-muted-foreground truncate">{entry.domains.join(", ")}</div>
                          <div className="text-xs text-yellow-600 mt-1">
                            {t("trackerCustomizations.importDialog.conflictsWith", { name: entry.conflict?.displayName })}
                          </div>
                        </div>
                        <div className="flex items-center gap-1 shrink-0">
                          <Button
                            variant={conflictChoices.get(entry.index) === "skip" ? "secondary" : "ghost"}
                            size="sm"
                            className="h-6 px-2 text-xs"
                            onClick={() => setChoice(entry.index, "skip")}
                          >
                            {t("trackerCustomizations.importDialog.skip")}
                          </Button>
                          <Button
                            variant={conflictChoices.get(entry.index) === "overwrite" ? "secondary" : "ghost"}
                            size="sm"
                            className="h-6 px-2 text-xs"
                            onClick={() => setChoice(entry.index, "overwrite")}
                          >
                            {t("trackerCustomizations.importDialog.overwrite")}
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
      </div>
      <DialogFooter className="flex-shrink-0">
        <Button variant="outline" onClick={onDone}>
          {t("trackerCustomizations.importDialog.cancel")}
        </Button>
        <Button onClick={handleImport} disabled={!allConflictsResolved || importing}>
          {importing ? t("trackerCustomizations.importDialog.importing") : t("trackerCustomizations.importDialog.import")}
        </Button>
      </DialogFooter>
    </DialogContent>
  )
}

function IconAction({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" className="size-11 sm:size-8" onClick={onClick} disabled={disabled} aria-label={label}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
