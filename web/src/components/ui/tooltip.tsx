/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
  registerPointerType?: (pointerType: string | null) => void
}>({ isTouchDevice: false })

const Tooltip = ({ children, ...props }: React.ComponentProps<typeof TooltipPrimitive.Root>) => {
  const isTouchDevice = useIsTouchDevice()
  const [open, setOpen] = React.useState(props.defaultOpen ?? false)

  // Track if the tooltip was opened via click (for touch devices)
  const openedViaClickRef = React.useRef(false)
  const lastPointerTypeRef = React.useRef<string | null>(null)

  const handleOpenChange = React.useCallback((nextOpen: boolean) => {
    // On touch devices, only allow opening via explicit click
    if (isTouchDevice && nextOpen && !openedViaClickRef.current) {
      const pointerType = lastPointerTypeRef.current

      // Block touch and programmatic (null) opens so sheets don't auto-focus the trigger
      if (pointerType === "touch" || pointerType === null) {
        return
      }
    }

    // Reset the click flag when closing
    if (!nextOpen) {
      openedViaClickRef.current = false
      lastPointerTypeRef.current = null
    }

    setOpen(nextOpen)
  }, [isTouchDevice])

  const handleSetOpen = React.useCallback((nextOpen: boolean) => {
    if (nextOpen) {
      // Mark that this was opened via click so touch can still toggle open
      openedViaClickRef.current = true
    } else {
      // Reset guards when closing manually so the next open is validated again
      openedViaClickRef.current = false
      lastPointerTypeRef.current = null
    }
    setOpen(nextOpen)
  }, [])

  const registerPointerType = React.useCallback((pointerType: string | null) => {
    lastPointerTypeRef.current = pointerType
  }, [])

  // Always use controlled state to avoid switching between controlled/uncontrolled
  // For touch devices, we prevent hover from opening via onOpenChange interception
  // For desktop, we let Radix's hover behavior trigger onOpenChange normally
  return (
    <TooltipContext.Provider value={{ isTouchDevice, isOpen: open, setOpen: handleSetOpen, registerPointerType }}>
      <TooltipPrimitive.Root open={open} onOpenChange={handleOpenChange} {...props}>
        {children}
      </TooltipPrimitive.Root>
    </TooltipContext.Provider>
  )
}

type TooltipTriggerProps = React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Trigger>
type TriggerPointerEvent = Parameters<NonNullable<TooltipTriggerProps["onPointerDown"]>>[0]
type TriggerKeyEvent = Parameters<NonNullable<TooltipTriggerProps["onKeyDown"]>>[0]

const TooltipTrigger = React.forwardRef<
  React.ComponentRef<typeof TooltipPrimitive.Trigger>,
  TooltipTriggerProps
>(({ onClick, onPointerDown, onPointerEnter, onKeyDown, ...props }, ref) => {
  const context = React.useContext(TooltipContext)

  const registerPointerType = context.registerPointerType

  const handlePointerDown = React.useCallback((event: TriggerPointerEvent) => {
    registerPointerType?.(event.pointerType)
    onPointerDown?.(event)
  }, [registerPointerType, onPointerDown])

  const handlePointerEnter = React.useCallback((event: TriggerPointerEvent) => {
    registerPointerType?.(event.pointerType)
    onPointerEnter?.(event)
  }, [registerPointerType, onPointerEnter])

  const handleKeyDown = React.useCallback((event: TriggerKeyEvent) => {
    registerPointerType?.("keyboard")
    onKeyDown?.(event)
  }, [registerPointerType, onKeyDown])

  if (context.isTouchDevice) {
    // On touch devices, handle click to toggle tooltip
    return (
      <TooltipPrimitive.Trigger
        ref={ref}
        onPointerDown={handlePointerDown}
        onPointerEnter={handlePointerEnter}
        onKeyDown={handleKeyDown}
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
      onPointerDown={handlePointerDown}
      onPointerEnter={handlePointerEnter}
      onKeyDown={handleKeyDown}
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

  const baseClasses = "bg-primary font-medium text-primary-foreground animate-in fade-in-0 zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 z-50 rounded-md px-3 py-1.5 text-xs text-pretty"

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
