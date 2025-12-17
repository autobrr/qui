/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { copyTextToClipboard } from "@/lib/utils"
import type { WebSeed } from "@/types"
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table"
import { Copy, Loader2, Search, X } from "lucide-react"
import { memo, useMemo, useState } from "react"
import { toast } from "sonner"

interface WebSeedsTableProps {
  webseeds: WebSeed[] | undefined
  loading: boolean
  incognitoMode: boolean
}

const columnHelper = createColumnHelper<WebSeed>()

export const WebSeedsTable = memo(function WebSeedsTable({
  webseeds,
  loading,
  incognitoMode,
}: WebSeedsTableProps) {
  const [searchQuery, setSearchQuery] = useState("")

  const columns = useMemo(() => [
    columnHelper.accessor("url", {
      header: "URL",
      cell: (info) => {
        const url = info.getValue()
        if (incognitoMode) {
          try {
            const parsed = new URL(url)
            return (
              <span className="font-mono text-xs truncate block">
                {parsed.protocol}//***masked***{parsed.pathname.slice(0, 20)}...
              </span>
            )
          } catch {
            return <span className="font-mono text-xs">***masked***</span>
          }
        }
        return (
          <span className="font-mono text-xs break-all">
            {renderTextWithLinks(url)}
          </span>
        )
      },
    }),
  ], [incognitoMode])

  const filteredData = useMemo(() => {
    const data = webseeds || []
    if (!searchQuery) return data
    const query = searchQuery.toLowerCase()
    return data.filter((ws) => ws.url.toLowerCase().includes(query))
  }, [webseeds, searchQuery])

  const table = useReactTable({
    data: filteredData,
    columns,
    getCoreRowModel: getCoreRowModel(),
  })

  const handleCopyUrl = (webseed: WebSeed) => {
    if (incognitoMode) return
    copyTextToClipboard(webseed.url)
    toast.success("URL copied to clipboard")
  }

  if (loading && !webseeds) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    )
  }

  if (!webseeds || webseeds.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No HTTP sources
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b text-xs min-h-9">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search URLs..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="h-6 w-40 pl-7 pr-7 text-xs"
          />
          {searchQuery && (
            <button
              onClick={() => setSearchQuery("")}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <span className="ml-auto text-muted-foreground">
          {searchQuery
            ? `${filteredData.length} of ${webseeds.length}`
            : `${webseeds.length} HTTP source${webseeds.length !== 1 ? "s" : ""}`}
        </span>
      </div>

      <ScrollArea className="flex-1 min-h-0">
        <div className="min-w-[300px]">
          <table className="w-full text-xs">
            <thead className="sticky top-0 z-10 bg-background border-b">
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id}>
                  {headerGroup.headers.map((header) => (
                    <th
                      key={header.id}
                      className="px-2 py-2 text-left font-medium text-muted-foreground select-none"
                    >
                      {flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.map((row) => (
                <ContextMenu key={row.id}>
                  <ContextMenuTrigger asChild>
                    <tr className="border-b border-border/50 hover:bg-muted/30 cursor-default">
                      {row.getVisibleCells().map((cell) => (
                        <td key={cell.id} className="px-2 py-1.5">
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      ))}
                    </tr>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem
                      onClick={() => handleCopyUrl(row.original)}
                      disabled={incognitoMode}
                    >
                      <Copy className="h-3.5 w-3.5 mr-2" />
                      Copy URL
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ))}
            </tbody>
          </table>
        </div>
      </ScrollArea>
    </div>
  )
})
