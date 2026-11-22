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

function TooltipProvider({ delayDuration = 400, ...props }: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return <TooltipPrimitive.Provider delayDuration={delayDuration} {...props} />
}

// Context to share tooltip state between components
const TooltipContext = React.createContext<{
  isTouchDevice: boolean
  isOpen?: boolean
  setOpen?: (open: boolean) => void
  registerPointerType?: (pointerType: string | null) => void
}>({ isTouchDevice: false })

type TooltipProps = React.ComponentProps<typeof TooltipPrimitive.Root> & {
  /** Keeps the tooltip closed. For a trigger that stays mounted in a state where it needs no tooltip. */
  disabled?: boolean
}

const Tooltip = ({ children, disabled = false, ...props }: TooltipProps) => {
  const isTouchDevice = useIsTouchDevice()
  const [open, setOpen] = React.useState(props.defaultOpen ?? false)

  // Track if the tooltip was opened via click (for touch devices)
  const openedViaClickRef = React.useRef(false)
  const lastPointerTypeRef = React.useRef<string | null>(null)

  if (disabled && open) {
    openedViaClickRef.current = false
    lastPointerTypeRef.current = null
    setOpen(false)
  }

  const handleOpenChange = React.useCallback((nextOpen: boolean) => {
    if (disabled && nextOpen) {
      return
    }

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
  }, [isTouchDevice, disabled])

  const handleSetOpen = React.useCallback((nextOpen: boolean) => {
    if (disabled && nextOpen) {
      return
    }
    if (nextOpen) {
      // Mark that this was opened via click so touch can still toggle open
      openedViaClickRef.current = true
    } else {
      // Reset guards when closing manually so the next open is validated again
      openedViaClickRef.current = false
      lastPointerTypeRef.current = null
    }
    setOpen(nextOpen)
  }, [disabled])

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

type TooltipTriggerProps = React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Trigger> & {
  /**
   * What a tap on a touch device does. Without it, a tap opens the tooltip unless the tap also acts: it reaches an
   * enabled link, button, form field, label of an enabled field, option, or menu item at the trigger or around it. A
   * bare trigger (no asChild) opens on every tap. true opens it, also when the tap reaches a control around the trigger;
   * false keeps it closed.
   * "only" opens it and stops the click from bubbling to the row around the trigger, whose action would cover it.
   */
  openOnTap?: boolean | "only"
}
type TriggerPointerEvent = Parameters<NonNullable<TooltipTriggerProps["onPointerDown"]>>[0]
type TriggerKeyEvent = Parameters<NonNullable<TooltipTriggerProps["onKeyDown"]>>[0]

const CONTROL_SELECTOR = "a[href], button, input, select, textarea, label, [role=button], [role=menuitem], [role=option]"

function tapActs(target: EventTarget) {
  const control = target instanceof Element ? target.closest(CONTROL_SELECTOR) : null
  const activated = control instanceof HTMLLabelElement ? control.control : control
  return activated !== null && !activated.matches(":disabled, [aria-disabled=true]")
}

const TooltipTrigger = React.forwardRef<
  React.ComponentRef<typeof TooltipPrimitive.Trigger>,
  TooltipTriggerProps
>(({ openOnTap, onClick, onPointerDown, onPointerEnter, onKeyDown, ...props }, ref) => {
  const context = React.useContext(TooltipContext)

  const registerPointerType = context.registerPointerType
  // iOS Safari reports a tap as a mouse click, so the type comes from pointer down.
  const pressPointerTypeRef = React.useRef<string | null>(null)

  const handlePointerDown = React.useCallback((event: TriggerPointerEvent) => {
    pressPointerTypeRef.current = event.pointerType
    registerPointerType?.(event.pointerType)
    onPointerDown?.(event)
  }, [registerPointerType, onPointerDown])

  const handlePointerEnter = React.useCallback((event: TriggerPointerEvent) => {
    registerPointerType?.(event.pointerType)
    onPointerEnter?.(event)
  }, [registerPointerType, onPointerEnter])

  const handleKeyDown = React.useCallback((event: TriggerKeyEvent) => {
    pressPointerTypeRef.current = "keyboard"
    registerPointerType?.("keyboard")
    onKeyDown?.(event)
  }, [registerPointerType, onKeyDown])

  // A bare trigger renders a button that would submit any enclosing form. With asChild the
  // props land on the caller's own element, often not a button, so only default type here.
  const triggerProps = props.asChild ? props : { type: "button" as const, ...props }

  if (context.isTouchDevice) {
    return (
      <TooltipPrimitive.Trigger
        ref={ref}
        onPointerDown={handlePointerDown}
        onPointerEnter={handlePointerEnter}
        onKeyDown={handleKeyDown}
        onClick={(e) => {
          // A click from a mouse (for example an iPad trackpad) or a key always toggles.
          // A bare trigger renders its own button, which does nothing but show the tooltip.
          const tap = pressPointerTypeRef.current === "touch" || pressPointerTypeRef.current === "pen"
          if (!tap || (openOnTap ?? (!props.asChild || !tapActs(e.target)))) {
            context.setOpen?.(!context.isOpen)
          }
          if (tap && openOnTap === "only") {
            e.stopPropagation()
          }
          onClick?.(e)
        }}
        {...triggerProps}
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
      {...triggerProps}
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
