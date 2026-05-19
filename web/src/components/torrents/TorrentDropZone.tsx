/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cn } from "@/lib/utils"
import type { DragEvent, ReactNode } from "react"
import { forwardRef, useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import type { AddTorrentDropPayload } from "./AddTorrentDialog"

const DROP_URL_PATTERNS = [
  /^magnet:\?/i,
  /^https?:\/\/.+/i,
]

function decodeHtmlEntities(value: string): string {
  if (!value) {
    return value
  }
  if (typeof DOMParser === "undefined") {
    return value
  }
  const parser = new DOMParser()
  const doc = parser.parseFromString(value, "text/html")
  return doc.body?.textContent ?? value
}

function filterSupportedTorrentFiles(fileList: FileList | null | undefined): File[] {
  if (!fileList || fileList.length === 0) {
    return []
  }
  return Array.from(fileList).filter((file) => {
    const name = typeof file.name === "string" ? file.name.toLowerCase() : ""
    const mime = typeof file.type === "string" ? file.type.toLowerCase() : ""
    return name.endsWith(".torrent") || mime === "application/x-bittorrent"
  })
}

function extractSupportedUrls(raw: string): string[] {
  if (!raw) {
    return []
  }

  const unique = new Set<string>()

  const pushIfSupported = (value: string) => {
    const trimmed = value.trim()
    if (!trimmed || trimmed.startsWith("#")) {
      return
    }
    for (const pattern of DROP_URL_PATTERNS) {
      if (pattern.test(trimmed)) {
        unique.add(trimmed)
        break
      }
    }
  }

  raw
    .split(/\r?\n/)
    .forEach((line) => {
      pushIfSupported(line)

      const inlineMatches = line.match(/(magnet:\?[^ \t\r\n"]+|https?:\/\/\S+)/gi)
      if (inlineMatches) {
        inlineMatches.forEach(pushIfSupported)
      }
    })

  return Array.from(unique)
}

interface TorrentDropZoneProps extends React.HTMLAttributes<HTMLDivElement> {
  children: ReactNode
  onDropPayload: (payload: AddTorrentDropPayload) => void
  overlayMessage?: string
}

export const TorrentDropZone = forwardRef<HTMLDivElement, TorrentDropZoneProps>(function TorrentDropZone(
  { children, onDropPayload, overlayMessage = "Drop .torrent files or magnet links to add", className, ...rest },
  ref
) {
  const [isDropTargetActive, setIsDropTargetActive] = useState(false)
  const dragDepthRef = useRef(0)

  const resetDropState = useCallback(() => {
    dragDepthRef.current = 0
    setIsDropTargetActive(false)
  }, [])

  const isPotentialDrop = useCallback((event: DragEvent<HTMLDivElement>) => {
    const dataTransfer = event.dataTransfer
    if (!dataTransfer) {
      return false
    }

    if (filterSupportedTorrentFiles(dataTransfer.files).length > 0) {
      return true
    }

    const items = Array.from(dataTransfer.items ?? [])
    for (const item of items) {
      if (item.kind !== "file") {
        continue
      }
      const file = item.getAsFile()
      if (!file) {
        return true
      }
      const name = typeof file.name === "string" ? file.name.toLowerCase() : ""
      const mime = typeof file.type === "string" ? file.type.toLowerCase() : ""
      if (name.endsWith(".torrent") || mime === "application/x-bittorrent") {
        return true
      }
    }

    const types = Array.from(dataTransfer.types ?? [])
    return types.some((type) =>
      type === "Files" ||
      type === "text/uri-list" ||
      type === "text/plain" ||
      type === "text/html"
    )
  }, [])

  const handleDragEnter = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!isPotentialDrop(event)) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    dragDepthRef.current += 1
    setIsDropTargetActive(true)
  }, [isPotentialDrop])

  const handleDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!isPotentialDrop(event)) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = "copy"
    }
  }, [isPotentialDrop])

  const handleDragLeave = useCallback((event: DragEvent<HTMLDivElement>) => {
    if (!isPotentialDrop(event)) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    const nextDepth = Math.max(0, dragDepthRef.current - 1)
    dragDepthRef.current = nextDepth
    if (nextDepth === 0) {
      setIsDropTargetActive(false)
    }
  }, [isPotentialDrop])

  const handleDrop = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.stopPropagation()
    const dataTransfer = event.dataTransfer
    resetDropState()

    if (!dataTransfer) {
      return
    }

    const torrentFiles = filterSupportedTorrentFiles(dataTransfer.files)
    if (torrentFiles.length > 0) {
      onDropPayload({ type: "file", files: torrentFiles })
      return
    }

    const types = Array.from(dataTransfer.types ?? [])
    const textSegments: string[] = []

    if (types.includes("text/uri-list")) {
      const uriList = dataTransfer.getData("text/uri-list")
      if (uriList) {
        textSegments.push(uriList)
      }
    }

    if (types.includes("text/plain")) {
      const plain = dataTransfer.getData("text/plain")
      if (plain) {
        textSegments.push(plain)
      }
    }

    if (types.includes("text/html")) {
      const html = dataTransfer.getData("text/html")
      if (html) {
        const hrefMatches = Array.from(html.matchAll(/href="([^"]+)"/gi)).map((match) => decodeHtmlEntities(match[1]))
        if (hrefMatches.length > 0) {
          textSegments.push(hrefMatches.join("\n"))
        }
      }
    }

    const combinedText = textSegments.join("\n")
    const supportedUrls = extractSupportedUrls(combinedText)
    if (supportedUrls.length > 0) {
      onDropPayload({ type: "url", urls: supportedUrls })
      return
    }

    if (combinedText.trim().length > 0 || types.length > 0) {
      toast.error("Drop a .torrent file or magnet link to add it")
    }
  }, [onDropPayload, resetDropState])

  useEffect(() => {
    return () => {
      resetDropState()
    }
  }, [resetDropState])

  return (
    <div
      {...rest}
      ref={ref}
      className={cn("relative", className)}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {isDropTargetActive && (
        <div className="pointer-events-none absolute inset-0 z-60 flex items-center justify-center rounded-md border border-dashed border-primary bg-background/80 backdrop-blur-md">
          <div className="text-center text-lg font-medium text-muted-foreground">
            {overlayMessage}
          </div>
        </div>
      )}

      {children}
    </div>
  )
})

TorrentDropZone.displayName = "TorrentDropZone"
