/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { databaseShrinkQueryKey, useDatabaseShrink } from "@/hooks/useDatabaseShrink"
import { api } from "@/lib/api"
import { formatBytes } from "@/lib/utils"
import type { DatabaseShrinkStatus, TransferHistoryInstance } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

const ALL = "all"

const MANUAL_SHRINK_DOCS_URL = "https://getqui.com/docs/features/statistics#shrink-the-database-file-by-hand"

type Range = "everything" | "before"

/** The start of a date from a date input, in the time zone of the browser. */
function startOfDate(value: string): Date {
  const [year, month, day] = value.split("-").map(Number)
  return new Date(year, month - 1, day)
}

/** The date of a date input as text, with no shift to another time zone. */
function formatDate(value: string, locale: string): string {
  return startOfDate(value).toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" })
}

function today(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`
}

export function StatisticsDeleteHistory({ instances }: { instances: TransferHistoryInstance[] }) {
  const { t, i18n } = useTranslation("settings")
  const queryClient = useQueryClient()
  const [target, setTarget] = useState(ALL)
  const [range, setRange] = useState<Range>("everything")
  const [date, setDate] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [shrinkTicked, setShrinkTicked] = useState(false)
  const engineQuery = useQuery({ queryKey: ["application-info"], queryFn: () => api.getApplicationInfo() })
  const engine = engineQuery.data?.database.engine
  const shrinkQuery = useQuery({ queryKey: databaseShrinkQueryKey, queryFn: () => api.getDatabaseShrink(), enabled: confirming && engine === "sqlite" })
  const shrinkStatus = shrinkQuery.data
  const shrinkFits = shrinkStatus !== undefined && shrinkStatus.freeBytes >= shrinkStatus.needBytes
  const shrink = shrinkTicked && shrinkFits
  const { requestShrink } = useDatabaseShrink()

  const mutation = useMutation({
    mutationFn: () => api.deleteTransferHistory({
      instanceId: target === ALL ? "all" : Number(target),
      before: range === "before" ? startOfDate(date) : undefined,
    }),
    onSuccess: () => {
      // A delete of everything takes the instance out of the list.
      setTarget(ALL)
      toast.success(t("statistics.deleteHistory.deleted"))
      if (shrink) {
        setShrinkTicked(false)
        void requestShrink()
      }
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t("statistics.deleteHistory.failed"))
    },
    onSettled: () => {
      setConfirming(false)
      // A delete that fails can still have deleted a part of the history.
      void queryClient.invalidateQueries({ queryKey: ["transfer-history"] })
    },
  })

  const instanceName = (instance: TransferHistoryInstance) => instance.name || t("statistics.deleteHistory.unnamedInstance", { id: instance.instanceId })
  const selected = instances.find(instance => String(instance.instanceId) === target)
  const name = selected ? instanceName(selected) : t("statistics.deleteHistory.allInstances")
  const ready = instances.length > 0 && (range === "everything" || date !== "") && (target === ALL || selected !== undefined)

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <span className="text-sm font-medium">{t("statistics.deleteHistory.label")}</span>
        <p className="text-sm text-muted-foreground">{t("statistics.deleteHistory.description")}</p>
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
        <div className="space-y-1.5">
          <Label htmlFor="statistics-delete-instance">{t("statistics.deleteHistory.instance")}</Label>
          <Select name="statistics-delete-instance" value={target} onValueChange={setTarget} disabled={mutation.isPending}>
            <SelectTrigger id="statistics-delete-instance" className="h-9 w-full sm:w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t("statistics.deleteHistory.allInstances")}</SelectItem>
              {instances.map(instance => (
                <SelectItem key={instance.instanceId} value={String(instance.instanceId)}>{instanceName(instance)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <RadioGroup value={range} onValueChange={value => setRange(value as Range)} disabled={mutation.isPending} className="gap-2">
          <div className="flex items-center gap-2">
            <RadioGroupItem id="statistics-delete-everything" value="everything" />
            <Label htmlFor="statistics-delete-everything" className="font-normal">{t("statistics.deleteHistory.everything")}</Label>
          </div>
          <div className="flex items-center gap-2">
            <RadioGroupItem id="statistics-delete-before" value="before" />
            <Label htmlFor="statistics-delete-before" className="font-normal">{t("statistics.deleteHistory.before")}</Label>
          </div>
        </RadioGroup>
        {range === "before" && (
          <Input
            aria-label={t("statistics.deleteHistory.date")}
            type="date"
            max={today()}
            value={date}
            disabled={mutation.isPending}
            onChange={event => setDate(event.target.value)}
            className="h-9 w-full sm:w-44"
          />
        )}
      </div>
      <Button variant="destructive" size="sm" disabled={!ready || mutation.isPending} onClick={() => setConfirming(true)}>
        {t("statistics.deleteHistory.button")}
      </Button>

      <AlertDialog open={confirming} onOpenChange={open => !mutation.isPending && setConfirming(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("statistics.deleteHistory.confirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {range === "before" && date !== ""
                ? t("statistics.deleteHistory.confirmBefore", { name, date: formatDate(date, i18n.resolvedLanguage ?? i18n.language) })
                : t("statistics.deleteHistory.confirmEverything", { name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {engine === "sqlite" && !shrink && <p className="text-sm text-muted-foreground">{t("statistics.deleteHistory.sqliteSpace")}</p>}
          {engine === "sqlite" && shrinkStatus?.available && (
            <ShrinkCheckbox status={shrinkStatus} checked={shrink} onCheckedChange={setShrinkTicked} disabled={mutation.isPending} />
          )}
          {engine === "postgres" && <p className="text-sm text-muted-foreground">{t("statistics.deleteHistory.postgresSpace")}</p>}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={mutation.isPending}>{t("statistics.deleteHistory.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={mutation.isPending}
              onClick={event => {
                // The dialog stays open until the delete ends.
                event.preventDefault()
                mutation.mutate()
              }}
            >
              {t("statistics.deleteHistory.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function ShrinkCheckbox({ status, checked, onCheckedChange, disabled }: {
  status: DatabaseShrinkStatus
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled: boolean
}) {
  const { t } = useTranslation("settings")
  const fits = status.freeBytes >= status.needBytes

  return (
    <div className="flex items-start gap-2">
      <Checkbox
        id="statistics-delete-shrink"
        checked={checked}
        disabled={disabled || !fits}
        onCheckedChange={value => onCheckedChange(value === true)}
        className="mt-0.5"
      />
      <div className="space-y-1">
        <Label htmlFor="statistics-delete-shrink">{t("statistics.deleteHistory.shrink.label")}</Label>
        <p className="text-sm text-muted-foreground">
          {fits
            ? t(status.restartAvailable ? "statistics.deleteHistory.shrink.restart" : "statistics.deleteHistory.shrink.nextStart")
            : t("statistics.deleteHistory.shrink.tooLittleSpace", { free: formatBytes(status.freeBytes), need: formatBytes(status.needBytes) })}
        </p>
        {!fits && (
          <a className="text-sm underline underline-offset-2" href={MANUAL_SHRINK_DOCS_URL} target="_blank" rel="noopener noreferrer">
            {t("statistics.deleteHistory.shrink.manualDocs")}
          </a>
        )}
      </div>
    </div>
  )
}
