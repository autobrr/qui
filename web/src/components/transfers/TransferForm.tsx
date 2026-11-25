/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { SwitchRow } from "@/components/kit/SwitchRow"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import type { TransferAfter, TransferWayChoice } from "@/lib/transfers-api"
import type { InstanceResponse } from "@/types"
import { useId, useMemo } from "react"
import { useTranslation } from "react-i18next"
import type { TransferFormValue } from "./transfer-form-value"

const afterOptions: readonly TransferAfter[] = ["removeWithFiles", "keepFiles", "keepSeeding"]
const wayOptions: readonly TransferWayChoice[] = ["auto", "copy", "bittorrent"]
const noCategory = "none"
const categoryPrefix = "category:"

interface TransferFormProps {
  value: TransferFormValue
  onChange: (value: TransferFormValue) => void
  targets: InstanceResponse[] | undefined
}

export function TransferForm({ value, onChange, targets }: TransferFormProps) {
  const { t } = useTranslation("transfers")
  const id = useId()
  const { data: metadata } = useInstanceMetadata(value.targetInstanceId ?? 0)
  const categories = useMemo(() => {
    const names = Object.keys(metadata?.categories ?? {})
    if (value.category && !names.includes(value.category)) names.push(value.category)
    return names.sort()
  }, [metadata, value.category])
  const set = (patch: Partial<TransferFormValue>) => onChange({ ...value, ...patch })

  return (
    <div className="@container">
      <div className="grid grid-cols-[minmax(0,1fr)] gap-4 @lg:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-target`}>{t("form.target")}</Label>
          {targets?.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("form.noTargets")}</p>
          ) : (
            <Select
              value={value.targetInstanceId?.toString() ?? ""}
              onValueChange={target => set({ targetInstanceId: Number(target), category: "" })}
            >
              <SelectTrigger id={`${id}-target`} className="w-full">
                <SelectValue placeholder={t("form.targetPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {targets?.map(instance => (
                  <SelectItem key={instance.id} value={instance.id.toString()}>{instance.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-way`}>
            {t("form.way")}
            <FieldHelp>{t("form.wayHelp")}</FieldHelp>
          </Label>
          <Select value={value.way} onValueChange={way => set({ way: way as TransferWayChoice })}>
            <SelectTrigger id={`${id}-way`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {wayOptions.map(option => (
                <SelectItem key={option} value={option}>{t(`form.wayOptions.${option}`)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1.5 @lg:col-span-2">
          <Label htmlFor={`${id}-save-path`}>
            {t("form.savePath")}
            <FieldHelp>{t("form.savePathHelp")}</FieldHelp>
          </Label>
          <Input
            id={`${id}-save-path`}
            value={value.savePath}
            onChange={event => set({ savePath: event.target.value })}
            placeholder={value.category ? t("form.savePathPlaceholderCategory") : t("form.savePathPlaceholder")}
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-category`}>{t("form.category")}</Label>
          <Select
            value={value.category ? categoryPrefix + value.category : noCategory}
            onValueChange={option => set({ category: option === noCategory ? "" : option.slice(categoryPrefix.length) })}
            disabled={value.targetInstanceId === null}
          >
            <SelectTrigger id={`${id}-category`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={noCategory}>{t("form.noCategory")}</SelectItem>
              {categories.map(category => (
                <SelectItem key={category} value={categoryPrefix + category}>{category}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor={`${id}-tags`}>{t("form.tags")}</Label>
          <Input
            id={`${id}-tags`}
            value={value.tags}
            onChange={event => set({ tags: event.target.value })}
            placeholder={t("form.tagsPlaceholder")}
          />
        </div>

        <div className="space-y-1.5 @lg:col-span-2">
          <Label htmlFor={`${id}-after`}>
            {t("form.after")}
            <FieldHelp>{t("form.afterHelp")}</FieldHelp>
          </Label>
          <Select value={value.after} onValueChange={after => set({ after: after as TransferAfter })}>
            <SelectTrigger id={`${id}-after`} className="w-full whitespace-normal text-left data-[size=default]:h-auto data-[size=default]:min-h-9 *:data-[slot=select-value]:line-clamp-none">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {afterOptions.map(option => (
                <SelectItem key={option} value={option}>{t(`form.afterOptions.${option}`)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="@lg:col-span-2">
          <SwitchRow
            label={t("form.leaveStopped")}
            checked={value.leaveStopped}
            onCheckedChange={leaveStopped => set({ leaveStopped })}
          />
        </div>
      </div>
    </div>
  )
}
