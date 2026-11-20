/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ArrowDown, ArrowUp, PlugZap, Shapes, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CrossSeedDeleteNotice, useCrossSeedDeleteCheck } from "@/components/cross-seed/RouteProtectionConfirm"
import { DeleteHistoryCheckbox } from "@/components/instances/DeleteHistoryCheckbox"
import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { RemoteAccessIndicator } from "@/components/instances/RemoteAccessIndicator"
import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { ResourceList, type ResourceAction, type ResourceRow } from "@/components/kit/ResourceList"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { instanceStatusKey, useInstances } from "@/hooks/useInstances"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { formatErrorMessage } from "@/lib/format-error-message"
import { useIncognitoMode } from "@/lib/incognito"
import type { InstanceResponse } from "@/types"

interface InstanceListProps {
  instances: readonly InstanceResponse[]
  /** Opens the settings of the instance. */
  onEdit: (instance: InstanceResponse) => void
  onChangeIcon: (instance: InstanceResponse) => void
  onAdd: () => void
}

// The errors that InstanceErrorDisplay shows.
const hasInstanceErrors = (instance: InstanceResponse) =>
  instance.hasDecryptionError || (!instance.connected && (instance.recentErrors?.length ?? 0) > 0)

const errorText = (error: unknown, fallback: string) => error instanceof Error ? formatErrorMessage(error.message) : fallback

/** The qBittorrent instances in their order, each with its state, its on/off switch, a test, a move, and a delete. */
export function InstanceList({ instances, onEdit, onChangeIcon, onAdd }: InstanceListProps) {
  const { t } = useTranslation("instances")
  const { testConnection, setInstanceStatus, reorderInstances, isReordering } = useInstances()
  const toggling = usePendingIds(instanceStatusKey)
  const [incognito] = useIncognitoMode()
  const marksSettled = useClientSettingsSettled()
  const [testing, setTesting] = useState<ReadonlySet<number>>(() => new Set())
  // The instance stays in the state while the dialog fades out. A new session gives the dialog fresh state.
  const [removal, setRemoval] = useState<{ instance: InstanceResponse; open: boolean; session: number } | null>(null)

  const test = async (instance: InstanceResponse) => {
    setTesting((prev) => new Set(prev).add(instance.id))
    try {
      const result = await testConnection(instance.id)
      if (result.connected) {
        toast.success(t("card.toast.testSuccessTitle"), { description: result.message || t("card.toast.testSuccessDescription") })
      } else {
        toast.error(t("card.toast.testFailedTitle"), {
          description: result.message ? formatErrorMessage(result.message) : t("card.toast.testFailedDescription"),
        })
      }
    } catch (error) {
      toast.error(t("card.toast.testFailedTitle"), { description: errorText(error, t("card.toast.connectionFailed")) })
    } finally {
      setTesting((prev) => {
        const next = new Set(prev)
        next.delete(instance.id)
        return next
      })
    }
  }

  const setActive = (instance: InstanceResponse, isActive: boolean) => {
    setInstanceStatus({ id: instance.id, isActive }, {
      onSuccess: () => {
        toast.success(isActive ? t("card.toast.instanceEnabledTitle") : t("card.toast.instanceDisabledTitle"), {
          description: isActive ? t("card.toast.instanceEnabledDescription") : t("card.toast.instanceDisabledDescription"),
        })
      },
      onError: (error) => {
        toast.error(t("card.toast.statusUpdateFailedTitle"), { description: errorText(error, t("card.toast.statusUpdateFailedDescription")) })
      },
    })
  }

  const move = (index: number, direction: -1 | 1) => {
    const orderedIds = instances.map((instance) => instance.id)
    const [moved] = orderedIds.splice(index, 1)
    orderedIds.splice(index + direction, 0, moved)
    reorderInstances(orderedIds, {
      onError: (error) => toast.error(t("list.reorderFailed"), { description: error instanceof Error ? error.message : undefined }),
    })
  }

  const row = (instance: InstanceResponse): ResourceRow => {
    const index = instances.indexOf(instance)
    const moves: ResourceAction[] = [
      ...(index > 0 ? [{ label: t("card.tooltips.moveUp"), icon: ArrowUp, disabled: isReordering, onSelect: () => move(index, -1) }] : []),
      ...(index < instances.length - 1 ? [{ label: t("card.tooltips.moveDown"), icon: ArrowDown, disabled: isReordering, onSelect: () => move(index, 1) }] : []),
    ]

    return {
      title: instance.name,
      icon: <InstanceMarkIcon instanceId={instance.id} problem={instance.isActive && !instance.connected} />,
      status: !instance.isActive
        ? { tone: "off", label: t("card.status.disabled") }
        : instance.connected ? { tone: "ok", label: t("card.status.connected") } : { tone: "error", label: t("card.status.disconnected") },
      meta: [
        <span key="host" className={incognito ? "select-none blur-sm" : undefined}>{instance.host}</span>,
        <RemoteAccessIndicator key="access" instance={instance} variant="fact" />,
        ...(instance.tlsSkipVerify ? [t("list.tlsSkipped")] : []),
      ],
      enabled: {
        checked: instance.isActive,
        onCheckedChange: (isActive) => setActive(instance, isActive),
        disabled: toggling.has(instance.id),
      },
      onOpen: () => onEdit(instance),
      // A wide screen shows Edit, Test, and Delete as buttons, and Change icon and the moves in the menu.
      actions: [
        {
          label: t("card.actions.testConnection"),
          icon: PlugZap,
          disabled: !instance.isActive,
          pending: testing.has(instance.id),
          onSelect: () => void test(instance),
        },
        {
          label: t("card.actions.delete"),
          icon: Trash2,
          destructive: true,
          onSelect: () => setRemoval((prev) => ({ instance, open: true, session: (prev?.session ?? 0) + 1 })),
        },
        { label: t("card.actions.changeIcon"), icon: Shapes, disabled: !marksSettled, onSelect: () => onChangeIcon(instance) },
        ...moves,
      ],
      detail: hasInstanceErrors(instance) ? <InstanceErrorDisplay instance={instance} onEdit={() => onEdit(instance)} /> : undefined,
    }
  }

  return (
    <>
      <ResourceList
        label={t("list.label")}
        items={instances}
        itemKey={(instance) => instance.id}
        row={row}
        empty={{ title: t("list.emptyTitle"), description: t("list.emptyDescription"), onAdd }}
      />
      {removal && (
        <DeleteInstanceDialog
          key={removal.session}
          instance={removal.instance}
          open={removal.open}
          onOpenChange={(open) => setRemoval((prev) => prev && { ...prev, open })}
        />
      )}
    </>
  )
}

interface DeleteInstanceDialogProps {
  instance: InstanceResponse
  open: boolean
  onOpenChange: (open: boolean) => void
}

function DeleteInstanceDialog({ instance, open, onOpenChange }: DeleteInstanceDialogProps) {
  const { t } = useTranslation("instances")
  const { deleteInstance, isDeleting } = useInstances()
  const [deleteHistory, setDeleteHistory] = useState(false)
  const check = useCrossSeedDeleteCheck(instance.id, open)

  // A rejected promise keeps the dialog open when the delete fails, so the user can try again.
  const confirm = () => new Promise<void>((resolve, reject) => {
    deleteInstance({ id: instance.id, name: instance.name, deleteHistory }, {
      onSuccess: () => {
        toast.success(t("card.toast.instanceDeletedTitle"), { description: t("card.toast.instanceDeletedDescription", { name: instance.name }) })
        resolve()
      },
      onError: (error) => {
        toast.error(t("card.toast.deleteFailedTitle"), { description: errorText(error, t("card.toast.deleteFailedDescription")) })
        reject(error)
      },
    })
  })

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("card.deleteDialog.title")}
      description={t("card.deleteDialog.description", { name: instance.name })}
      confirmLabel={t("card.deleteDialog.confirm")}
      confirmDisabled={check.pending}
      onConfirm={confirm}
    >
      <CrossSeedDeleteNotice check={check} />
      <DeleteHistoryCheckbox checked={deleteHistory} onCheckedChange={setDeleteHistory} disabled={isDeleting} />
    </ConfirmDialog>
  )
}
