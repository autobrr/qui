/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useMutation, useQuery } from "@tanstack/react-query"
import { Download, Loader2, X } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import ReactMarkdown from "react-markdown"
import { toast } from "sonner"

export function UpdateBanner() {
  const [dismissed, setDismissed] = useState(false)
  const [showChangelog, setShowChangelog] = useState(false)
  const [changelogData, setChangelogData] = useState<{
    version: string
    body: string
  } | null>(null)
  const pollingRef = useRef<NodeJS.Timeout | null>(null)

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    // Check for updates every 2 minutes
    refetchInterval: 2 * 60 * 1000,
    // Don't show loading state on mount, run in background
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  // Poll for server reconnection after update
  const pollForReconnection = () => {
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
          setChangelogData({
            version: versionInfo.tag_name,
            body: versionInfo.body
          })
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
  }

  const handleCloseChangelog = () => {
    setShowChangelog(false)
    // Reload page to update service worker cache after user reads changelog
    setTimeout(() => {
      window.location.reload()
    }, 200)
  }

  // Clean up changelog by removing commit hashes
  const cleanChangelog = (markdown: string) => {
    // Remove commit hashes (40 hex chars followed by ": ") from list items
    // Example: "* a7e79d862928c1bf8838b1a30678bdb3844d3315: feat(backups)..." -> "* feat(backups)..."
    return markdown.replace(/^(\s*[*-]\s+)([a-f0-9]{40}):\s+/gm, '$1')
  }

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollingRef.current) {
        clearInterval(pollingRef.current)
        pollingRef.current = null
      }
    }
  }, [])

  const updateMutation = useMutation({
    mutationFn: () => api.triggerSelfUpdate(),
    onSuccess: ({ message }) => {
      toast.success(message)
      setDismissed(true)
      // Start polling for reconnection
      pollForReconnection()
    },
    onError: (error: unknown) => {
      const message = error instanceof Error ? error.message : "Failed to start self-update"
      toast.error(message)
    },
  })

  // Don't show banner if dismissed or no update available
  if (dismissed || !updateInfo) {
    return (
      <>
        {/* Changelog dialog - show even if banner is dismissed */}
        <Dialog open={showChangelog} onOpenChange={(open) => !open && handleCloseChangelog()}>
          <DialogContent className="!max-w-[90vw] w-auto max-h-[85vh] overflow-hidden flex flex-col">
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
      </>
    )
  }

  const handleViewUpdate = () => {
    window.open(updateInfo.html_url, "_blank", "noopener,noreferrer")
  }

  const handleSelfUpdate = () => {
    updateMutation.mutate()
  }

  const handleDismiss = () => {
    setDismissed(true)
  }

  return (
    <>
      <div className={cn(
        "mb-3 rounded-md border border-green-200 bg-green-50 p-3",
        "dark:border-green-800 dark:bg-green-950/50"
      )}>
        <div className="flex items-start gap-2">
          <Download className="h-4 w-4 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-green-800 dark:text-green-200">
              Update Available
            </p>
            <p className="text-xs text-green-700 dark:text-green-300 mt-1">
              Version {updateInfo.tag_name} is now available
            </p>
            <div className="mt-2 flex flex-wrap gap-2">
              {updateInfo.self_update_supported && (
                <Button
                  size="sm"
                  className="h-6 text-xs"
                  onClick={handleSelfUpdate}
                  disabled={updateMutation.isPending}
                >
                  {updateMutation.isPending ? (
                    <span className="flex items-center gap-1">
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      Updating...
                    </span>
                  ) : (
                    "Update & Restart"
                  )}
                </Button>
              )}
              <Button
                size="sm"
                variant="outline"
                className="h-6 text-xs border-green-300 text-green-700 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900"
                onClick={handleViewUpdate}
              >
                View Release
              </Button>
            </div>
          </div>
          <Button
            size="icon"
            variant="ghost"
            className="h-4 w-4 text-green-600 hover:text-green-800 dark:text-green-400 dark:hover:text-green-200"
            onClick={handleDismiss}
          >
            <X className="h-3 w-3" />
            <span className="sr-only">Dismiss</span>
          </Button>
        </div>
      </div>

      {/* Changelog dialog */}
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
              Close
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}