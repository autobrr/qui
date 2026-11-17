/*
 * PROTOTYPE, throwaway. Variant B: a popover from a button in the sidebar footer, beside the
 * language and GitHub buttons. It opens to the right, as the System notice rows in the sidebar do,
 * and lists what gets sent as rows like those notice rows. On a phone, the same content opens in a
 * dialog, as a System notice does from a menu.
 */

import { ChevronLeft, ChevronRight, History, Loader2, ScrollText, Server } from "lucide-react"
import { useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { logLineCount, logLines, qui, systemRows } from "./mock"
import { CauseBox, ErrorDot, LaneFullNote, PrivacyLine, SenderMenu, SentRow } from "./parts"
import { setProto, useProto } from "./store"
import { useComposer, useSentReports } from "./useComposer"

type TitleSlot = (props: { className?: string; children: ReactNode }) => ReactNode

/** inDialog leaves room for the close button of the dialog that holds it on a phone. */
export function PopoverBody({ Title, inDialog }: { Title: TitleSlot; inDialog?: boolean }) {
  const proto = useProto()
  return proto.view === "sent" ? <SentView Title={Title} /> : <ComposeView Title={Title} inDialog={inDialog} />
}

function ComposeView({ Title, inDialog }: { Title: TitleSlot; inDialog?: boolean }) {
  const c = useComposer({ onSent: () => setProto({ open: false }) })
  const [open, setOpen] = useState<null | "error" | "system" | "logs">(null)
  const toggle = (key: "error" | "system" | "logs") => setOpen(open === key ? null : key)

  return (
    <div className="text-[13px]">
      <div className={cn("flex items-center gap-2 px-3 pb-2 pt-3", inDialog && "pr-10")}>
        <Title className="text-sm font-semibold leading-none">Report a problem</Title>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Sent reports" className="ml-auto size-6 text-muted-foreground hover:text-foreground" onClick={() => setProto({ view: "sent" })}>
              <History className="size-3.5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="top" className="text-xs">Sent reports</TooltipContent>
        </Tooltip>
      </div>

      <div className="px-3">
        <Textarea
          autoFocus
          value={c.text}
          onChange={(e) => c.setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && c.canSend) c.send() }}
          placeholder={c.hasError ? "What were you doing? (optional)" : "What happened?"}
          className="min-h-20 resize-none text-[13px] leading-relaxed"
        />
      </div>

      <div className="space-y-0.5 px-1.5 pt-2">
        {c.hasError && (
          <>
            <Row icon={<ErrorDot />} label={c.error.title} onClick={() => toggle("error")} open={open === "error"} />
            {open === "error" && <CauseBox value={c.error.cause} copyLabel="Cause" className="mx-1.5 mb-1" />}
          </>
        )}
        <Row icon={<Server className="size-3.5" />} label={`qui ${qui.version}, ${qui.instances} instances`} onClick={() => toggle("system")} open={open === "system"} />
        {open === "system" && (
          <CauseBox value={systemRows.map((r) => `${r.label}: ${r.value}`).join("\n")} copyLabel="Details" className="mx-1.5 mb-1" />
        )}
        <Row
          icon={<ScrollText className="size-3.5" />}
          label={`${logLineCount} log lines`}
          muted={!c.logsIncluded}
          onClick={() => toggle("logs")}
          open={open === "logs"}
          control={<Switch aria-label="Send the log lines" checked={c.logsIncluded} onCheckedChange={c.setLogsIncluded} className="scale-75" />}
        />
        {open === "logs" && <CauseBox value={logLines.join("\n")} copyLabel="Log lines" className="mx-1.5 mb-1 max-h-28 overflow-y-auto" />}
      </div>

      {c.phase === "blocked" && <LaneFullNote c={c} className="mx-3 mt-2 border-t pt-3" />}

      <div className="mt-2 flex items-center gap-2 border-t px-3 py-2">
        <SenderMenu c={c} className="-ml-2" />
        <Button size="sm" className="ml-auto h-7 px-2.5 text-xs" disabled={!c.canSend} onClick={c.send}>
          {c.phase === "sending" && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
          {c.phase === "sending" ? "Sending" : "Send"}
        </Button>
      </div>
      <PrivacyLine c={c} className="px-3 pb-3 text-[11px] leading-4" />
    </div>
  )
}

function Row({ icon, label, muted, onClick, open, control }: { icon: ReactNode; label: string; muted?: boolean; onClick?: () => void; open?: boolean; control?: ReactNode }) {
  return (
    <div className="group flex items-center gap-1 rounded-md pr-1 hover:bg-accent/60">
      <button type="button" onClick={onClick} disabled={!onClick} aria-expanded={onClick ? open : undefined} className="flex min-w-0 flex-1 items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default">
        <span className="flex size-4 items-center justify-center text-muted-foreground">{icon}</span>
        <span className={cn("min-w-0 flex-1 truncate", muted && "text-muted-foreground line-through")}>{label}</span>
        {onClick && <ChevronRight className={cn("size-3.5 text-muted-foreground opacity-0 transition group-hover:opacity-100", open && "rotate-90 opacity-100")} />}
      </button>
      {control}
    </div>
  )
}

function SentView({ Title }: { Title: TitleSlot }) {
  const { rows, remove } = useSentReports()
  return (
    <div>
      <div className="flex items-center gap-1 px-1.5 pb-1 pt-2">
        <Button variant="ghost" size="icon" aria-label="Back" className="size-7 text-muted-foreground" onClick={() => setProto({ view: "compose" })}>
          <ChevronLeft className="size-4" />
        </Button>
        <Title className="text-sm font-semibold leading-none">Sent reports</Title>
      </div>
      <div className="space-y-0.5 px-1.5 pb-1.5">
        {rows.map((r) => <SentRow key={r.id} report={r} onDelete={() => remove(r.id)} />)}
      </div>
      <p className="border-t px-3 py-2 text-[11px] leading-4 text-muted-foreground">The status updates each time you open this list.</p>
    </div>
  )
}
