/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

"use client"

import * as AccordionPrimitive from "@radix-ui/react-accordion"
import { FileIcon, FolderIcon, FolderOpenIcon } from "lucide-react"
import {
  createContext,
  forwardRef,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentPropsWithoutRef,
  type Dispatch,
  type ReactNode,
  type SetStateAction
} from "react"

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"

type Direction = "rtl" | "ltr"

export type TreeViewElement = {
  id: string
  name: string
  kind: "file" | "folder"
  isSelectable?: boolean
  children?: TreeViewElement[]
}

interface TreeContextProps {
  selectedId?: string
  expandedItems: string[]
  indicator: boolean
  handleExpand: (id: string) => void
  selectItem: (id: string) => void
  setExpandedItems: Dispatch<SetStateAction<string[]>>
  openIcon?: ReactNode
  closeIcon?: ReactNode
  direction: Direction
}

const TreeContext = createContext<TreeContextProps | null>(null)

const useTree = (): TreeContextProps => {
  const context = useContext(TreeContext)
  if (!context) {
    throw new Error("useTree must be used within a TreeProvider")
  }
  return context
}

type AccordionRootProps = Omit<
  ComponentPropsWithoutRef<typeof AccordionPrimitive.Root>,
  "type" | "value" | "defaultValue" | "onValueChange" | "dir" | "className"
>

export interface TreeProps extends AccordionRootProps {
  className?: string
  initialSelectedId?: string
  indicator?: boolean
  elements?: TreeViewElement[]
  initialExpandedItems?: string[]
  openIcon?: ReactNode
  closeIcon?: ReactNode
  dir?: Direction
  onSelectionChange?: (id: string) => void
}

const Tree = forwardRef<HTMLDivElement, TreeProps>(
  (
    {
      className,
      elements,
      initialSelectedId,
      initialExpandedItems,
      children,
      indicator = true,
      openIcon,
      closeIcon,
      dir,
      onSelectionChange,
      ...accordionProps
    },
    ref
  ) => {
    const [selectedId, setSelectedId] = useState<string | undefined>(initialSelectedId)
    const [expandedItems, setExpandedItems] = useState<string[]>(initialExpandedItems ?? [])

    const selectItem = useCallback(
      (id: string) => {
        setSelectedId(id)
        if (onSelectionChange) {
          onSelectionChange(id)
        }
      },
      [onSelectionChange]
    )

    const handleExpand = useCallback((id: string) => {
      setExpandedItems(prev => {
        if (prev.includes(id)) {
          return prev.filter(item => item !== id)
        }
        return [...prev, id]
      })
    }, [])

    const direction: Direction = dir === "rtl" ? "rtl" : "ltr"

    useEffect(() => {
      if (initialSelectedId !== undefined) {
        setSelectedId(initialSelectedId)
      }
    }, [initialSelectedId])

    useEffect(() => {
      if (!initialSelectedId) {
        return
      }
      if (!initialExpandedItems && elements) {
        const pathSegments = initialSelectedId.split("/")
        const cumulativeIds: string[] = []
        for (let i = 0; i < pathSegments.length - 1; i++) {
          const parentId =
            cumulativeIds.length === 0? pathSegments[i]: `${cumulativeIds[i - 1]}/${pathSegments[i]}`
          cumulativeIds.push(parentId)
        }
        setExpandedItems(prev => Array.from(new Set([...prev, ...cumulativeIds])))
      }
    }, [elements, initialExpandedItems, initialSelectedId])

    const renderedChildren = useMemo(() => {
      if (!elements || elements.length === 0) {
        return children
      }

      const renderNodes = (nodes: TreeViewElement[]): ReactNode =>
        nodes.map(node => {
          if (node.kind === "folder") {
            return (
              <Folder key={node.id} value={node.id} element={node.name} isSelectable={node.isSelectable}>
                {node.children ? renderNodes(node.children) : null}
              </Folder>
            )
          }

          return (
            <File key={node.id} value={node.id} isSelectable={node.isSelectable}>
              {node.name}
            </File>
          )
        })

      return renderNodes(elements)
    }, [children, elements])

    return (
      <TreeContext.Provider
        value={{
          selectedId,
          expandedItems,
          handleExpand,
          selectItem,
          setExpandedItems,
          indicator,
          openIcon,
          closeIcon,
          direction,
        }}
      >
        <div className={cn("size-full", className)}>
          <ScrollArea ref={ref} className="relative h-full px-2" dir={direction}>
            <AccordionPrimitive.Root
              {...accordionProps}
              type="multiple"
              value={expandedItems}
              className="flex flex-col gap-1"
              onValueChange={value => setExpandedItems(value)}
              dir={direction}
            >
              {renderedChildren}
            </AccordionPrimitive.Root>
          </ScrollArea>
        </div>
      </TreeContext.Provider>
    )
  }
)

Tree.displayName = "Tree"

const TreeIndicator = forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => {
    const { direction } = useTree()

    return (
      <div
        dir={direction}
        ref={ref}
        className={cn(
          "bg-muted absolute left-1.5 h-full w-px rounded-md py-3 duration-300 ease-in-out hover:bg-slate-300 rtl:right-1.5",
          className
        )}
        {...props}
      />
    )
  }
)

TreeIndicator.displayName = "TreeIndicator"

type AccordionItemProps = ComponentPropsWithoutRef<typeof AccordionPrimitive.Item>

interface FolderProps extends Omit<AccordionItemProps, "value"> {
  element: string
  value?: string
  isSelectable?: boolean
  isSelect?: boolean
}

const Folder = forwardRef<
  HTMLDivElement,
  FolderProps
>(
  (
    {
      className,
      element,
      value = element,
      isSelectable = true,
      isSelect,
      children,
      ...props
    },
    ref
  ) => {
    const {
      direction,
      handleExpand,
      expandedItems,
      indicator,
      setExpandedItems,
      openIcon,
      closeIcon,
      selectItem,
      selectedId,
    } = useTree()

    const isExpanded = expandedItems.includes(value)
    const isSelected = isSelect ?? selectedId === value

    const handleTriggerClick = useCallback(() => {
      handleExpand(value)
      if (isSelectable) {
        selectItem(value)
      }
    }, [handleExpand, isSelectable, selectItem, value])

    return (
      <AccordionPrimitive.Item
        ref={ref}
        {...props}
        value={value}
        className="relative h-full overflow-hidden"
      >
        <AccordionPrimitive.Trigger
          className={cn(
            "flex items-center gap-1 rounded-md text-sm",
            className,
            isSelected && isSelectable ? "bg-muted" : null,
            "cursor-pointer"
          )}
          onClick={handleTriggerClick}
        >
          {isExpanded ? (
            openIcon ?? <FolderOpenIcon className="size-4" />
          ) : (
            closeIcon ?? <FolderIcon className="size-4" />
          )}
          <span>{element}</span>
        </AccordionPrimitive.Trigger>
        <AccordionPrimitive.Content className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down relative h-full overflow-hidden text-sm">
          {indicator && children ? <TreeIndicator aria-hidden="true" /> : null}
          <AccordionPrimitive.Root
            dir={direction}
            type="multiple"
            className="ml-5 flex flex-col gap-1 py-1 rtl:mr-5"
            value={expandedItems}
            onValueChange={nextValue => setExpandedItems(nextValue)}
          >
            {children}
          </AccordionPrimitive.Root>
        </AccordionPrimitive.Content>
      </AccordionPrimitive.Item>
    )
  }
)

Folder.displayName = "Folder"

const File = forwardRef<
  HTMLButtonElement,
  {
    value: string
    handleSelect?: (id: string) => void
    isSelectable?: boolean
    isSelect?: boolean
    fileIcon?: ReactNode
  } & React.ButtonHTMLAttributes<HTMLButtonElement>
>(
  (
    {
      value,
      className,
      handleSelect,
      isSelectable = true,
      isSelect,
      fileIcon,
      children,
      ...props
    },
    ref
  ) => {
    const { direction, selectedId, selectItem } = useTree()
    const isSelected = isSelect ?? selectedId === value

    const handleClick = useCallback(() => {
      if (!isSelectable) {
        return
      }
      selectItem(value)
      if (handleSelect) {
        handleSelect(value)
      }
    }, [handleSelect, isSelectable, selectItem, value])

    return (
      <button
        ref={ref}
        type="button"
        disabled={!isSelectable}
        className={cn(
          "flex w-fit items-center gap-1 rounded-md pr-1 text-sm duration-200 ease-in-out rtl:pr-0 rtl:pl-1",
          {
            "bg-muted": isSelected && isSelectable,
          },
          isSelectable ? "cursor-pointer" : "cursor-not-allowed opacity-50",
          direction === "rtl" ? "rtl" : "ltr",
          className
        )}
        onClick={handleClick}
        {...props}
      >
        {fileIcon ?? <FileIcon className="size-4" />}
        {children}
      </button>
    )
  }
)

File.displayName = "File"

const CollapseButton = forwardRef<
  HTMLButtonElement,
  {
    elements: TreeViewElement[]
    expandAll?: boolean
  } & React.HTMLAttributes<HTMLButtonElement>
>(({ className, elements, expandAll = false, children, ...props }, ref) => {
  const { expandedItems, setExpandedItems } = useTree()

  const expandAllNodes = useCallback(
    (nodes: TreeViewElement[]) => {
      const collectIds = (items: TreeViewElement[], acc: Set<string>) => {
        items.forEach(item => {
          if (item.kind === "folder") {
            acc.add(item.id)
            if (item.children) {
              collectIds(item.children, acc)
            }
          }
        })
      }
      const ids = new Set<string>()
      collectIds(nodes, ids)
      setExpandedItems(Array.from(ids))
    },
    [setExpandedItems]
  )

  const collapseAll = useCallback(() => {
    setExpandedItems([])
  }, [setExpandedItems])

  useEffect(() => {
    if (expandAll) {
      expandAllNodes(elements)
    }
  }, [elements, expandAll, expandAllNodes])

  return (
    <Button
      variant="ghost"
      className={cn("absolute right-2 bottom-1 h-8 w-fit p-1", className)}
      onClick={() => {
        if (expandedItems.length > 0) {
          collapseAll()
        } else {
          expandAllNodes(elements)
        }
      }}
      ref={ref}
      {...props}
    >
      {children}
      <span className="sr-only">Toggle</span>
    </Button>
  )
})

CollapseButton.displayName = "CollapseButton"

export { CollapseButton, File, Folder, Tree }
