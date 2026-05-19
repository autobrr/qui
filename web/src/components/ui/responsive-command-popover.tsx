/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as React from "react";
import { Command as CommandPrimitive } from "cmdk";
import { Search } from "lucide-react";
import {
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger
} from "@/components/ui/drawer";
import {
  Popover,
  PopoverContent,
  PopoverTrigger
} from "@/components/ui/popover";
import { useIsMobile } from "@/hooks/useMediaQuery";
import { cn } from "@/lib/utils";

const MobileContext = React.createContext<boolean | undefined>(undefined);

/**
 * Returns whether the responsive command popover is rendering in mobile mode.
 * Must be used within a ResponsiveCommandPopover tree.
 */
export function useResponsiveMobile(): boolean {
  const value = React.useContext(MobileContext);
  if (value === undefined) {
    throw new Error("useResponsiveMobile must be used within a ResponsiveCommandPopover");
  }
  return value;
}

interface ResponsiveCommandPopoverProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: React.ReactNode;
  title?: string;
  popoverWidth?: string;
  popoverAlign?: "start" | "center" | "end";
  children: React.ReactNode;
}

export function ResponsiveCommandPopover({
  open,
  onOpenChange,
  trigger,
  title = "Select",
  popoverWidth = "200px",
  popoverAlign = "start",
  children,
}: ResponsiveCommandPopoverProps) {
  const isMobile = useIsMobile();

  if (isMobile) {
    return (
      <MobileContext.Provider value={true}>
        <Drawer open={open} onOpenChange={onOpenChange} repositionInputs={false}>
          <DrawerTrigger asChild>{trigger}</DrawerTrigger>
          {/* fixed height keeps search input above viewport center so mobile keyboards don't shift the view */}
          <DrawerContent className="h-[85vh]">
            <DrawerHeader className="py-3 border-b">
              <DrawerTitle className="text-lg font-semibold">{title}</DrawerTitle>
            </DrawerHeader>
            <div className="flex-1 overflow-hidden flex flex-col">{children}</div>
          </DrawerContent>
        </Drawer>
      </MobileContext.Provider>
    );
  }

  return (
    <MobileContext.Provider value={false}>
      <Popover open={open} onOpenChange={onOpenChange}>
        <PopoverTrigger asChild>{trigger}</PopoverTrigger>
        <PopoverContent
          className="p-0"
          style={{ width: popoverWidth }}
          align={popoverAlign}
        >
          {children}
        </PopoverContent>
      </Popover>
    </MobileContext.Provider>
  );
}

const ResponsiveCommand = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive>
>(({ className, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  return (
    <CommandPrimitive
      ref={ref}
      className={cn(
        "flex h-full w-full flex-col overflow-hidden rounded-md bg-popover text-popover-foreground",
        isMobile && "bg-transparent rounded-none flex-1",
        className
      )}
      {...props}
    />
  );
});
ResponsiveCommand.displayName = "ResponsiveCommand";

const ResponsiveCommandInput = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive.Input>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.Input>
>(({ className, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  return (
    <div
      className={cn(
        "flex items-center border-b px-3",
        isMobile && "px-4 bg-muted/20 sticky top-0 z-10"
      )}
      cmdk-input-wrapper=""
    >
      <Search className={cn(
        "mr-3 shrink-0 text-muted-foreground",
        isMobile ? "h-5 w-5" : "h-4 w-4"
      )} />
      <CommandPrimitive.Input
        ref={ref}
        className={cn(
          "flex w-full rounded-md bg-transparent outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
          isMobile ? "h-14 py-4 text-base" : "h-11 py-3 text-sm",
          className
        )}
        {...props}
      />
    </div>
  );
});
ResponsiveCommandInput.displayName = "ResponsiveCommandInput";

const ResponsiveCommandList = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.List>
>(({ className, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  return (
    <CommandPrimitive.List
      ref={ref}
      className={cn(
        "overflow-y-auto overflow-x-hidden",
        isMobile ? "flex-1 pb-[env(safe-area-inset-bottom)]" : "max-h-[300px] pointer-events-auto",
        className
      )}
      onWheel={(e) => {
        // Prevent scroll event propagation to parent elements to ensure the list scrolls
        e.stopPropagation();
      }}
      {...props}
    />
  );
});
ResponsiveCommandList.displayName = "ResponsiveCommandList";

const ResponsiveCommandEmpty = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive.Empty>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.Empty>
>(({ className, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  return (
    <CommandPrimitive.Empty
      ref={ref}
      className={cn(
        "text-center text-muted-foreground",
        isMobile ? "py-12 text-base" : "py-6 text-sm",
        className
      )}
      {...props}
    />
  );
});
ResponsiveCommandEmpty.displayName = "ResponsiveCommandEmpty";

const ResponsiveCommandGroup = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive.Group>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.Group>
>(({ className, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  return (
    <CommandPrimitive.Group
      ref={ref}
      className={cn(
        "overflow-hidden text-foreground",
        isMobile
          ? "py-1 px-2 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pt-4 [&_[cmdk-group-heading]]:pb-2 [&_[cmdk-group-heading]]:text-sm [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:text-foreground [&_[cmdk-group-heading]]:border-b [&_[cmdk-group-heading]]:border-border/50 [&_[cmdk-group-heading]]:mb-2"
          : "p-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground",
        className
      )}
      {...props}
    />
  );
});
ResponsiveCommandGroup.displayName = "ResponsiveCommandGroup";

// For distinguishing taps from scrolls
const TOUCH_SLOP = 10;

const ResponsiveCommandItem = React.forwardRef<
  React.ElementRef<typeof CommandPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof CommandPrimitive.Item> & {
    disableHighlight?: boolean;
  }
>(({ className, disableHighlight, onSelect, ...props }, ref) => {
  const isMobile = useResponsiveMobile();
  const lastPointerType = React.useRef<string>("");
  const touchStart = React.useRef<{ x: number; y: number } | null>(null);

  const handlePointerDown = React.useCallback((e: React.PointerEvent) => {
    lastPointerType.current = e.pointerType;
    if (e.pointerType === "touch") {
      touchStart.current = { x: e.clientX, y: e.clientY };
    } else {
      touchStart.current = null;
    }
  }, []);

  const onSelectRef = React.useRef(onSelect);
  const valueRef = React.useRef(props.value);
  React.useEffect(() => {
    onSelectRef.current = onSelect;
    valueRef.current = props.value;
  });

  // On mobile, only fire onSelect if the finger didn't move beyond touch slop
  const handlePointerUp = React.useCallback(
    (e: React.PointerEvent) => {
      if (isMobile && onSelectRef.current && e.pointerType === "touch" && touchStart.current) {
        const dx = Math.abs(e.clientX - touchStart.current.x);
        const dy = Math.abs(e.clientY - touchStart.current.y);

        if (dx < TOUCH_SLOP && dy < TOUCH_SLOP) {
          e.preventDefault();
          onSelectRef.current(valueRef.current ?? "");
        }
      }
      touchStart.current = null;
    },
    [isMobile]
  );

  const handleSelect = React.useCallback(
    (value: string) => {
      if (!isMobile || lastPointerType.current !== "touch") {
        onSelect?.(value);
      }
    },
    [isMobile, onSelect]
  );

  return (
    <CommandPrimitive.Item
      ref={ref}
      className={cn(
        "relative flex cursor-default select-none items-center outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50",
        isMobile
          ? "min-h-[48px] px-4 py-2.5 text-[15px] rounded-lg"
          : "px-2 py-1.5 text-sm rounded-sm",
        disableHighlight
          ? "data-[selected='true']:bg-transparent"
          : "data-[selected='true']:bg-accent data-[selected='true']:text-accent-foreground",
        className
      )}
      onSelect={handleSelect}
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
      onPointerCancel={() => { touchStart.current = null; lastPointerType.current = ""; }}
      {...props}
    />
  );
});
ResponsiveCommandItem.displayName = "ResponsiveCommandItem";

export {
  ResponsiveCommand,
  ResponsiveCommandInput,
  ResponsiveCommandList,
  ResponsiveCommandEmpty,
  ResponsiveCommandGroup,
  ResponsiveCommandItem,
};
