/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQueries } from "@tanstack/react-query"
import { Download, Link2, Pencil, Trash2, Upload, X } from "lucide-react"
import { useId, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList, type ResourceRow } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { SelectionBar } from "@/components/kit/SelectionBar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
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
  type ImportError,
  type TrackerRow
} from "@/lib/tracker-customizations-manage"
import { cn, copyTextToClipboard } from "@/lib/utils"
import type { TrackerCustomization } from "@/types"

type CustomizeMode = "edit" | "rename" | "merge"

export function TrackerCustomizationsManager() {
  const { t } = useTranslation("settings")
  const { data: trackerIcons } = useTrackerIcons()
  const [incognitoMode] = useIncognitoMode()
  const { instances } = useInstances()

  const customizationsQuery = useTrackerCustomizations()
  const customizations = customizationsQuery.data
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
  const selectedDomains = useMemo(() => {
    const standalone = new Set(rows.filter(row => row.customizationId === undefined).map(row => row.domain))
    return new Set([...selectedDomainState].filter(domain => standalone.has(domain)))
  }, [selectedDomainState, rows])
  if (selectedDomains.size !== selectedDomainState.size) setSelectedDomains(selectedDomains)
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null)
  const [customizeOpen, setCustomizeOpen] = useState(false)
  const [editingCustomization, setEditingCustomization] = useState<{ id: number; displayName: string; domains: string[] } | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [importSession, setImportSession] = useState(0)

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

  const handleDelete = (customizationId: number) =>
    deleteCustomization.mutateAsync(customizationId).then(() => setSelectedGroupId(prev => prev === customizationId ? null : prev))

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

  const reorderDomainsForIcons = (domains: string[]): string[] => {
    if (!trackerIcons || domains.length <= 1) return domains
    const hasIcon = (d: string) => Boolean(trackerIcons[d.toLowerCase()] || trackerIcons[d])
    return [...domains.filter(hasIcon), ...domains.filter(d => !hasIcon(d))]
  }

  const openEditDialog = (customizationId: number, displayName: string, domains: string[]) => {
    setEditingCustomization({ id: customizationId, displayName, domains })
    setCustomizeOpen(true)
  }

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

  const openImport = () => {
    setImportSession(session => session + 1)
    setImportOpen(true)
  }

  const saving = createCustomization.isPending || updateCustomization.isPending
  const customizeMode: CustomizeMode = editingCustomization ? "edit" : selectedDomains.size === 1 ? "rename" : "merge"
  const empty = {
    title: trackersLoading ? t("trackerCustomizations.loading") : t("trackerCustomizations.emptyTitle"),
    description: trackersLoading ? undefined : t("trackerCustomizations.emptyDescription"),
  }

  const resourceRow = (row: TrackerRow): ResourceRow => {
    const { domain, displayName, domains, customizationId } = row
    const shownDomains = incognitoMode ? domains.map(getLinuxTrackerDomain) : domains
    const base: ResourceRow = {
      title: incognitoMode ? getLinuxTrackerDomain(displayName) : displayName,
      icon: <TrackerIconImage tracker={incognitoMode ? getLinuxTrackerDomain(domain) : domain} trackerIcons={trackerIcons} />,
      meta: domains.length > 1
        ? [t("trackerCustomizations.mergedFrom", { domains: shownDomains.join(", ") })]
        : customizationId !== undefined && displayName !== domain ? [t("trackerCustomizations.original", { domain: shownDomains[0] })] : undefined,
      select: customizationId === undefined
        ? { checked: selectedDomains.has(domain), onCheckedChange: () => toggleSelection(domain) }
        : selectedGroupId !== null && selectedGroupId !== customizationId
          ? undefined
          : { checked: selectedGroupId === customizationId, onCheckedChange: () => toggleGroupSelection(customizationId) },
    }

    if (customizationId === undefined) {
      const merging = selectedGroupId !== null || selectedDomains.size > 0
      return {
        ...base,
        actions: [{
          label: selectedGroupId !== null ? t("trackerCustomizations.mergeIntoGroup") : merging ? t("trackerCustomizations.addToMerge") : t("trackerCustomizations.rename"),
          icon: merging ? Link2 : Pencil,
          disabled: saving && merging,
          onSelect: () => selectedGroupId !== null ? handleMergeIntoGroup(selectedGroupId, domain) : openRenameDialog(domain),
        }],
      }
    }

    if (selectedDomains.size > 0 && (selectedGroupId === null || selectedGroupId === customizationId)) {
      return {
        ...base,
        actions: [{ label: t("trackerCustomizations.mergeTooltip"), icon: Link2, disabled: saving, onSelect: () => handleMergeIntoGroup(customizationId) }],
      }
    }

    return {
      ...base,
      onOpen: () => openEditDialog(customizationId, displayName, domains),
      actions: [{
        label: t("trackerCustomizations.delete"),
        icon: Trash2,
        destructive: true,
        pending: deleteCustomization.isPending && deleteCustomization.variables === customizationId,
        onSelect: () => handleDelete(customizationId),
        confirm: {
          title: t("trackerCustomizations.deleteDialog.title"),
          description: t("trackerCustomizations.deleteDialog.description", { name: base.title }),
          confirmLabel: t("trackerCustomizations.delete"),
        },
      }],
    }
  }

  const picked = selectedDomains.size + (selectedGroupId === null ? 0 : 1)
  const primary = selectedDomains.size === 0
    ? undefined
    : selectedGroupId !== null
      ? { label: t("trackerCustomizations.mergeIntoGroup"), onClick: () => handleMergeIntoGroup(selectedGroupId) }
      : {
        label: selectedDomains.size === 1 ? t("trackerCustomizations.rename") : t("trackerCustomizations.customizeDialog.mergeAction"),
        onClick: () => openRenameDialog([...selectedDomains][0]),
      }

  return (
    <>
      <Section
        title={t("trackerCustomizations.cardTitle")}
        description={t("trackerCustomizations.cardDescription")}
        help={t("trackerCustomizations.cardHelp")}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={openImport} disabled={saving}>
              <Download />
              {t("trackerCustomizations.importButton")}
            </Button>
            <Button variant="outline" size="sm" onClick={handleExport} disabled={!customizations || customizations.length === 0}>
              <Upload />
              {t("trackerCustomizations.exportButton")}
            </Button>
          </>
        }
        queries={[customizationsQuery]}
      >
        {rows.length === 0 ? (
          <ResourceList label={t("trackerCustomizations.cardTitle")} items={[]} itemKey={() => ""} row={resourceRow} empty={empty} />
        ) : (
          <div className="-mx-6 max-sm:-mx-4">
            <div className={cn("px-6 max-sm:px-4", picked > 0 && "pb-4")}>
              <ResourceList
                label={t("trackerCustomizations.cardTitle")}
                items={rows}
                itemKey={row => row.customizationId !== undefined ? `c-${row.customizationId}` : `d-${row.domain}`}
                row={resourceRow}
                empty={empty}
              />
            </div>
            {picked > 0 && (
              <SelectionBar label={t("trackerCustomizations.selectionLabel")} summary={t("trackerCustomizations.selectedCount", { count: picked })} onClear={clearSelection}>
                {primary && <Button size="sm" disabled={saving} onClick={primary.onClick}>{primary.label}</Button>}
              </SelectionBar>
            )}
          </div>
        )}
      </Section>

      {customizeOpen && (
        <CustomizeEditor
          mode={customizeMode}
          initialName={editingCustomization?.displayName ?? ""}
          domains={editingCustomization ? editingCustomization.domains : Array.from(selectedDomains)}
          incognitoMode={incognitoMode}
          saving={saving}
          onRemoveDomain={handleRemoveDomainFromDialog}
          onSave={handleSaveCustomization}
          onClose={closeCustomizeDialog}
        />
      )}

      <ImportEditor
        key={importSession}
        open={importOpen}
        onOpenChange={setImportOpen}
        customizations={customizations ?? []}
        createCustomization={createCustomization}
        updateCustomization={updateCustomization}
      />
    </>
  )
}

interface CustomizeEditorProps {
  mode: CustomizeMode
  initialName: string
  domains: string[]
  incognitoMode: boolean
  saving: boolean
  onRemoveDomain: (domain: string) => void
  onSave: (displayName: string) => void
  onClose: () => void
}

function CustomizeEditor({ mode, initialName, domains, incognitoMode, saving, onRemoveDomain, onSave, onClose }: CustomizeEditorProps) {
  const { t } = useTranslation("settings")
  const domainsLabelId = useId()
  const [displayName, setDisplayName] = useState(initialName)
  const [submitted, setSubmitted] = useState(false)
  const title = { edit: t("trackerCustomizations.customizeDialog.editTitle"), rename: t("trackerCustomizations.customizeDialog.renameTitle"), merge: t("trackerCustomizations.customizeDialog.mergeTitle") }[mode]
  const description = { edit: t("trackerCustomizations.customizeDialog.editDescription"), rename: t("trackerCustomizations.customizeDialog.renameDescription"), merge: t("trackerCustomizations.customizeDialog.mergeDescription") }[mode]
  const action = { edit: t("trackerCustomizations.customizeDialog.save"), rename: t("trackerCustomizations.customizeDialog.renameAction"), merge: t("trackerCustomizations.customizeDialog.mergeAction") }[mode]

  return (
    <Editor
      open
      onOpenChange={(open) => !open && onClose()}
      title={title}
      description={description}
      submitLabel={action}
      onSubmit={() => {
        setSubmitted(true)
        onSave(displayName.trim())
      }}
      pending={submitted && saving}
      submitDisabled={!displayName.trim() || saving}
    >
      <Field label={t("trackerCustomizations.customizeDialog.displayName")}>
        <Input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("trackerCustomizations.customizeDialog.displayNamePlaceholder")}
          autoComplete="off"
        />
      </Field>
      <div role="group" aria-labelledby={domainsLabelId} className="space-y-2">
        <p id={domainsLabelId} className="text-sm font-medium leading-5">
          {mode === "edit" ? t("trackerCustomizations.customizeDialog.domains") : t("trackerCustomizations.customizeDialog.selectedTrackers")}
        </p>
        <ul className="divide-y rounded-lg border">
          {domains.map((domain, index) => {
            const shownDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
            return (
              <li key={domain} className="flex min-h-11 items-center gap-2 py-1 pr-1 pl-3">
                <span className={cn("min-w-0 flex-1 text-sm [overflow-wrap:anywhere]", index === 0 && "font-medium")}>{shownDomain}</span>
                {domains.length > 1 && index === 0 && <Badge variant="secondary">{t("trackerCustomizations.customizeDialog.primary")}</Badge>}
                {domains.length > 1 && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => onRemoveDomain(domain)}
                    aria-label={t("trackerCustomizations.customizeDialog.removeDomain", { domain: shownDomain })}
                    className="size-8 shrink-0 text-muted-foreground hover:text-destructive max-sm:size-10"
                  >
                    <X />
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      </div>
    </Editor>
  )
}

interface ImportEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  customizations: TrackerCustomization[]
  createCustomization: ReturnType<typeof useCreateTrackerCustomization>
  updateCustomization: ReturnType<typeof useUpdateTrackerCustomization>
}

function ImportEditor({ open, onOpenChange, customizations, createCustomization, updateCustomization }: ImportEditorProps) {
  const { t } = useTranslation("settings")
  const conflictsLabelId = useId()
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

    onOpenChange(false)

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

  const summaryParts = [
    newCount > 0 && t("trackerCustomizations.importDialog.newCount", { count: newCount }),
    conflicts.length > 0 && (
      <span key="conflicts" className="text-amber-600 dark:text-amber-500">
        {t("trackerCustomizations.importDialog.conflictCount", { count: conflicts.length })}
      </span>
    ),
    unchangedCount > 0 && t("trackerCustomizations.importDialog.unchangedCount", { count: unchangedCount }),
  ].filter(Boolean)
  const summary = summaryParts.length > 0 ? <>{summaryParts.flatMap((part, index) => index === 0 ? [part] : [", ", part])}</> : undefined

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t("trackerCustomizations.importDialog.title")}
      description={t("trackerCustomizations.importDialog.description")}
      submitLabel={importing ? t("trackerCustomizations.importDialog.importing") : t("trackerCustomizations.importDialog.import")}
      onSubmit={() => void handleImport()}
      submitDisabled={!allConflictsResolved || importing}
    >
      <Field
        label={t("trackerCustomizations.importDialog.jsonData")}
        error={!parse.valid && parse.error ? errorText[parse.error] : undefined}
        description={summary}
      >
        <Textarea
          value={importJson}
          onChange={(e) => {
            setImportJson(e.target.value)
            setConflictChoices(new Map())
          }}
          placeholder={"{\n  \"trackerCustomizations\": [\n    { \"displayName\": \"Name\", \"domains\": [\"domain.com\"] }\n  ]\n}"}
          autoCapitalize="off"
          spellCheck={false}
          className="max-h-72 min-h-32 font-mono md:text-xs"
        />
      </Field>
      {conflicts.length > 0 && (
        <div role="group" aria-labelledby={conflictsLabelId} className="space-y-2">
          <p id={conflictsLabelId} className="text-sm font-medium leading-5">{t("trackerCustomizations.importDialog.resolveConflicts")}</p>
          <ul className="divide-y rounded-lg border">
            {conflicts.map(entry => {
              const choice = conflictChoices.get(entry.index)
              return (
                <li key={entry.index} className="flex gap-3 px-3 py-2.5 max-sm:flex-col sm:items-center">
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium [overflow-wrap:anywhere]">{entry.displayName}</p>
                    <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{entry.domains.join(", ")}</p>
                    <p className="mt-0.5 text-xs text-amber-600 dark:text-amber-500 [overflow-wrap:anywhere]">
                      {t("trackerCustomizations.importDialog.conflictsWith", { name: entry.conflict?.displayName })}
                    </p>
                  </div>
                  <div role="group" aria-label={entry.displayName} className="flex shrink-0 gap-0.5 rounded-md border p-0.5 max-sm:*:flex-1">
                    {(["skip", "overwrite"] as const).map(option => (
                      <Button
                        key={option}
                        type="button"
                        variant={choice === option ? "secondary" : "ghost"}
                        size="sm"
                        aria-pressed={choice === option}
                        className="h-7 px-2.5 text-xs max-sm:h-9"
                        onClick={() => setChoice(entry.index, option)}
                      >
                        {option === "skip" ? t("trackerCustomizations.importDialog.skip") : t("trackerCustomizations.importDialog.overwrite")}
                      </Button>
                    ))}
                  </div>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </Editor>
  )
}
