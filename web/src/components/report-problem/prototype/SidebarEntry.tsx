/*
 * PROTOTYPE, throwaway. The entry point in the sidebar footer, beside the language and GitHub
 * buttons, with the new-feature bubble. Mounted from Sidebar in dev builds only.
 */

import { useNavigate } from "@tanstack/react-router"
import { MessageSquareWarning } from "lucide-react"
import { useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { FeatureBubble } from "./parts"
import { keepOpenForSwitcher, setProto, useDesktop, useProto } from "./store"
import { PopoverBody } from "./VariantPopover"

export function useOpenReport() {
  const navigate = useNavigate()
  const { variant } = useProto()
  return () => {
    if (variant === "C") void navigate({ to: "/settings", search: { tab: "reports" as never } })
    else setProto({ open: true, view: "compose" })
  }
}

export function ProtoSidebarEntry() {
  const proto = useProto()
  const openReport = useOpenReport()
  const desktop = useDesktop()
  const buttonRef = useRef<HTMLButtonElement>(null)
  const [offset, setOffset] = useState(16)

  if (!proto.variant) return null

  const open = () => {
    setProto({ notice: false })
    // Opens to the right of the sidebar, as the System notice rows do.
    const button = buttonRef.current
    const sidebar = button?.closest("[data-qui-area='app-sidebar']")
    if (button && sidebar) setOffset(sidebar.getBoundingClientRect().right - button.getBoundingClientRect().right + 16)
    openReport()
  }

  const button = (
    <Button
      ref={buttonRef}
      variant="ghost"
      size="icon"
      aria-label="Report a problem"
      className="h-6 w-6 text-sidebar-foreground/40 hover:text-sidebar-foreground data-[state=open]:text-sidebar-foreground"
      onClick={proto.variant === "B" ? undefined : open}
    >
      <MessageSquareWarning className="h-3.5 w-3.5" />
    </Button>
  )

  const withTooltip = (trigger: React.ReactNode) => (
    <Tooltip>
      <TooltipTrigger asChild>{trigger}</TooltipTrigger>
      <TooltipContent side="top" className="text-xs">Report a problem</TooltipContent>
    </Tooltip>
  )

  return (
    <FeatureBubble
      open={desktop && proto.notice && !proto.open}
      onDismiss={() => setProto({ notice: false })}
      onAction={open}
    >
      <span className="inline-flex rounded-md">
        {proto.variant === "B" ? (
          <Popover open={proto.open && desktop} onOpenChange={(next) => (next ? open() : setProto({ open: false, view: "compose" }))}>
            {withTooltip(<PopoverTrigger asChild>{button}</PopoverTrigger>)}
            <PopoverContent
              side="right"
              align="end"
              sideOffset={offset}
              className="w-[22rem] p-0"
              onInteractOutside={keepOpenForSwitcher}
            >
              <PopoverBody Title={({ className, children }) => <p className={className}>{children}</p>} />
            </PopoverContent>
          </Popover>
        ) : (
          withTooltip(button)
        )}
      </span>
    </FeatureBubble>
  )
}
