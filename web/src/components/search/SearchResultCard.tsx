/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useRef } from "react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import type { InstanceResponse, TorznabSearchResult } from "@/types"
import { Download, ExternalLink, MoreVertical, Plus } from "lucide-react"

type SearchResultCardProps = {
  result: TorznabSearchResult
  isSelected: boolean
  onSelect: () => void
  onAddTorrent: (overrideInstanceId?: number) => void
  onDownload: () => void
  onViewDetails: () => void
  categoryName: string
  formatSize: (bytes: number) => string
  formatDate: (date: string) => string
  instances?: InstanceResponse[]
  hasInstances: boolean
  targetInstanceName?: string
}

export function SearchResultCard({
  result,
  isSelected,
  onSelect,
  onAddTorrent,
  onDownload,
  onViewDetails,
  categoryName,
  formatSize,
  formatDate,
  instances,
  hasInstances,
  targetInstanceName,
}: SearchResultCardProps) {
  const { t } = useTranslation("search")
  const primaryAddLabel = targetInstanceName ? t("card.addToNamed", { name: targetInstanceName }) : t("card.addToInstance")
  const menuCooldownRef = useRef(false)
  const menuCooldownTimerRef = useRef<number | null>(null)

  const handleMenuOpenChange = useCallback((open: boolean) => {
    if (menuCooldownTimerRef.current !== null) {
      clearTimeout(menuCooldownTimerRef.current)
      menuCooldownTimerRef.current = null
    }
    if (open) {
      menuCooldownRef.current = true
    } else {
      // Brief cooldown so the phantom tap after menu close doesn't reach the card
      menuCooldownTimerRef.current = window.setTimeout(() => {
        menuCooldownRef.current = false
        menuCooldownTimerRef.current = null
      }, 300)
    }
  }, [])

  const handleCardClick = useCallback(() => {
    if (!menuCooldownRef.current) {
      onSelect()
    }
  }, [onSelect])

  return (
    <Card
      className={cn(
        "p-3 transition-colors cursor-pointer",
        isSelected? "bg-accent text-accent-foreground ring-2 ring-inset ring-accent": "hover:bg-muted/60"
      )}
      role="button"
      tabIndex={0}
      aria-selected={isSelected}
      onClick={handleCardClick}
      onKeyDown={(event) => {
        if (event.currentTarget !== event.target) {
          return
        }

        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault()
          onSelect()
        }
      }}
    >
      <div className="space-y-2">
        {/* Title */}
        <div className="flex items-start justify-between gap-2 h-10">
          <h3 className="text-sm font-medium leading-tight line-clamp-2">
            {result.title}
          </h3>
          <DropdownMenu onOpenChange={handleMenuOpenChange}>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                onClick={(e) => e.stopPropagation()}
              >
                <MoreVertical className="h-4 w-4" />
                <span className="sr-only">{t("card.actions")}</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" onCloseAutoFocus={(e) => e.preventDefault()}>
              <DropdownMenuItem onSelect={() => onAddTorrent()} disabled={!hasInstances}>
                <Plus className="mr-2 h-4 w-4" /> {primaryAddLabel}
              </DropdownMenuItem>
              {hasInstances && instances && instances.length > 1 && (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    {t("card.quickAddTo")}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {instances.map(instance => (
                      <DropdownMenuItem
                        key={instance.id}
                        onSelect={() => onAddTorrent(instance.id)}
                      >
                        {instance.connected ? instance.name : t("card.addToNamedOffline", { name: instance.name })}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  onDownload()
                }}
                disabled={!result.downloadUrl}
              >
                <Download className="mr-2 h-4 w-4" /> {t("card.download")}
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  onViewDetails()
                }}
                disabled={!result.infoUrl}
              >
                <ExternalLink className="mr-2 h-4 w-4" /> {t("card.viewDetails")}
              </DropdownMenuItem>
              {isSelected && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onSelect={(event) => {
                      event.preventDefault()
                      onSelect()
                    }}
                  >
                    {t("card.clearSelection")}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {/* Key Info Row */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{result.indexer}</span>
          <span>{formatSize(result.size)}</span>
          <Badge variant={result.seeders > 0 ? "default" : "secondary"} className="text-xs">
            {t("card.seeders", { count: result.seeders })}
          </Badge>
        </div>

        {/* Category and Metadata */}
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="outline" className="text-xs">
            {categoryName}
          </Badge>
          {result.source && (
            <Badge variant="outline" className="text-xs">
              {result.source}
            </Badge>
          )}
          {result.group && (
            <Badge variant="outline" className="text-xs">
              {result.group}
            </Badge>
          )}
          {result.downloadVolumeFactor === 0 && (
            <Badge variant="default" className="text-xs">{t("results.free")}</Badge>
          )}
          {result.downloadVolumeFactor > 0 && result.downloadVolumeFactor < 1 && (
            <Badge variant="secondary" className="text-xs">{result.downloadVolumeFactor * 100}%</Badge>
          )}
        </div>

        {/* Published Date */}
        <div className="text-xs text-muted-foreground">
          {t("card.published", { date: formatDate(result.publishDate) })}
        </div>
      </div>
    </Card>
  )
}
