/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, useRef, useState, type KeyboardEvent, type RefObject } from "react"
import { useTranslation } from "react-i18next"

import { Editor } from "@/components/kit/Editor"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  DEFAULT_INSTANCE_MARK,
  INSTANCE_MARK_COLORS,
  instanceMarkColorProps,
  normalizeInstanceMarkLetters,
  useInstanceMark,
  type InstanceMark,
  type InstanceMarkColor
} from "@/lib/instance-marks"
import { cn } from "@/lib/utils"

import { INSTANCE_MARK_ICON_GROUPS, INSTANCE_MARK_ICONS } from "./instance-mark-icons"
import { MarkGlyph } from "./InstanceMarkIcon"

const ICON_COLUMNS = 8

const COLOR_LABEL_KEYS: Record<InstanceMarkColor, string> = {
  sky: "mark.colors.sky",
  violet: "mark.colors.violet",
  pink: "mark.colors.pink",
  orange: "mark.colors.orange",
  amber: "mark.colors.amber",
  lime: "mark.colors.lime",
  emerald: "mark.colors.emerald",
  teal: "mark.colors.teal",
}

const SWATCHES: (InstanceMarkColor | null)[] = [null, ...INSTANCE_MARK_COLORS]

const COLOR_STEPS: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }

const optionClassName = "outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50"
const pressedClassName = "bg-accent ring-1 ring-foreground/40"
const previewClassName = "grid size-8 place-items-center rounded-md border bg-background text-foreground"
const labelColumnClassName = "sm:grid sm:grid-cols-[6rem_1fr] sm:items-center sm:gap-2"

function moveFocus(event: KeyboardEvent<HTMLElement>, count: number, columns: number) {
  const from = Number((event.target as HTMLElement).dataset.index)
  if (Number.isNaN(from)) return
  const steps: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1, ArrowDown: columns, ArrowUp: -columns }
  let to: number
  if (event.key in steps) to = from + steps[event.key]
  else if (event.key === "Home") to = from - (from % columns)
  else if (event.key === "End") to = Math.min(from - (from % columns) + columns - 1, count - 1)
  else return
  event.preventDefault()
  if (to < 0 || to >= count) return
  event.currentTarget.querySelector<HTMLButtonElement>(`[data-index="${to}"]`)?.focus()
}

export function InstanceMarkSummary({ instanceId, autoLetters }: { instanceId: number; autoLetters: string }) {
  const { t } = useTranslation("instances")
  const [mark] = useInstanceMark(instanceId)
  const entry = INSTANCE_MARK_ICONS.find((icon) => icon.name === mark.icon)
  const parts = entry
    ? [t(entry.labelKey)]
    : mark.letters ? [t("mark.picker.letters"), mark.letters] : [t("mark.picker.automatic"), ...(autoLetters ? [autoLetters] : [])]
  if (mark.color) parts.push(t(COLOR_LABEL_KEYS[mark.color]))
  return parts.join(" · ")
}

interface LettersFieldProps {
  id: string
  value: string
  placeholder: string
  active: boolean
  colorProps: ReturnType<typeof instanceMarkColorProps>
  onLetters: (letters: string) => void
  onDone: () => void
}

function LettersField({ id, value, placeholder, active, colorProps, onLetters, onDone }: LettersFieldProps) {
  const [composing, setComposing] = useState<string | null>(null)
  const commit = (raw: string) => {
    const letters = normalizeInstanceMarkLetters(raw)
    if (letters !== value) onLetters(letters)
  }

  return (
    <Input
      id={id}
      data-current={active || undefined}
      value={composing ?? value}
      placeholder={placeholder}
      onCompositionStart={(event) => setComposing(event.currentTarget.value)}
      onCompositionEnd={(event) => {
        setComposing(null)
        commit(event.currentTarget.value)
      }}
      onChange={(event) => {
        if (composing === null) commit(event.target.value)
        else setComposing(event.target.value)
      }}
      onKeyDown={(event) => {
        if (event.key === "Enter" && !event.nativeEvent.isComposing && event.keyCode !== 229) onDone()
      }}
      onFocus={(event) => event.currentTarget.select()}
      className={cn(
        "h-9 w-14 text-center font-semibold uppercase placeholder:font-normal placeholder:text-muted-foreground/60 max-sm:h-11",
        active && cn("border-foreground/40 bg-accent/60 dark:bg-accent/60", colorProps.className)
      )}
      style={active ? colorProps.style : undefined}
      autoComplete="off"
      spellCheck={false}
    />
  )
}

interface InstanceMarkPickerBodyProps {
  mark: InstanceMark
  autoLetters: string
  automaticRef: RefObject<HTMLButtonElement | null>
  onChange: (mark: InstanceMark) => void
  onDone: () => void
}

function MarkPreview({ mark, autoLetters }: { mark: InstanceMark; autoLetters: string }) {
  const { t } = useTranslation("instances")
  const iconEntry = INSTANCE_MARK_ICONS.find((entry) => entry.name === mark.icon)
  const collapsedLetters = mark.letters || autoLetters
  const driveLabel = t("mark.icons.hardDrive")
  const iconLabel = iconEntry ? t(iconEntry.labelKey) : null

  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
      <span className="flex items-center gap-2">
        {t("mark.picker.previewCollapsed")}
        <span role="img" aria-label={iconLabel ?? (collapsedLetters || driveLabel)} className={previewClassName}>
          <MarkGlyph mark={mark} letters={collapsedLetters} />
        </span>
      </span>
      <span className="flex items-center gap-2">
        {t("mark.picker.previewElsewhere")}
        <span role="img" aria-label={iconLabel ?? driveLabel} className={previewClassName}>
          <MarkGlyph mark={mark} />
        </span>
      </span>
    </div>
  )
}

function InstanceMarkPickerBody({ mark, autoLetters, automaticRef, onChange, onDone }: InstanceMarkPickerBodyProps) {
  const { t } = useTranslation("instances")
  const lettersInputId = useId()
  const arrowHintId = useId()
  const selectedIcon = INSTANCE_MARK_ICONS.findIndex((entry) => entry.name === mark.icon)
  const usesLetters = selectedIcon === -1 && mark.letters !== ""
  const automatic = selectedIcon === -1 && mark.letters === ""
  const colorProps = instanceMarkColorProps(mark.color)
  const [iconTabStop, setIconTabStop] = useState<number | null>(null)

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <button
          ref={automaticRef}
          type="button"
          data-current={automatic || undefined}
          aria-pressed={automatic}
          onClick={() => onChange({ ...mark, icon: null, letters: "" })}
          className={cn("flex h-9 items-center gap-2 rounded-md border px-2.5 text-sm max-sm:h-11", optionClassName, automatic && pressedClassName)}
        >
          <MarkGlyph mark={{ ...mark, icon: null }} letters={autoLetters} colored={automatic} />
          {t("mark.picker.automatic")}
        </button>
        <span className="ml-auto flex items-center gap-2">
          <label htmlFor={lettersInputId} className="text-sm text-muted-foreground">{t("mark.picker.letters")}</label>
          <LettersField
            id={lettersInputId}
            value={selectedIcon === -1 ? mark.letters : ""}
            placeholder={autoLetters || "–"}
            active={usesLetters}
            colorProps={colorProps}
            onLetters={(letters) => onChange({ ...mark, icon: null, letters })}
            onDone={onDone}
          />
        </span>
      </div>

      <div className={labelColumnClassName}>
        <p aria-hidden="true" className="pb-1.5 text-xs text-muted-foreground sm:pb-0">
          {mark.color ? t("mark.picker.colorCurrent", { color: t(COLOR_LABEL_KEYS[mark.color]) }) : t("mark.picker.color")}
        </p>
        <div
          role="radiogroup"
          aria-label={t("mark.picker.color")}
          onKeyDown={(event) => {
            const step = COLOR_STEPS[event.key]
            const from = Number((event.target as HTMLElement).dataset.index)
            if (!step || Number.isNaN(from)) return
            event.preventDefault()
            const to = (from + step + SWATCHES.length) % SWATCHES.length
            onChange({ ...mark, color: SWATCHES[to] })
            event.currentTarget.querySelector<HTMLButtonElement>(`[data-index="${to}"]`)?.focus()
          }}
          className="grid grid-cols-9 gap-1 sm:flex sm:flex-wrap sm:items-center"
        >
          {SWATCHES.map((color, index) => {
            const checked = color === mark.color
            const label = t(color ? COLOR_LABEL_KEYS[color] : "mark.colors.none")
            const swatch = instanceMarkColorProps(color)
            return (
              <Tooltip key={color ?? "none"}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    role="radio"
                    data-index={index}
                    tabIndex={checked ? 0 : -1}
                    aria-checked={checked}
                    aria-label={label}
                    onClick={() => onChange({ ...mark, color })}
                    className={cn(
                      "grid aspect-square w-full max-w-11 place-items-center justify-self-center rounded-full sm:size-8",
                      optionClassName,
                      swatch.className
                    )}
                    style={swatch.style}
                  >
                    <span
                      className={cn(
                        "relative size-5 overflow-hidden rounded-full ring-offset-2 ring-offset-background",
                        color && "bg-current",
                        checked ? "ring-2 ring-foreground" : "ring-1 ring-border"
                      )}
                    >
                      {color === null && (
                        <span className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 -rotate-45 bg-muted-foreground" />
                      )}
                    </span>
                  </button>
                </TooltipTrigger>
                <TooltipContent>{label}</TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </div>

      <div className="border-t pt-3">
        <p id={arrowHintId} className="sr-only">{t("mark.picker.arrowHint")}</p>
        <div
          role="group"
          aria-label={t("mark.picker.iconHeading")}
          aria-describedby={arrowHintId}
          onKeyDown={(event) => moveFocus(event, INSTANCE_MARK_ICONS.length, ICON_COLUMNS)}
          className="space-y-2.5 sm:space-y-0.5"
        >
          {INSTANCE_MARK_ICON_GROUPS.map((group, groupIndex) => (
            <div
              key={group.labelKey}
              role="group"
              aria-label={t(group.labelKey)}
              className={labelColumnClassName}
            >
              <p aria-hidden="true" className="pb-1 text-xs text-muted-foreground sm:pb-0">{t(group.labelKey)}</p>
              <div className="grid grid-cols-8 gap-0.5">
                {group.icons.map((entry, i) => {
                  const index = groupIndex * ICON_COLUMNS + i
                  const pressed = index === selectedIcon
                  const label = t(entry.labelKey)
                  return (
                    <Tooltip key={entry.name}>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          data-index={index}
                          data-current={pressed || undefined}
                          tabIndex={index === (iconTabStop ?? Math.max(selectedIcon, 0)) ? 0 : -1}
                          aria-label={label}
                          aria-pressed={pressed}
                          onFocus={() => setIconTabStop(index)}
                          onClick={() => {
                            setIconTabStop(index)
                            onChange({ ...mark, icon: entry.name, letters: "" })
                          }}
                          className={cn("grid h-7 w-full place-items-center rounded-md max-sm:h-11", optionClassName, pressed && pressedClassName)}
                        >
                          <entry.Icon className={cn("size-[18px]", pressed && colorProps.className)} style={pressed ? colorProps.style : undefined} />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>{label}</TooltipContent>
                    </Tooltip>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

interface InstanceMarkEditorProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  name: string
  autoLetters: string
}

export function InstanceMarkEditor({ open, onOpenChange, instanceId, name, autoLetters }: InstanceMarkEditorProps) {
  const { t } = useTranslation("instances")
  const [mark, setMark] = useInstanceMark(instanceId)
  const automaticRef = useRef<HTMLButtonElement>(null)
  const customized = mark.icon !== null || mark.letters !== "" || mark.color !== null

  return (
    <Editor
      open={open}
      onOpenChange={onOpenChange}
      title={t("mark.picker.title", { name })}
      description={t("mark.picker.description")}
      initialFocus="[data-current]"
      pinned={<MarkPreview mark={mark} autoLetters={autoLetters} />}
      secondaryActions={
        <Button
          type="button"
          variant="outline"
          disabled={!customized}
          onClick={() => {
            setMark(DEFAULT_INSTANCE_MARK)
            automaticRef.current?.focus()
          }}
        >
          {t("mark.picker.reset")}
        </Button>
      }
    >
      <InstanceMarkPickerBody mark={mark} autoLetters={autoLetters} automaticRef={automaticRef} onChange={setMark} onDone={() => onOpenChange(false)} />
    </Editor>
  )
}
