/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { Plus } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { InstanceForm } from "@/components/instances/InstanceForm"
import { InstanceList } from "@/components/instances/InstanceList"
import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { InstanceMarkEditor, InstanceMarkSummary } from "@/components/instances/marks/InstanceMarkEditor"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { InstancePreferencesDialog } from "@/components/instances/preferences/InstancePreferencesDialog"
import { ResourceList } from "@/components/kit/ResourceList"
import { Section } from "@/components/kit/Section"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Button } from "@/components/ui/button"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { api } from "@/lib/api"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { autoLettersFor } from "@/lib/instance-marks"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import type { SettingsSearch } from "@/routes/_authenticated/settings"

interface InstancesManagerProps {
  search: SettingsSearch
  onSearchChange: (search: SettingsSearch) => void
}

/** The Instances tab: the qBittorrent instances, the add form that `?modal=add-instance` opens, and the speeds in the browser tab. */
export function InstancesManager({ search, onSearchChange }: InstancesManagerProps) {
  const { t } = useTranslation("settings")
  const { t: tCommon } = useTranslation("common")
  // The same query as useInstances, which the list and the forms use for their changes.
  const instancesQuery = useQuery({ queryKey: ["instances"], queryFn: () => api.getInstances() })
  const instances = instancesQuery.data ?? []
  const [editingId, setEditingId] = useState<number | null>(null)
  const editing = instances.find((instance) => instance.id === editingId)
  const [markEditing, setMarkEditing] = useState<{ target: MarkTarget; open: boolean } | null>(null)
  const sidebarShowsUnified = instances.filter((instance) => instance.isActive).length > 1
  const markTargets: MarkTarget[] = [
    ...(sidebarShowsUnified
      ? [{ id: ALL_INSTANCES_ID, name: tCommon("unifiedScope.unified"), autoLetters: "", inactive: false }]
      : []),
    ...instances.map((instance) => ({
      id: instance.id,
      name: instance.name,
      autoLetters: autoLettersFor(instances, instance.id),
      inactive: !instance.isActive,
    })),
  ]
  const openMarkEditor = (target: MarkTarget) => setMarkEditing({ target, open: true })

  const addOpen = search.tab === "instances" && search.modal === "add-instance"
  // Each open of the add form starts from empty fields. The form keeps its key while it closes, so it can fade out.
  const [addSession, setAddSession] = useState(0)
  const [wasAddOpen, setWasAddOpen] = useState(addOpen)
  if (addOpen !== wasAddOpen) {
    setWasAddOpen(addOpen)
    if (addOpen) setAddSession((session) => session + 1)
  }

  const openAdd = () => onSearchChange({ ...search, tab: "instances", modal: "add-instance" })
  const closeAdd = () => onSearchChange({ tab: "instances" })

  return (
    <>
      <Section
        title={t("instances.title")}
        description={t("instances.description")}
        actions={
          <Button variant="outline" size="sm" onClick={openAdd}>
            <Plus />
            {t("instances.addButton")}
          </Button>
        }
        queries={[instancesQuery]}
      >
        <PasswordIssuesBanner instances={instances} className="mb-0" />
        <InstanceList
          instances={instances}
          onEdit={(instance) => setEditingId(instance.id)}
          onChangeIcon={(instance) => {
            const target = markTargets.find((entry) => entry.id === instance.id)
            if (target) openMarkEditor(target)
          }}
          onAdd={openAdd}
        />
      </Section>

      {instances.length > 0 && <InstanceIconsSection targets={markTargets} onOpen={openMarkEditor} />}

      <TitleBarSpeedsSection />

      <InstanceForm key={addSession} open={addOpen} onOpenChange={(open) => open ? openAdd() : closeAdd()} onSuccess={closeAdd} />

      {markEditing && (
        <InstanceMarkEditor
          key={`mark-${markEditing.target.id}`}
          open={markEditing.open}
          onOpenChange={(open) => setMarkEditing((prev) => prev && { ...prev, open })}
          instanceId={markEditing.target.id}
          name={markEditing.target.name}
          autoLetters={markEditing.target.autoLetters}
        />
      )}

      {editing && (
        <InstancePreferencesDialog
          open={true}
          onOpenChange={(open) => !open && setEditingId(null)}
          instanceId={editing.id}
          instanceName={editing.name}
          instance={editing}
        />
      )}
    </>
  )
}

interface MarkTarget {
  id: number
  name: string
  autoLetters: string
  inactive: boolean
}

interface InstanceIconsSectionProps {
  targets: readonly MarkTarget[]
  onOpen: (target: MarkTarget) => void
}

function InstanceIconsSection({ targets, onOpen }: InstanceIconsSectionProps) {
  const { t } = useTranslation("settings")
  const { t: tInstances } = useTranslation("instances")
  const settled = useClientSettingsSettled()

  return (
    <Section title={t("instances.icons.title")} description={t("instances.icons.description")}>
      <ResourceList
        label={t("instances.icons.title")}
        items={targets}
        itemKey={(target) => target.id}
        row={(target) => ({
          title: target.name,
          icon: <InstanceMarkIcon instanceId={target.id} autoLetters={target.autoLetters} />,
          status: target.inactive ? { tone: "off", label: tInstances("card.status.disabled") } : undefined,
          meta: [<InstanceMarkSummary key="mark" instanceId={target.id} autoLetters={target.autoLetters} />],
          onOpen: settled ? () => onOpen(target) : undefined,
        })}
        empty={{ title: tInstances("list.emptyTitle") }}
      />
    </Section>
  )
}

function TitleBarSpeedsSection() {
  const { t } = useTranslation("settings")
  const [enabled, setEnabled] = usePersistedTitleBarSpeeds(false)
  // Until the server answers, the switch shows the default, and a change would be lost.
  const settled = useClientSettingsSettled()

  return (
    <Section title={t("instances.browserTab.title")}>
      <SwitchRow label={t("instances.titleBarSpeeds.label")} checked={enabled} disabled={!settled} onCheckedChange={setEnabled} />
    </Section>
  )
}
