/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { useReturnFocus } from "@/components/kit/use-return-focus"
import { ThemeThumbnail } from "@/components/themes/ThemeThumbnail"
import { Command, CommandEmpty, CommandGroup, CommandItem, CommandList, CommandShortcut } from "@/components/ui/command"
import { DialogContent, DialogTitle } from "@/components/ui/dialog"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { isMac, keyLabels, modShortcut } from "@/lib/keyboard"
import { passwordManagerAttributes } from "@/lib/password-manager"
import { Command as CommandPrimitive } from "cmdk"
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ChevronRight, Search, X } from "lucide-react"
import { type CSSProperties, type KeyboardEvent, type ReactNode, useId, useReducer, useRef } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"
import { findEntry, INITIAL_PALETTE_STATE, type PaletteEntry, type PaletteGroup, paletteKeyAction, paletteReducer, searchEntries } from "./palette"
import { useDestinations } from "./useDestinations"
import { useTorrentResults } from "./useTorrentResults"
import { useVisibleHeight } from "./useVisibleHeight"

function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="inline-flex h-6 min-w-6 items-center justify-center rounded-md bg-foreground/10 px-1.5 font-sans text-xs font-medium text-foreground">
      {children}
    </kbd>
  )
}

function Hint({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="flex items-center gap-1.5">
      {children}
      <span>{label}</span>
    </span>
  )
}

export function CommandPaletteContent({ close, startText = "" }: { close: () => void; startText?: string }) {
  const { t } = useTranslation(["common", "torrents"])
  const [state, dispatch] = useReducer(paletteReducer, { ...INITIAL_PALETTE_STATE, query: startText })
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const returnFocus = useReturnFocus()
  const idPrefix = useId()
  const touch = useMediaQuery("(pointer: coarse)")
  const visibleHeight = useVisibleHeight()
  const rowId = (entry: PaletteEntry) => `${idPrefix}-${entry.id}`

  const query = state.query.trim()
  const destinations = useDestinations(close)
  const parent = findEntry(destinations, state.parentId)
  const torrents = useTorrentResults(parent ? "" : query, close)
  const scope = parent?.sections ?? destinations
  const searchFirst = touch && !torrents.noMatch ? torrents.entries.slice(0, 1) : []
  const groups: PaletteGroup[] = [
    { id: "torrent-search", entries: searchFirst },
    ...(query ? [{ id: "results", entries: searchEntries(scope, query) }] : scope),
    { id: "torrents", label: t("torrents:page.routeTitle"), entries: torrents.entries.slice(searchFirst.length) },
  ].filter(group => group.entries.length > 0)
  const rows = groups.flatMap(group => group.entries).filter(entry => !entry.disabled)
  const highlighted = rows.find(entry => entry.id === state.highlightId) ?? rows.at(0)
  const moreOf = !parent && highlighted?.sections ? highlighted : undefined

  const changeText = (text: string) => {
    dispatch({ type: "typed", query: text })
    listRef.current?.scrollTo({ top: 0 })
  }
  const openSections = (entry: PaletteEntry) => {
    dispatch({ type: "sectionsOpened", parentId: entry.id })
    listRef.current?.scrollTo({ top: 0 })
    inputRef.current?.focus()
  }
  const select = (entry: PaletteEntry) => (entry.run ? entry.run() : openSections(entry))
  const back = () => {
    flushSync(() => dispatch({ type: "wentBack" }))
    if (parent) document.getElementById(rowId(parent))?.scrollIntoView({ block: "nearest" })
    inputRef.current?.focus()
  }

  const onKeyDown = (event: KeyboardEvent) => {
    const input = inputRef.current
    const action = paletteKeyAction(event.nativeEvent, {
      entries: [...destinations.flatMap(group => group.entries), ...torrents.entries],
      moreOf,
      inSections: parent !== undefined,
      queryEmpty: state.query === "",
      caretAtEnd: input !== null && input.selectionStart === input.value.length,
    })
    if (!action) return
    event.preventDefault()
    if (action.type === "run") select(action.entry)
    else if (action.type === "openSections") openSections(action.parent)
    else if (action.type === "goBack") back()
  }

  const renderRow = (entry: PaletteEntry, alignsShortcut: boolean) => {
    const subtitle = [entry.current ? t("common:commandPalette.current") : null, parent ? null : entry.trail, entry.note].filter(Boolean).join(" · ")
    const copies = entry.copies ?? 0
    return (
      <CommandItem asChild key={entry.id} value={entry.id} disabled={entry.disabled} onSelect={() => select(entry)} className="cursor-pointer gap-3 rounded-lg px-3 py-2">
        <div id={rowId(entry)}>
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-foreground/[0.06] text-muted-foreground">
            {"instanceId" in entry.icon ? <InstanceMarkIcon instanceId={entry.icon.instanceId} problem={entry.icon.problem} /> : <entry.icon className="size-4" />}
          </span>
          <span className="flex min-w-0 flex-1 flex-col">
            <TruncatedText className="font-medium" openOnTap={false}>{entry.label}</TruncatedText>
            {subtitle && <span className="truncate text-xs text-muted-foreground">{subtitle}</span>}
          </span>
          {entry.thumbnail && <ThemeThumbnail palette={entry.thumbnail} detail="compact" className="w-10 shrink-0 rounded-sm" />}
          {copies > 0 && (
            <Tooltip>
              <TooltipTrigger asChild openOnTap="only">
                <span className="shrink-0 rounded-full bg-foreground/10 px-1.5 py-0.5 text-xs text-muted-foreground">
                  <span aria-hidden="true">{t("torrents:copyGroups.badge", { hidden: copies })}</span>
                  <span className="sr-only">{t("torrents:copyGroups.copies", { count: copies, name: entry.label })}</span>
                </span>
              </TooltipTrigger>
              <TooltipContent>{t("torrents:copyGroups.copies", { count: copies, name: entry.label })}</TooltipContent>
            </Tooltip>
          )}
          {entry.shortcut !== undefined && (
            <CommandShortcut className="text-sm tracking-normal pointer-coarse:hidden">{modShortcut(entry.shortcut === "Enter" ? keyLabels.enter : entry.shortcut)}</CommandShortcut>
          )}
          {!parent && entry.sections ? (
            <button
              type="button"
              tabIndex={-1}
              aria-label={t("common:commandPalette.moreOf", { name: entry.label })}
              className="shrink-0 rounded-md p-1 text-muted-foreground hover:bg-foreground/10 hover:text-foreground pointer-coarse:-m-2 pointer-coarse:p-3"
              onClick={(event) => {
                event.stopPropagation()
                openSections(entry)
              }}
            >
              <ChevronRight className="size-4" />
            </button>
          ) : alignsShortcut && entry.shortcut !== undefined && <span className="size-6 shrink-0 pointer-coarse:hidden" />}
        </div>
      </CommandItem>
    )
  }

  return (
    <DialogContent
      data-command-palette=""
      showCloseButton={false}
      overlayClassName="bg-black/40 backdrop-blur-sm"
      aria-describedby={undefined}
      onOpenAutoFocus={returnFocus.remember}
      onCloseAutoFocus={returnFocus.restore}
      style={visibleHeight === null ? undefined : { "--visible-height": `${visibleHeight}px` } as CSSProperties}
      className="top-[calc(var(--visible-height,100vh)*0.12)] flex max-h-[calc(var(--visible-height,100vh)*0.76)] translate-y-0 flex-col gap-0 overflow-hidden rounded-2xl bg-popover p-0 text-popover-foreground shadow-2xl outline-none sm:max-w-2xl"
    >
      <DialogTitle className="sr-only">{t("common:commandPalette.title")}</DialogTitle>
      <Command
        shouldFilter={false}
        loop
        vimBindings={isMac}
        value={highlighted?.id ?? ""}
        onValueChange={id => dispatch({ type: "highlighted", id })}
        onKeyDown={onKeyDown}
        label={t("common:commandPalette.title")}
        className="h-auto min-h-0 rounded-none"
      >
        <div className="flex items-center gap-3 px-5">
          {parent ? (
            <button type="button" tabIndex={-1} onClick={back} aria-label={t("common:commandPalette.hints.back")} className="text-muted-foreground hover:text-foreground pointer-coarse:-m-3 pointer-coarse:p-3">
              <ArrowLeft className="size-5" />
            </button>
          ) : (
            <Search className="size-5 text-muted-foreground" />
          )}
          <CommandPrimitive.Input asChild value="" onValueChange={changeText}>
            <input
              ref={inputRef}
              {...passwordManagerAttributes()}
              value={state.query}
              aria-activedescendant={highlighted && rowId(highlighted)}
              placeholder={parent ? t("common:commandPalette.placeholderIn", { name: parent.label }) : t("common:commandPalette.placeholder")}
              className="h-14 min-w-0 flex-1 bg-transparent text-base outline-none placeholder:text-muted-foreground"
            />
          </CommandPrimitive.Input>
          <button
            type="button"
            onClick={close}
            onKeyDown={event => event.key === "Enter" && event.stopPropagation()}
            aria-label={t("common:actions.close")}
            className="-m-3 hidden p-3 text-muted-foreground pointer-coarse:block"
          >
            <X className="size-5" />
          </button>
        </div>
        <CommandList ref={listRef} onMouseDown={event => event.preventDefault()} className="max-h-[min(28rem,calc(var(--visible-height,100vh)*0.56))] scroll-py-2 overscroll-contain px-2 pb-2">
          {parent && <CommandEmpty className="py-8 text-center text-sm text-muted-foreground">{t("common:commandPalette.noMatchIn", { name: parent.label })}</CommandEmpty>}
          {groups.map(group => (
            <CommandGroup key={group.id} heading={group.label} className="p-0 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pt-3 [&_[cmdk-group-heading]]:pb-1.5">
              {group.entries.map(entry => renderRow(entry, !parent && group.entries.some(other => other.sections)))}
            </CommandGroup>
          ))}
        </CommandList>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t bg-foreground/[0.03] px-4 py-2.5 text-sm text-muted-foreground pointer-coarse:hidden">
          <Hint label={t("common:commandPalette.hints.navigate")}>
            <Kbd><ArrowUp className="size-3.5" /></Kbd>
            <Kbd><ArrowDown className="size-3.5" /></Kbd>
          </Hint>
          <Hint label={t("common:commandPalette.hints.open")}><Kbd>{keyLabels.enter}</Kbd></Hint>
          {moreOf && <Hint label={t("common:commandPalette.hints.more")}><Kbd><ArrowRight className="size-3.5" /></Kbd></Hint>}
          {torrents.entries.length > 0 && <Hint label={t("common:commandPalette.hints.searchTorrents")}><Kbd>{modShortcut(keyLabels.enter)}</Kbd></Hint>}
          {parent && <Hint label={t("common:commandPalette.hints.back")}><Kbd>{keyLabels.backspace}</Kbd></Hint>}
          <Hint label={t("common:actions.close")}><Kbd>{keyLabels.escape}</Kbd></Hint>
        </div>
      </Command>
    </DialogContent>
  )
}
