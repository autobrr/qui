/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import ReactMarkdown from "react-markdown"

interface ChangelogData {
  version: string
  body: string
}

/**
 * Hook to handle server reconnection after self-update with changelog display.
 *
 * Polls the server after a restart to detect when it comes back online,
 * then shows the changelog and reloads the page to update the service worker cache.
 */
export function useServerReconnect() {
  const [showChangelog, setShowChangelog] = useState(false)
  const [changelogData, setChangelogData] = useState<ChangelogData | null>(null)
  const pollingRef = useRef<NodeJS.Timeout | null>(null)
  const changelogRef = useRef<ChangelogData | null>(null)

  // Poll for server reconnection after update
  const pollForReconnection = useCallback(() => {
    let attempts = 0
    const maxAttempts = 60 // Poll for up to 60 seconds

    pollingRef.current = setInterval(async () => {
      attempts++

      try {
        // Try to fetch version info - if this succeeds, server is back
        const versionInfo = await api.getLatestVersion()

        // Server is back online
        if (pollingRef.current) {
          clearInterval(pollingRef.current)
          pollingRef.current = null
        }

        // Show changelog dialog if we have release notes
        if (versionInfo?.body) {
          const latestRelease = {
            version: versionInfo.tag_name,
            body: versionInfo.body
          }
          setChangelogData(latestRelease)
          changelogRef.current = latestRelease
          setShowChangelog(true)
        } else if (changelogRef.current) {
          setChangelogData(changelogRef.current)
          setShowChangelog(true)
        } else {
          toast.success("Update completed successfully!")
          // Reload to update service worker cache
          setTimeout(() => {
            window.location.reload()
          }, 1000)
        }
      } catch {
        // Server not ready yet
        if (attempts >= maxAttempts) {
          if (pollingRef.current) {
            clearInterval(pollingRef.current)
            pollingRef.current = null
          }
          toast.info("Update completed. Please refresh the page.")
        }
      }
    }, 1000) // Poll every second
  }, [])

  const handleCloseChangelog = useCallback(() => {
    setShowChangelog(false)
    // Reload page to update service worker cache after user reads changelog
    setTimeout(() => {
      window.location.reload()
    }, 200)
  }, [])

  // Clean up changelog by removing commit hashes
  const cleanChangelog = useCallback((markdown: string) => {
    // Remove commit hashes (40 hex chars followed by ": ") from list items
    // Example: "* a7e79d862928c1bf8838b1a30678bdb3844d3315: feat(backups)..." -> "* feat(backups)..."
    return markdown.replace(/^(\s*[*-]\s+)([a-f0-9]{40}):\s+/gm, "$1")
  }, [])

  // Store changelog data for after reconnection
  const storeChangelog = useCallback((version: string, body: string) => {
    changelogRef.current = { version, body }
  }, [])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current)
        pollingRef.current = null
      }
    }
  }, [])

  // Render the changelog dialog
  const ChangelogDialog = useCallback(() => (
    <Dialog open={showChangelog} onOpenChange={(open) => !open && handleCloseChangelog()}>
      <DialogContent className="max-w-[90vw] w-auto max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Update Completed - Version {changelogData?.version}</DialogTitle>
          <DialogDescription>
            qui has been successfully updated. Here's what's new:
          </DialogDescription>
        </DialogHeader>
        <div className="overflow-y-auto flex-1 pr-2">
          <div className="prose dark:prose-invert max-w-none prose-headings:mb-3 prose-headings:mt-6 prose-p:my-2 prose-ul:my-2 prose-li:my-1">
            {changelogData?.body && (
              <ReactMarkdown
                components={{
                  h1: ({ children }) => <h2 className="text-xl font-bold mt-6 mb-3">{children}</h2>,
                  h2: ({ children }) => <h3 className="text-lg font-semibold mt-5 mb-2">{children}</h3>,
                  h3: ({ children }) => <h4 className="text-base font-semibold mt-4 mb-2">{children}</h4>,
                  ul: ({ children }) => <ul className="list-disc pl-5 space-y-1">{children}</ul>,
                  li: ({ children }) => <li className="text-sm leading-relaxed break-words">{children}</li>,
                  p: ({ children }) => <p className="text-sm my-2 break-words">{children}</p>,
                  code: ({ children }) => <code className="text-xs bg-muted px-1.5 py-0.5 rounded break-all">{children}</code>,
                  a: ({ href, children }) => (
                    <a href={href} target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">
                      {children}
                    </a>
                  ),
                }}
              >
                {cleanChangelog(changelogData.body)}
              </ReactMarkdown>
            )}
          </div>
        </div>
        <div className="flex justify-end pt-4 border-t">
          <Button onClick={handleCloseChangelog}>
            Reload
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  ), [showChangelog, changelogData, handleCloseChangelog, cleanChangelog])

  return {
    pollForReconnection,
    storeChangelog,
    ChangelogDialog,
  }
}
