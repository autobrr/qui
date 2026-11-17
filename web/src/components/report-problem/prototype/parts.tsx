/*
 * PROTOTYPE, throwaway. Pieces the three variants share. Each one copies an existing qui pattern:
 * the System notice dot, detail, and menu row, the new-feature bubble, and the header menus.
 */

import * as PopperPrimitive from "@radix-ui/react-popper"
import { Check, ChevronDown, Loader2, Trash2, UserRound, VenetianMask } from "lucide-react"
import { useId, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import { featureNotice, laneFull, privacyText, signedInUser, statusText, type ReportStatus, type SentReport } from "./mock"
import type { Composer } from "./useComposer"

const statusDot: Record<ReportStatus, string> = {
  "received": "bg-muted-foreground ring-muted-foreground/20",
  "issue": "bg-green-500 ring-green-500/20",
  "closed": "bg-transparent ring-0 border border-muted-foreground/60",
  "not-sent": "bg-yellow-500 ring-yellow-500/20",
}

/** The System notice dot, with one color for each report status. */
export function StatusDot({ status, className }: { status: ReportStatus; className?: string }) {
  return <span className={cn("inline-flex h-2 w-2 shrink-0 rounded-full ring-[3px]", statusDot[status], className)} />
}

/** The dot of a System notice, for the error that the report is about. */
export function ErrorDot({ className }: { className?: string }) {
  return <span className={cn("inline-flex h-2 w-2 shrink-0 rounded-full bg-destructive ring-[3px] ring-destructive/20", className)} />
}

/** The cause box of a System notice: monospace text and a copy button. */
export function CauseBox({ value, copyLabel, className }: { value: string; copyLabel: string; className?: string }) {
  return (
    <div className={cn("flex items-start gap-1 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1", className)}>
      <code className="min-w-0 flex-1 break-words whitespace-pre-wrap py-0.5 font-mono text-[11px] leading-relaxed text-muted-foreground">{value}</code>
      <CopyButton value={value} label={copyLabel} className="size-6" iconClassName="size-3" />
    </div>
  )
}

/** Who sends the report. Same trigger and items as the header menus. */
export function SenderMenu({ c, variant = "ghost", className }: { c: Composer; variant?: "ghost" | "outline"; className?: string }) {
  if (c.signingIn) {
    return (
      <Button variant={variant} size="sm" disabled className={cn("h-7 gap-1.5 px-2 text-xs", className)}>
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        Waiting for {c.signingIn}
      </Button>
    )
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant={variant} size="sm" className={cn("h-7 gap-1.5 px-2 text-xs", variant === "ghost" && "text-muted-foreground hover:text-foreground", className)}>
          {c.anonymous ? <VenetianMask className="h-3.5 w-3.5" /> : <UserRound className="h-3.5 w-3.5" />}
          {c.anonymous ? "Anonymous" : signedInUser.name}
          <ChevronDown className="h-3 w-3 opacity-60" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuLabel className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">Send as</DropdownMenuLabel>
        <DropdownMenuItem onSelect={c.signOut} className="justify-between">
          <span className="flex items-center gap-2"><VenetianMask className="h-4 w-4" />Anonymous</span>
          {c.anonymous && <Check className="h-3.5 w-3.5" />}
        </DropdownMenuItem>
        {!c.anonymous && (
          <DropdownMenuItem className="justify-between">
            <span className="flex items-center gap-2"><UserRound className="h-4 w-4" />{signedInUser.name}<span className="text-xs text-muted-foreground">{signedInUser.provider}</span></span>
            <Check className="h-3.5 w-3.5" />
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        {c.anonymous ? (
          <>
            <DropdownMenuItem onSelect={() => c.signIn("Discord")}>Sign in with Discord</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => c.signIn("GitHub")}>Sign in with GitHub</DropdownMenuItem>
          </>
        ) : (
          <DropdownMenuItem onSelect={c.signOut}>Sign out</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function PrivacyLine({ c, className }: { c: Composer; className?: string }) {
  return (
    <p className={cn("text-xs leading-5 text-muted-foreground text-pretty", className)}>
      {c.anonymous ? privacyText.anonymous : privacyText.signedIn}{" "}
      <a href="https://bin.getqui.com/privacy" target="_blank" rel="noopener noreferrer" className="underline underline-offset-2 hover:text-foreground">Privacy</a>
    </p>
  )
}

/** Shown after Send while the anonymous lane is full. Laid out like a System notice. */
export function LaneFullNote({ c, gutter, className }: { c: Composer; gutter?: boolean; className?: string }) {
  return (
    <div role="status" className={cn(gutter ? "relative" : "flex items-start gap-3", className)}>
      <StatusDot status="not-sent" className={gutter ? "absolute -left-6 top-1.5" : "mt-1.5"} />
      <div className="min-w-0">
        <p className="text-sm font-semibold leading-snug">{laneFull.title}</p>
        <p className="mt-0.5 text-[13px] leading-relaxed text-muted-foreground">{laneFull.body}</p>
        <div className="mt-2 flex flex-wrap gap-2">
          <Button size="sm" variant="outline" className="h-7 px-2.5 text-xs" disabled={!!c.signingIn} onClick={() => c.signIn("Discord")}>Sign in with Discord</Button>
          <Button size="sm" variant="outline" className="h-7 px-2.5 text-xs" disabled={!!c.signingIn} onClick={() => c.signIn("GitHub")}>Sign in with GitHub</Button>
        </div>
      </div>
    </div>
  )
}

/** A sent report, laid out like a System notice row in the header menu. */
export function SentRow({ report, onDelete }: { report: SentReport; onDelete: () => void }) {
  return (
    <div className="group flex items-start gap-3 rounded-md px-2 py-1.5 hover:bg-accent/60">
      <span className="mt-1.5 flex h-2 w-4 items-center justify-center"><StatusDot status={report.status} /></span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{report.title}</span>
        <span className={cn("block truncate text-xs", report.status === "issue" ? "text-foreground" : "text-muted-foreground")}>
          {report.status === "issue" ? <a href="#issue" className="underline-offset-2 hover:underline">{statusText(report)}</a> : statusText(report)}
        </span>
      </span>
      {report.status === "not-sent" ? (
        <Button size="sm" variant="outline" className="mt-0.5 h-6 px-2 text-xs">Send</Button>
      ) : (
        <span className="mt-0.5 text-xs tabular-nums text-muted-foreground group-hover:hidden">{report.age}</span>
      )}
      <Button variant="ghost" size="icon" aria-label="Delete report" onClick={onDelete} className={cn("size-6 text-muted-foreground hover:text-foreground", report.status !== "not-sent" && "hidden group-hover:inline-flex")}>
        <Trash2 className="size-3.5" />
      </Button>
    </div>
  )
}

/** A small drawing of the current page, for the screenshot. */
export function Thumb({ className }: { className?: string }) {
  return (
    <span aria-hidden className={cn("flex shrink-0 overflow-hidden rounded-sm border bg-background", className)}>
      <span className="w-1/5 border-r bg-muted/60" />
      <span className="flex flex-1 flex-col gap-[3px] p-1">
        <span className="h-[3px] w-2/3 rounded-full bg-muted-foreground/40" />
        <span className="h-[3px] w-full rounded-full bg-muted-foreground/20" />
        <span className="h-[3px] w-full rounded-full bg-muted-foreground/20" />
        <span className="h-[3px] w-4/5 rounded-full bg-muted-foreground/20" />
      </span>
    </span>
  )
}

/**
 * The new-feature bubble of qui (FeatureNotice), copied so that the prototype does not add a
 * real entry to FEATURE_NOTICES.
 */
export function FeatureBubble({ open, side = "top", onDismiss, onAction, children }: { open: boolean; side?: "top" | "bottom" | "right"; onDismiss: () => void; onAction: () => void; children: ReactNode }) {
  const titleId = useId()
  return (
    <PopperPrimitive.Root>
      <PopperPrimitive.Anchor asChild className={open ? "motion-safe:animate-feature-notice-pulse motion-reduce:ring-2 motion-reduce:ring-primary/30" : undefined}>
        {children}
      </PopperPrimitive.Anchor>
      {open && (
        <PopperPrimitive.Content
          side={side}
          sideOffset={5}
          collisionPadding={8}
          role="region"
          aria-labelledby={titleId}
          className="z-50 w-80 origin-(--radix-popper-transform-origin) rounded-xl border border-foreground/15 bg-popover p-3 text-popover-foreground shadow-xl outline-none motion-safe:animate-in motion-safe:fade-in-0 motion-safe:zoom-in-95 motion-safe:data-[side=bottom]:slide-in-from-top-1 motion-safe:data-[side=top]:slide-in-from-bottom-1"
        >
          <p id={titleId} className="text-sm font-semibold leading-5">{featureNotice.title}</p>
          <p className="mt-1 text-sm leading-5 text-muted-foreground text-pretty">{featureNotice.description}</p>
          <div className="mt-3 flex justify-end gap-1">
            <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={onDismiss}>Got it</Button>
            <Button size="sm" onClick={onAction}>{featureNotice.action}</Button>
          </div>
          <PopperPrimitive.Arrow asChild>
            <svg width="20" height="10" viewBox="0 0 20 10" className="block overflow-visible">
              <path d="M0 -2 L0 0 L10 9 L20 0 L20 -2 Z" className="fill-popover" />
              <path d="M0 -0.5 L10 8.5 L20 -0.5" fill="none" className="stroke-foreground/15" />
            </svg>
          </PopperPrimitive.Arrow>
        </PopperPrimitive.Content>
      )}
    </PopperPrimitive.Root>
  )
}
