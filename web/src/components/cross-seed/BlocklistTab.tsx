/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Ban, Copy, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import { Editor } from "@/components/kit/Editor"
import { Field } from "@/components/kit/Field"
import { ResourceList } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { api } from "@/lib/api"
import { getLinuxIsoName, getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { buildTrackerCustomizationLookup, resolveTrackerDisplay } from "@/lib/tracker-customizations"
import { copyTextToClipboard } from "@/lib/utils"
import type { CrossSeedBlocklistEntry, Instance } from "@/types"

interface BlocklistTabProps {
  instances: Instance[]
}

type BlocklistPayload = { instanceId: number; infoHash: string; note?: string }

type EditorState = { mode: "closed" } | { mode: "add" } | { mode: "edit"; entry: CrossSeedBlocklistEntry }

const infoHashRegex = /^[a-f0-9]{40}$|^[a-f0-9]{64}$/

export function BlocklistTab({ instances }: BlocklistTabProps) {
  const { t } = useTranslation("crossseed")
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()
  const { data: trackerCustomizations } = useTrackerCustomizations()
  const { data: trackerIcons } = useTrackerIcons()
  const [incognitoMode] = useIncognitoMode()
  const trackerLookup = useMemo(() => buildTrackerCustomizationLookup(trackerCustomizations), [trackerCustomizations])

  const [instanceId, setInstanceId] = useState<number | null>(null)
  const [editor, setEditor] = useState<EditorState>({ mode: "closed" })
  const [editorSession, setEditorSession] = useState(0)

  if (!instances.some((instance) => instance.id === instanceId)) {
    const fallbackId = instances[0]?.id ?? null
    if (fallbackId !== instanceId) {
      setInstanceId(fallbackId)
    }
  }

  const blocklistQuery = useQuery({
    queryKey: ["cross-seed", "blocklist", instanceId],
    queryFn: () => instanceId ? api.listCrossSeedBlocklist(instanceId) : Promise.resolve([]),
    enabled: instanceId !== null,
  })

  const saveMutation = useMutation({
    mutationFn: (payload: BlocklistPayload) => api.addCrossSeedBlocklist(payload),
    onSuccess: () => {
      toast.success(t(editor.mode === "edit" ? "blocklist.toast.noteSaved" : "blocklist.toast.addedToBlocklist"))
      setEditor({ mode: "closed" })
      void queryClient.invalidateQueries({ queryKey: ["cross-seed", "blocklist"] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (entry: CrossSeedBlocklistEntry) => api.deleteCrossSeedBlocklist(entry.instanceId, entry.infoHash),
    onSuccess: () => {
      toast.success(t("blocklist.toast.removedFromBlocklist"))
      void queryClient.invalidateQueries({ queryKey: ["cross-seed", "blocklist"] })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const openEditor = (next: EditorState) => {
    saveMutation.reset()
    setEditorSession((session) => session + 1)
    setEditor(next)
  }

  const copyHash = async (hash: string) => {
    await copyTextToClipboard(hash)
    toast.success(t("blocklist.toast.hashCopied"))
  }

  const describe = (entry: CrossSeedBlocklistEntry) => {
    const name = entry.torrentName ? (incognitoMode ? getLinuxIsoName(entry.infoHash) : entry.torrentName) : ""
    const tracker = entry.tracker ? (incognitoMode ? getLinuxTrackerDomain(entry.tracker) : entry.tracker) : ""
    const trackerName = tracker && !incognitoMode ? resolveTrackerDisplay(tracker, trackerLookup).displayName : tracker
    return { name, tracker, trackerName }
  }

  if (instances.length === 0) {
    return <Section title={t("blocklist.sectionTitle")} description={t("blocklist.noInstancesDescription")} />
  }

  const instanceName = instances.find((instance) => instance.id === instanceId)?.name ?? ""
  const pendingDelete = deleteMutation.isPending ? deleteMutation.variables : undefined

  return (
    <>
      <Section
        title={t("blocklist.sectionTitle")}
        description={t("blocklist.sectionDescription")}
        actions={
          <>
            {instances.length > 1 && (
              <Select value={instanceId?.toString() ?? ""} onValueChange={(value) => setInstanceId(Number(value))}>
                <SelectTrigger size="sm" aria-label={t("blocklist.instanceLabel")} className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {instances.map((instance) => (
                    <SelectItem key={instance.id} value={instance.id.toString()}>{instance.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            <Button variant="outline" size="sm" onClick={() => openEditor({ mode: "add" })}>
              <Plus />
              {t("blocklist.blockHash")}
            </Button>
          </>
        }
        queries={[blocklistQuery]}
      >
        <ResourceList
          label={t("blocklist.sectionTitle")}
          items={blocklistQuery.data ?? []}
          itemKey={(entry) => `${entry.instanceId}-${entry.infoHash}`}
          row={(entry) => {
            const { name, tracker, trackerName } = describe(entry)
            const isDeleting = pendingDelete?.instanceId === entry.instanceId && pendingDelete.infoHash === entry.infoHash
            return {
              title: name || entry.infoHash,
              icon: tracker ? <TrackerIconImage tracker={tracker} trackerIcons={trackerIcons} /> : Ban,
              meta: [
                ...(trackerName ? [trackerName] : []),
                ...(entry.note ? [<span key="note" className="text-foreground/80">{entry.note}</span>] : []),
                t("blocklist.blockedAt", { date: formatDate(new Date(entry.createdAt)) }),
              ],
              onOpen: () => openEditor({ mode: "edit", entry }),
              actions: [
                { label: t("blocklist.copyHash"), icon: Copy, onSelect: () => copyHash(entry.infoHash) },
                {
                  label: t("blocklist.remove"),
                  icon: Trash2,
                  destructive: true,
                  pending: isDeleting,
                  onSelect: () => deleteMutation.mutateAsync(entry),
                  confirm: {
                    title: t("blocklist.removeDialog.title"),
                    description: t("blocklist.removeDialog.description", { name: name || entry.infoHash, instance: instanceName }),
                    confirmLabel: t("blocklist.remove"),
                  },
                },
              ],
            }
          }}
          empty={{
            title: t("blocklist.emptyTitle"),
            description: t("blocklist.emptyDescription"),
            onAdd: () => openEditor({ mode: "add" }),
          }}
        />
      </Section>
      <BlocklistEditor
        key={editorSession}
        state={editor}
        instances={instances}
        defaultInstanceId={instanceId}
        entryName={editor.mode === "edit" ? describe(editor.entry).name : ""}
        pending={saveMutation.isPending}
        error={saveMutation.error?.message}
        onClose={() => setEditor({ mode: "closed" })}
        onSave={(payload) => saveMutation.mutate(payload)}
      />
    </>
  )
}

interface BlocklistEditorProps {
  state: EditorState
  instances: Instance[]
  defaultInstanceId: number | null
  entryName: string
  pending: boolean
  error?: string
  onClose: () => void
  onSave: (payload: BlocklistPayload) => void
}

function BlocklistEditor({ state, instances, defaultInstanceId, entryName, pending, error, onClose, onSave }: BlocklistEditorProps) {
  const { t } = useTranslation("crossseed")
  const entry = state.mode === "edit" ? state.entry : null
  const [instanceId, setInstanceId] = useState(entry?.instanceId ?? defaultInstanceId)
  const [infoHash, setInfoHash] = useState(entry?.infoHash ?? "")
  const [note, setNote] = useState(entry?.note ?? "")
  const [hashError, setHashError] = useState<string | undefined>()

  const submit = () => {
    const hash = infoHash.trim().toLowerCase()
    if (!infoHashRegex.test(hash)) {
      setHashError(t("blocklist.toast.invalidInfohash"))
      return
    }
    if (!instanceId) return
    onSave({ instanceId, infoHash: hash, note: note.trim() || undefined })
  }

  return (
    <Editor
      open={state.mode !== "closed"}
      onOpenChange={(open) => !open && onClose()}
      title={t(entry ? "blocklist.editDialog.title" : "blocklist.addDialog.title")}
      description={entry ? (
        <span className="block space-y-0.5 [overflow-wrap:anywhere]">
          {entryName && <span className="block">{entryName}</span>}
          <span className="block font-mono text-xs">{entry.infoHash}</span>
        </span>
      ) : t("blocklist.addDialog.description")}
      submitLabel={t(entry ? "blocklist.editDialog.save" : "blocklist.addDialog.submit")}
      onSubmit={submit}
      pending={pending}
      error={error}
    >
      {!entry && instances.length > 1 && (
        <Field label={t("blocklist.instanceLabel")}>
          {(control) => (
            <Select value={instanceId?.toString() ?? ""} onValueChange={(value) => setInstanceId(Number(value))}>
              <SelectTrigger {...control} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {instances.map((instance) => (
                  <SelectItem key={instance.id} value={instance.id.toString()}>{instance.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
      )}
      {!entry && (
        <Field label={t("blocklist.infohashLabel")} error={hashError}>
          <Input
            value={infoHash}
            placeholder={t("blocklist.infohashPlaceholder")}
            autoComplete="off"
            spellCheck={false}
            className="font-mono"
            onChange={(event) => {
              setInfoHash(event.target.value)
              setHashError(undefined)
            }}
          />
        </Field>
      )}
      <Field label={t("blocklist.noteLabel")}>
        <Input value={note} placeholder={t("blocklist.notePlaceholder")} autoComplete="off" onChange={(event) => setNote(event.target.value)} />
      </Field>
    </Editor>
  )
}
