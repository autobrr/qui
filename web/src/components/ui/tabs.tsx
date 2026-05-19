/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import * as TabsPrimitive from "@radix-ui/react-tabs"
import { motion } from "motion/react"
import * as React from "react"

import { cn } from "@/lib/utils"

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col", className)}
      {...props}
    />
  )
}

function TabsList({
  className,
  children,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>) {
  const listRef = React.useRef<HTMLDivElement>(null)
  const [indicatorStyle, setIndicatorStyle] = React.useState({ x: 0, width: 0 })
  const [isReady, setIsReady] = React.useState(false)

  const updateIndicator = React.useCallback(() => {
    const list = listRef.current
    if (!list) return

    const activeTab = list.querySelector<HTMLElement>('[data-state="active"]')
    if (!activeTab) return

    setIndicatorStyle({
      x: activeTab.offsetLeft,
      width: activeTab.offsetWidth,
    })
    setIsReady(true)
  }, [])

  React.useEffect(() => {
    updateIndicator()

    const list = listRef.current
    if (!list) return

    const observer = new MutationObserver(updateIndicator)
    observer.observe(list, { attributes: true, subtree: true, attributeFilter: ["data-state"] })

    const resizeObserver = new ResizeObserver(updateIndicator)
    resizeObserver.observe(list)

    return () => {
      observer.disconnect()
      resizeObserver.disconnect()
    }
  }, [updateIndicator])

  return (
    <TabsPrimitive.List
      ref={listRef}
      data-slot="tabs-list"
      className={cn(
        "bg-muted text-muted-foreground relative inline-flex h-9 w-fit items-center justify-center rounded-lg p-0.75",
        className
      )}
      {...props}
    >
      <motion.div
        className="bg-background dark:bg-input/30 border border-muted-foreground/30 absolute left-0 top-0.75 bottom-0.75 rounded-md shadow-sm"
        animate={{ x: indicatorStyle.x, width: indicatorStyle.width }}
        transition={{ type: "spring", stiffness: 350, damping: 30 }}
        style={{ opacity: isReady ? 1 : 0 }}
      />
      {children}
    </TabsPrimitive.List>
  )
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "data-[state=active]:text-accent-foreground focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:outline-ring text-muted-foreground hover:text-accent-foreground inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm font-medium whitespace-nowrap transition-colors focus-visible:ring-[3px] focus-visible:outline-1 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 relative z-1",
        className
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn("flex-1 outline-none", className)}
      {...props}
    />
  )
}

export { Tabs, TabsContent, TabsList, TabsTrigger }

