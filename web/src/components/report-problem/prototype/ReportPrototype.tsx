/*
 * PROTOTYPE, throwaway. Three variants of "Report a problem", switchable with ?variant=A|B|C on
 * any page. Mounted from AppLayout in dev builds only. The entry point is the button in the
 * sidebar footer (SidebarEntry); variant C is a Settings tab (VariantSettings).
 */

import { ChevronLeft, ChevronRight } from "lucide-react"
import { useEffect } from "react"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import type { Identity, Lane, Source } from "./mock"
import { useOpenReport } from "./SidebarEntry"
import { keepOpenForSwitcher, setProto, useDesktop, useProto, type Variant } from "./store"
import { VariantNotice } from "./VariantNotice"
import { PopoverBody } from "./VariantPopover"

const variants: { key: Variant; name: string }[] = [
  { key: "A", name: "Notice dialog" },
  { key: "B", name: "Sidebar popover" },
  { key: "C", name: "Settings page" },
]

export function ReportPrototype() {
  const proto = useProto()
  const desktop = useDesktop()
  const openReport = useOpenReport()

  const index = Math.max(0, variants.findIndex((v) => v.key === proto.variant))
  const cycle = (step: number) => setProto({ variant: variants[(index + step + variants.length) % variants.length].key, open: false, view: "compose" })

  useEffect(() => {
    if (!proto.variant) return
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target?.closest("input, textarea, [contenteditable='true'], [role='dialog'], [role='menu']")) return
      if (event.key === "ArrowLeft") cycle(-1)
      if (event.key === "ArrowRight") cycle(1)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  if (!proto.variant) return null
  const current = variants[index]

  return (
    <>
      {proto.open && proto.variant === "A" && <VariantNotice />}
      {/* Below lg the sidebar is hidden: the popover content opens in a dialog, as a System notice does from a menu. */}
      {proto.open && proto.variant === "B" && !desktop && (
        <Dialog open onOpenChange={(open) => !open && setProto({ open: false, view: "compose" })}>
          <DialogContent className="max-h-[calc(100dvh-2rem)] gap-0 overflow-y-auto p-0 sm:max-w-sm" onInteractOutside={keepOpenForSwitcher}>
            <PopoverBody inDialog Title={({ className, children }) => <DialogTitle className={className}>{children}</DialogTitle>} />
          </DialogContent>
        </Dialog>
      )}

      <div data-proto-switcher className="pointer-events-auto fixed bottom-4 left-1/2 z-[200] flex -translate-x-1/2 flex-col items-center gap-1.5 text-xs max-lg:bottom-20">
        <div className="flex flex-wrap items-center justify-center gap-1 rounded-2xl bg-zinc-900 px-1.5 py-1 text-zinc-100 shadow-xl ring-1 ring-zinc-700">
          <Seg<Identity> value={proto.identity} options={[["anonymous", "Anonymous"], ["signed-in", "Signed in"]]} onChange={(identity) => setProto({ identity })} />
          <Seg<Lane> value={proto.lane} options={[["open", "Lane open"], ["full", "Lane full"]]} onChange={(lane) => setProto({ lane })} />
          <Seg<Source> value={proto.source} options={[["menu", "From the button"], ["error", "From an error"], ["search", "From a search"]]} onChange={(source) => setProto({ source })} />
          <button type="button" className="rounded-full px-2.5 py-1 text-zinc-300 hover:text-zinc-100" onClick={() => setProto({ notice: true, open: false })}>Show feature notice</button>
          <button type="button" className="rounded-full bg-zinc-100 px-2.5 py-1 font-medium text-zinc-900" onClick={() => { setProto({ notice: false }); openReport() }}>Open</button>
        </div>
        <div className="flex items-center gap-1 rounded-full bg-zinc-900 px-1 py-1 text-zinc-100 shadow-xl ring-1 ring-zinc-700">
          <button type="button" aria-label="Previous variant" className="rounded-full p-1.5 hover:bg-zinc-700" onClick={() => cycle(-1)}><ChevronLeft className="h-4 w-4" /></button>
          <span className="min-w-44 text-center font-medium tabular-nums">Prototype {current.key} ({current.name})</span>
          <button type="button" aria-label="Next variant" className="rounded-full p-1.5 hover:bg-zinc-700" onClick={() => cycle(1)}><ChevronRight className="h-4 w-4" /></button>
        </div>
      </div>
    </>
  )
}

function Seg<T extends string>(props: { value: T; options: [T, string][]; onChange: (value: T) => void }) {
  return (
    <div className="flex rounded-full bg-zinc-800 p-0.5">
      {props.options.map(([value, label]) => (
        <button
          key={value}
          type="button"
          onClick={() => props.onChange(value)}
          className={cn("rounded-full px-2.5 py-1 transition-colors", props.value === value ? "bg-zinc-100 text-zinc-900" : "text-zinc-300 hover:text-zinc-100")}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
