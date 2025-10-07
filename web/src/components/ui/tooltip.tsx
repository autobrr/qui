/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as TooltipPrimitive from "@radix-ui/react-tooltip"
import * as React from "react"

import { cn } from "@/lib/utils"

// Hook to detect touch devices
function useIsTouchDevice() {
  const [isTouchDevice, setIsTouchDevice] = React.useState(false)

  React.useEffect(() => {
    const checkTouchDevice = () => {
      setIsTouchDevice("ontouchstart" in window || navigator.maxTouchPoints > 0)
    }

    checkTouchDevice()
    // Re-check on resize in case device orientation changes
    window.addEventListener("resize", checkTouchDevice)
    return () => window.removeEventListener("resize", checkTouchDevice)
  }, [])

  return isTouchDevice
}

const TooltipProvider = TooltipPrimitive.Provider

// Context to share tooltip state between components
const TooltipContext = React.createContext<{
  isTouchDevice: boolean
  isOpen?: boolean
  setOpen?: (open: boolean) => void
}>({ isTouchDevice: false })

const Tooltip = ({ children, ...props }: React.ComponentProps<typeof TooltipPrimitive.Root>) => {
  const isTouchDevice = useIsTouchDevice()
  const [open, setOpen] = React.useState(false)

  if (isTouchDevice) {
    // On touch devices, use controlled open state with click/touch to toggle
    return (
      <TooltipContext.Provider value={{ isTouchDevice, isOpen: open, setOpen }}>
        <TooltipPrimitive.Root open={open} onOpenChange={setOpen} {...props}>
          {children}
        </TooltipPrimitive.Root>
      </TooltipContext.Provider>
    )
  }

  // On desktop, use default hover behavior
  return (
    <TooltipContext.Provider value={{ isTouchDevice }}>
      <TooltipPrimitive.Root {...props}>
        {children}
      </TooltipPrimitive.Root>
    </TooltipContext.Provider>
  )
}

const TooltipTrigger = React.forwardRef<
  React.ComponentRef<typeof TooltipPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Trigger>
>(({ onClick, ...props }, ref) => {
  const context = React.useContext(TooltipContext)

  if (context.isTouchDevice) {
    // On touch devices, handle click to toggle tooltip
    return (
      <TooltipPrimitive.Trigger
        ref={ref}
        onClick={(e) => {
          // Toggle tooltip on mobile
          if (context.setOpen) {
            context.setOpen(!context.isOpen)
          }
          // Also call any custom onClick
          onClick?.(e)
        }}
        {...props}
      />
    )
  }

  // On desktop, use default behavior
  return (
    <TooltipPrimitive.Trigger
      ref={ref}
      onClick={onClick}
      {...props}
    />
  )
})
TooltipTrigger.displayName = TooltipPrimitive.Trigger.displayName

const TooltipContent = React.forwardRef<
  React.ComponentRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 4, children, ...props }, ref) => {
  const context = React.useContext(TooltipContext)

  const baseClasses = "bg-primary font-medium text-primary-foreground animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 rounded-md px-3 py-1.5 text-xs text-balance"

  // Add mobile-specific classes for better text wrapping
  const mobileClasses = context.isTouchDevice? "max-w-[calc(100vw-2rem)] break-words w-fit": "w-fit origin-(--radix-tooltip-content-transform-origin)"

  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn(baseClasses, mobileClasses, className)}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="fill-primary" />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  )
})
TooltipContent.displayName = TooltipPrimitive.Content.displayName

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger }

