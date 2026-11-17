/*
 * PROTOTYPE, throwaway. Variant A: a dialog laid out like the detail of a System notice
 * (SystemNoticeDialog): icon and title, body indented under the title, the cause box, and the
 * action bar with small buttons at the bottom. From 1024 px wide it adds a second column that
 * shows everything qui sends, in the row style of Settings > Application, with nothing to open.
 */

import { ChevronRight, ImagePlus, Loader2, MessageSquareWarning, X } from "lucide-react"
import { useRef, useState, type ClipboardEvent, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { CopyButton } from "@/components/ui/copy-button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"
import { allLogLineCount, allLogLines, logLineCount, logLines, qui, searchBreakdown, searchContext, sentReports, systemRows } from "./mock"
import { CauseBox, ErrorDot, LaneFullNote, PrivacyLine, SenderMenu, SentRow, StatusDot } from "./parts"
import { keepOpenForSwitcher, setProto, useDesktop, useProto } from "./store"
import { useComposer, useSentReports, type Composer } from "./useComposer"

export function VariantNotice() {
  const proto = useProto()
  const desktop = useDesktop()
  const close = () => setProto({ open: false, view: "compose" })
  const wide = desktop && proto.view === "compose"
  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent
        className={cn(
          "gap-0 p-0",
          wide ? "flex h-[min(44rem,calc(100dvh-4rem))] flex-col overflow-hidden sm:max-w-4xl xl:max-w-5xl" : "max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md",
        )}
        onInteractOutside={keepOpenForSwitcher}
      >
        {proto.view === "sent" ? <SentView /> : wide ? <WideComposeView onSent={close} /> : <ComposeView onSent={close} />}
      </DialogContent>
    </Dialog>
  )
}

function Header({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="flex items-start gap-3 px-4 pr-10 pt-4">
      <MessageSquareWarning className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <DialogTitle className="text-sm font-semibold leading-snug">{title}</DialogTitle>
        <DialogDescription className="mt-0.5 text-xs text-muted-foreground">{subtitle}</DialogDescription>
      </div>
    </div>
  )
}

/** A pasted image anywhere in the dialog becomes the attachment, for signed-in users. */
function pasteImage(c: Composer) {
  return (event: ClipboardEvent) => {
    if (!c.imageAllowed) return
    const file = [...event.clipboardData.files].find((f) => f.type.startsWith("image/"))
    if (!file) return
    event.preventDefault()
    c.setImage(file)
  }
}

function placeholder(c: Composer) {
  if (c.hasSearch) return "Which match did you expect, and on which indexer?"
  if (c.hasError) return "What were you doing when it happened?"
  return "What happened, and what did you expect?"
}

function ReportText({ c, className }: { c: Composer; className?: string }) {
  return (
    <Textarea
      autoFocus
      required
      aria-label="Your report"
      value={c.text}
      onChange={(e) => c.setText(e.target.value)}
      onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && c.canSend) c.send() }}
      placeholder={placeholder(c)}
      className={cn("resize-none text-[13px] leading-relaxed", className)}
    />
  )
}

function ActionBar({ c }: { c: Composer }) {
  return (
    <div className="flex shrink-0 items-center gap-2 border-t px-4 py-2.5 pl-11">
      <Button size="sm" className="h-7 px-2.5 text-xs" disabled={!c.canSend} onClick={c.send}>
        {c.phase === "sending" && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
        {c.phase === "sending" ? "Sending" : "Send report"}
      </Button>
      <SenderMenu c={c} />
      <Button variant="ghost" size="sm" className="ml-auto h-7 px-2 text-xs text-muted-foreground hover:text-foreground" onClick={() => setProto({ view: "sent" })}>
        Sent reports <span className="tabular-nums">{sentReports.length}</span>
      </Button>
    </div>
  )
}

function Context({ c }: { c: Composer }) {
  if (c.hasError) {
    return (
      <div className="relative">
        <ErrorDot className="absolute -left-6 top-1.5" />
        <p className="text-[13px] font-medium leading-snug">{c.error.title}</p>
        <p className="text-xs text-muted-foreground">Since {c.error.since}</p>
        <CauseBox value={c.error.cause} copyLabel="Cause" className="mt-2" />
      </div>
    )
  }
  if (c.hasSearch) {
    return (
      <div className="relative">
        <StatusDot status="received" className="absolute -left-6 top-1.5" />
        <p className="break-words text-[13px] font-medium leading-snug">{searchContext.title}</p>
        <p className="text-xs text-muted-foreground">{searchContext.summary}</p>
      </div>
    )
  }
  return null
}

/** Below 1024 px: one column, and the details open on a click. */
function ComposeView({ onSent }: { onSent: () => void }) {
  const c = useComposer({ onSent })
  return (
    <div onPaste={pasteImage(c)}>
      <Header title="Report a problem" subtitle="Goes to the qui maintainers." />
      <div className="space-y-3 px-4 pb-4 pl-11 pt-3">
        <Context c={c} />
        <ReportText c={c} className="min-h-24" />
        <Attached c={c} />
        {c.phase === "blocked" ? <LaneFullNote c={c} gutter className="pt-1" /> : <PrivacyLine c={c} />}
      </div>
      <ActionBar c={c} />
    </div>
  )
}

/** From 1024 px: the report on the left, everything qui sends on the right. */
function WideComposeView({ onSent }: { onSent: () => void }) {
  const c = useComposer({ onSent })
  return (
    <div className="flex min-h-0 flex-1 flex-col" onPaste={pasteImage(c)}>
      <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(0,25rem)] xl:grid-cols-[minmax(0,1fr)_minmax(0,32rem)]">
        <div className="flex min-h-0 flex-col overflow-y-auto">
          <Header title="Report a problem" subtitle="Goes to the qui maintainers." />
          <div className="flex flex-1 flex-col gap-3 px-4 pb-4 pl-11 pt-3">
            <Context c={c} />
            <ReportText c={c} className="min-h-32 flex-1" />
            {c.imageAllowed ? <ImageDrop c={c} /> : <p className="text-xs text-muted-foreground">Sign in to add an image.</p>}
            {c.phase === "blocked" ? <LaneFullNote c={c} gutter className="pt-1" /> : <PrivacyLine c={c} />}
          </div>
        </div>
        <WhatGetsSent c={c} />
      </div>
      <ActionBar c={c} />
    </div>
  )
}

function WhatGetsSent({ c }: { c: Composer }) {
  return (
    <aside aria-label="What gets sent" className="min-h-0 overflow-y-auto border-l bg-muted/30 pb-4">
      <div className="px-4 pr-10 pt-4">
        <p className="text-sm font-semibold leading-snug">What gets sent</p>
        <p className="mt-0.5 text-xs text-muted-foreground">qui fills this in. Nothing leaves qui before you press Send.</p>
      </div>

      <Rows rows={systemRows.map((r) => ({ label: r.label, value: r.value, secondary: r.secondary, mono: r.monospace }))} className="mt-3" />

      {c.hasSearch && (
        <Part
          id="proto-a-wide-breakdown"
          checked={c.breakdownIncluded}
          onChecked={c.setBreakdownIncluded}
          title="Search breakdown"
          note="From the search above. Includes up to 5 rejected release names for each reason."
        >
          <Rows
            className={cn("rounded-md border", !c.breakdownIncluded && "opacity-40")}
            rows={[
              ...searchBreakdown.rejections.map((r) => ({ label: r.reason, value: String(r.count) })),
              ...searchBreakdown.indexers.map((i) => ({ label: i.name, value: i.status === "searched" ? `Searched, ${i.candidates} candidates` : "Incomplete, rate limited" })),
            ]}
          />
        </Part>
      )}

      <Part
        id="proto-a-wide-logs"
        checked={c.logsIncluded}
        onChecked={c.setLogsIncluded}
        title="Logs"
        control={<LogScope c={c} />}
        note={c.logScope === "all"
          ? `The last ${allLogLineCount.toLocaleString()} lines at your log level, DEBUG. Keys removed.`
          : `The last ${logLineCount} warnings and errors. Keys removed.`}
      >
        <LogBox lines={c.logScope === "all" ? allLogLines : logLines} className={cn("transition-opacity", !c.logsIncluded && "opacity-40")} />
      </Part>
    </aside>
  )
}

function Rows({ rows, className }: { rows: { label: string; value: string; secondary?: string; mono?: boolean }[]; className?: string }) {
  return (
    <dl className={cn("divide-y border-y", className)}>
      {rows.map((row) => (
        <div key={row.label} className="flex items-baseline gap-3 px-4 py-2">
          <dt className="w-28 shrink-0 text-xs uppercase text-muted-foreground xl:w-36">{row.label}</dt>
          <dd className="min-w-0 flex-1">
            <p className={cn("break-words", row.mono ? "font-mono text-xs" : "text-[13px] font-medium")}>{row.value}</p>
            {row.secondary && <p className="text-xs text-muted-foreground">{row.secondary}</p>}
          </dd>
        </div>
      ))}
    </dl>
  )
}

/** One part of the report that the user can leave out. */
function Part({ id, checked, onChecked, title, control, note, children }: { id: string; checked: boolean; onChecked: (value: boolean) => void; title: string; control?: ReactNode; note: string; children: ReactNode }) {
  return (
    <div className="space-y-2 px-4 pt-4">
      <div className="flex items-center gap-2 text-[13px]">
        <Checkbox id={id} checked={checked} onCheckedChange={(v) => onChecked(v === true)} />
        <label htmlFor={id} className={cn("min-w-0 flex-1 font-medium", !checked && "text-muted-foreground line-through")}>{title}</label>
        {control}
      </div>
      {children}
      <p className="text-xs text-muted-foreground">{note}</p>
    </div>
  )
}

function LogScope({ c }: { c: Composer }) {
  return (
    <Select value={c.logScope} onValueChange={(v) => c.setLogScope(v as "problems" | "all")} disabled={!c.logsIncluded}>
      <SelectTrigger size="sm" aria-label="Which log lines" className="h-7 gap-1 px-2 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        <SelectItem value="problems" className="text-xs">Warnings and errors</SelectItem>
        <SelectItem value="all" className="text-xs">Everything recent</SelectItem>
      </SelectContent>
    </Select>
  )
}

/** The cause box, with one block per log line, so that a wrapped line hangs under its own start. */
function LogBox({ lines, className }: { lines: string[]; className?: string }) {
  return (
    <div className={cn("flex items-start gap-1 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1", className)}>
      <code className="max-h-52 min-w-0 flex-1 overflow-y-auto py-0.5 font-mono text-[11px] leading-relaxed text-muted-foreground">
        {lines.map((line, i) => <span key={i} className="block break-words pl-4 -indent-4">{line}</span>)}
      </code>
      <CopyButton value={lines.join("\n")} label="Log lines" className="size-6" iconClassName="size-3" />
    </div>
  )
}

/** Signed-in users can add one image: paste it, drop it, or pick a file. */
function ImageDrop({ c, compact }: { c: Composer; compact?: boolean }) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const pick = (
    <input ref={input} type="file" accept="image/*" hidden onChange={(e) => { const f = e.target.files?.[0]; if (f) c.setImage(f); e.target.value = "" }} />
  )
  if (c.image) {
    return (
      <div className="relative w-fit">
        <img src={c.image.url} alt={c.image.name} className={cn("rounded-md border object-contain", compact ? "max-h-24" : "max-h-36")} />
        <Button variant="secondary" size="icon" aria-label="Remove the image" className="absolute right-1 top-1 size-6 shadow" onClick={() => c.setImage(null)}>
          <X className="size-3.5" />
        </Button>
      </div>
    )
  }
  if (compact) {
    return (
      <>
        <Button variant="ghost" size="sm" className="-ml-2 h-7 gap-1.5 px-2 text-xs text-muted-foreground hover:text-foreground" onClick={() => input.current?.click()}>
          <ImagePlus className="size-3.5" />Add an image
        </Button>
        {pick}
      </>
    )
  }
  return (
    <>
      <button
        type="button"
        onClick={() => input.current?.click()}
        onDragOver={(e) => { e.preventDefault(); setOver(true) }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault()
          setOver(false)
          const file = [...e.dataTransfer.files].find((f) => f.type.startsWith("image/"))
          if (file) c.setImage(file)
        }}
        className={cn(
          "flex h-14 w-full shrink-0 items-center justify-center gap-2 rounded-md border border-dashed text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground",
          over && "border-primary bg-primary/5 text-foreground",
        )}
      >
        <ImagePlus className="size-4" />
        Add an image: paste it, drop it here, or choose a file
      </button>
      {pick}
    </>
  )
}

function Attached({ c }: { c: Composer }) {
  const [open, setOpen] = useState<null | "details" | "breakdown" | "logs">(null)
  const toggle = (key: "details" | "breakdown" | "logs") => setOpen(open === key ? null : key)
  return (
    <div className="space-y-1.5 text-[13px]">
      <p className="text-xs text-muted-foreground">Sent with it</p>
      <div className="flex items-center gap-2">
        <Checkbox checked disabled aria-label="Always sent" />
        <span className="min-w-0 flex-1">qui {qui.version} and {qui.instances} instances</span>
        <Disclose open={open === "details"} onClick={() => toggle("details")} label="Details" />
      </div>
      {open === "details" && <CauseBox value={systemRows.map((r) => `${r.label}: ${r.value}${r.secondary ? `, ${r.secondary.toLowerCase()}` : ""}`).join("\n")} copyLabel="Details" className="ml-6" />}
      {c.hasSearch && (
        <>
          <div className="flex items-center gap-2">
            <Checkbox id="proto-a-breakdown" checked={c.breakdownIncluded} onCheckedChange={(v) => c.setBreakdownIncluded(v === true)} />
            <label htmlFor="proto-a-breakdown" className={cn("min-w-0 flex-1", !c.breakdownIncluded && "text-muted-foreground line-through")}>Search breakdown</label>
            <Disclose open={open === "breakdown"} onClick={() => toggle("breakdown")} label="View" />
          </div>
          {open === "breakdown" && (
            <CauseBox
              value={[...searchBreakdown.rejections.map((r) => `${r.reason}: ${r.count}`), ...searchBreakdown.indexers.map((i) => `${i.name}: ${i.status}, ${i.candidates} candidates`)].join("\n")}
              copyLabel="Search breakdown"
              className="ml-6"
            />
          )}
        </>
      )}
      <div className="flex items-center gap-2">
        <Checkbox id="proto-a-logs" checked={c.logsIncluded} onCheckedChange={(v) => c.setLogsIncluded(v === true)} />
        <label htmlFor="proto-a-logs" className={cn("min-w-0 flex-1", !c.logsIncluded && "text-muted-foreground line-through")}>Logs</label>
        <LogScope c={c} />
        <Disclose open={open === "logs"} onClick={() => toggle("logs")} label="View" />
      </div>
      {open === "logs" && <LogBox lines={c.logScope === "all" ? allLogLines : logLines} className="ml-6" />}
      {c.imageAllowed && <div className="pt-1"><ImageDrop c={c} compact /></div>}
    </div>
  )
}

function Disclose({ open, onClick, label }: { open: boolean; onClick: () => void; label: string }) {
  return (
    <button type="button" onClick={onClick} aria-expanded={open} className="flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground">
      {label}
      <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
    </button>
  )
}

function SentView() {
  const { rows, remove } = useSentReports()
  return (
    <>
      <Header title="Sent reports" subtitle="The status updates each time you open this list." />
      <div className="space-y-0.5 px-2 pb-3 pl-9 pt-3">
        {rows.map((r) => <SentRow key={r.id} report={r} onDelete={() => remove(r.id)} />)}
      </div>
      <div className="flex items-center gap-2 border-t px-4 py-2.5 pl-11">
        <Button size="sm" variant="outline" className="h-7 px-2.5 text-xs" onClick={() => setProto({ view: "compose" })}>New report</Button>
      </div>
    </>
  )
}
