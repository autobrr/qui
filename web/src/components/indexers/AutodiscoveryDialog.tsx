/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Checkbox } from "@/components/ui/checkbox"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import type { JackettIndexer, TorznabIndexer, TorznabIndexerFormData, TorznabIndexerUpdate } from "@/types"
import { api } from "@/lib/api"

interface AutodiscoveryDialogProps {
  open: boolean
  onClose: () => void
}

export function AutodiscoveryDialog({ open, onClose }: AutodiscoveryDialogProps) {
  const [step, setStep] = useState<"input" | "select">("input")
  const [loading, setLoading] = useState(false)
  const [baseUrl, setBaseUrl] = useState("http://localhost:9696")
  const [baseUrlError, setBaseUrlError] = useState<string | null>(null)
  const [apiKey, setApiKey] = useState("")
  const [showBasicAuth, setShowBasicAuth] = useState(false)
  const [basicUsername, setBasicUsername] = useState("")
  const [basicPassword, setBasicPassword] = useState("")
  const [discoveredIndexers, setDiscoveredIndexers] = useState<JackettIndexer[]>([])
  const [selectedIndexers, setSelectedIndexers] = useState<Set<string>>(new Set())
  const [existingIndexersMap, setExistingIndexersMap] = useState<Map<string, TorznabIndexer>>(new Map())

  const normalizeBaseUrl = (value: string) => {
    const trimmed = value.trim()
    const withoutTrailingSlashes = trimmed.replace(/\/+$/, "")
    return withoutTrailingSlashes || trimmed
  }

  const handleDiscover = async (e: React.FormEvent) => {
    e.preventDefault()
    const normalizedBaseUrl = normalizeBaseUrl(baseUrl)
    if (!normalizedBaseUrl) {
      setBaseUrlError("Indexer URL is required")
      toast.error("Indexer URL is required")
      return
    }

    const trimmedBasicUser = basicUsername.trim()
    const trimmedBasicPass = basicPassword
    if (showBasicAuth && (!trimmedBasicUser || !trimmedBasicPass)) {
      toast.error("Basic auth requires username and password")
      return
    }

    setBaseUrlError(null)
    setLoading(true)

    try {
      const [response, existing] = await Promise.all([
        api.discoverJackettIndexers(
          normalizedBaseUrl,
          apiKey,
          showBasicAuth ? trimmedBasicUser : undefined,
          showBasicAuth ? trimmedBasicPass : undefined,
        ),
        api.listTorznabIndexers(),
      ])

      setDiscoveredIndexers(response.indexers)

      // Build map of existing indexers by name with full indexer data
      const existingMap = new Map<string, TorznabIndexer>()
      for (const idx of existing) {
        existingMap.set(idx.name, idx)
      }
      setExistingIndexersMap(existingMap)

      setStep("select")
      const existingCount = response.indexers.filter(idx => existingMap.has(idx.name)).length
      if (existingCount > 0) {
        toast.success(`Found ${response.indexers.length} indexers (${existingCount} already exist)`)
      } else {
        toast.success(`Found ${response.indexers.length} indexers`)
      }

      // Show discovery warnings if any
      if (response.warnings?.length) {
        for (const warning of response.warnings) {
          toast.warning(warning)
        }
      }
    } catch (error) {
      console.error("Failed to discover indexers:", error)
      const errorMessage = error instanceof Error ? error.message : "Unknown error"
      toast.error(`Failed to discover indexers: ${errorMessage}`)
    } finally {
      setLoading(false)
    }
  }

  const toggleIndexer = (id: string) => {
    const newSelected = new Set(selectedIndexers)
    if (newSelected.has(id)) {
      newSelected.delete(id)
    } else {
      newSelected.add(id)
    }
    setSelectedIndexers(newSelected)
  }

  const handleImport = async () => {
    const normalizedBaseUrl = normalizeBaseUrl(baseUrl)
    if (!normalizedBaseUrl) {
      setBaseUrlError("Indexer URL is required")
      toast.error("Provide an indexer URL before importing")
      setStep("input")
      return
    }

    const trimmedBasicUser = basicUsername.trim()
    const trimmedBasicPass = basicPassword
    if (showBasicAuth && (!trimmedBasicUser || !trimmedBasicPass)) {
      toast.error("Basic auth requires username and password")
      return
    }

    setBaseUrlError(null)
    setLoading(true)
    let createdCount = 0
    let updatedCount = 0
    let errorCount = 0
    const errors: string[] = []
    const warningDetails: string[] = []

    for (const indexer of discoveredIndexers) {
      if (!selectedIndexers.has(indexer.id)) continue

      const backend = indexer.backend ?? "jackett"
      const indexerId = indexer.id?.trim() ?? ""
      const normalizedIndexerId = indexerId !== "" ? indexerId : undefined

      try {
        const existing = existingIndexersMap.get(indexer.name)
        if (existing) {
          // Update existing indexer - keep base URL, API key, and backend aligned
          // Omit enabled to preserve the user's current enabled state
          const updateData: TorznabIndexerUpdate = {
            base_url: normalizedBaseUrl,
            api_key: apiKey,
            backend,
            indexer_id: normalizedIndexerId,
            capabilities: indexer.caps, // Include capabilities if discovered
            categories: indexer.categories, // Include categories if discovered
          }
          if (showBasicAuth) {
            updateData.basic_username = trimmedBasicUser
            updateData.basic_password = trimmedBasicPass
          }
          const response = await api.updateTorznabIndexer(existing.id, updateData)
          updatedCount++
          if (response.warnings?.length) {
            warningDetails.push(`${indexer.name}: ${response.warnings.join(", ")}`)
          }
        } else {
          // Create new indexer - enable by default for newly discovered indexers
          const createData: TorznabIndexerFormData = {
            name: indexer.name,
            base_url: normalizedBaseUrl,
            api_key: apiKey,
            backend,
            enabled: true,
            indexer_id: normalizedIndexerId,
            capabilities: indexer.caps, // Include capabilities if discovered
            categories: indexer.categories, // Include categories if discovered
          }
          if (showBasicAuth) {
            createData.basic_username = trimmedBasicUser
            createData.basic_password = trimmedBasicPass
          }
          const response = await api.createTorznabIndexer(createData)
          createdCount++
          if (response.warnings?.length) {
            warningDetails.push(`${indexer.name}: ${response.warnings.join(", ")}`)
          }
        }
      } catch (error) {
        errorCount++
        const errorMessage = error instanceof Error ? error.message : String(error)
        errors.push(`${indexer.name}: ${errorMessage}`)
        console.error(`Failed to import ${indexer.name}:`, error)
      }
    }

    setLoading(false)

    if (errorCount === 0) {
      const messages = []
      if (createdCount > 0) messages.push(`${createdCount} created`)
      if (updatedCount > 0) messages.push(`${updatedCount} updated`)
      if (warningDetails.length > 0) {
        toast.warning(`${messages.join(", ")} (${warningDetails.length} with warnings)`)
        // Show first few warning details
        for (const detail of warningDetails.slice(0, 3)) {
          toast.warning(detail)
        }
        if (warningDetails.length > 3) {
          toast.warning(`...and ${warningDetails.length - 3} more warnings`)
        }
      } else {
        toast.success(`Success: ${messages.join(", ")}`)
      }
    } else {
      const messages = []
      if (createdCount > 0) messages.push(`${createdCount} created`)
      if (updatedCount > 0) messages.push(`${updatedCount} updated`)
      if (errorCount > 0) messages.push(`${errorCount} failed`)
      toast.error(messages.join(", "))
      // Show first few error details
      for (const detail of errors.slice(0, 3)) {
        toast.error(detail)
      }
      if (errors.length > 3) {
        toast.error(`...and ${errors.length - 3} more errors`)
      }
    }

    handleClose()
  }

  const handleSelectAll = () => {
    const allIds = new Set(discoveredIndexers.map(idx => idx.id))
    setSelectedIndexers(allIds)
  }

  const handleDeselectAll = () => {
    setSelectedIndexers(new Set())
  }

  const handleClose = () => {
    setStep("input")
    setBaseUrl("http://localhost:9696")
    setBaseUrlError(null)
    setApiKey("")
    setShowBasicAuth(false)
    setBasicUsername("")
    setBasicPassword("")
    setDiscoveredIndexers([])
    setSelectedIndexers(new Set())
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={(open) => { if (!open) handleClose(); }}>
      <DialogContent className="sm:max-w-[525px] max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>Discover Indexers</DialogTitle>
          <DialogDescription>
            {step === "input"? "Connect to Jackett or Prowlarr to discover configured indexers": "Select indexers to import"}
          </DialogDescription>
        </DialogHeader>

        {step === "input" ? (
          <form onSubmit={handleDiscover} autoComplete="off" data-1p-ignore className="flex-1 flex flex-col min-h-0">
            <div className="grid gap-4 py-4 flex-1 overflow-y-auto">
              <div className="grid gap-2">
                <Label htmlFor="torznabUrl">Indexer URL</Label>
                <Input
                  id="torznabUrl"
                  type="url"
                  value={baseUrl}
                  onChange={(e) => {
                    setBaseUrl(e.target.value)
                    if (baseUrlError) {
                      setBaseUrlError(null)
                    }
                  }}
                  placeholder="http://localhost:9696"
                  className={baseUrlError ? "border-destructive focus-visible:ring-destructive" : undefined}
                  aria-invalid={baseUrlError ? "true" : "false"}
                  autoComplete="off"
                  data-1p-ignore
                  required
                />
                {baseUrlError && (
                  <p className="text-xs text-destructive">
                    {baseUrlError}
                  </p>
                )}
                <p className="text-xs text-muted-foreground">
                  Prowlarr defaults to http://localhost:9696, Jackett to http://localhost:9117.
                </p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="torznabApiKey">API Key</Label>
                <Input
                  id="torznabApiKey"
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder="Your indexer API key"
                  autoComplete="off"
                  data-1p-ignore
                  required
                />
              </div>
              <div className="flex items-center justify-between gap-4 rounded-lg border bg-muted/40 p-4">
                <div className="space-y-1">
                  <Label htmlFor="torznab-basic-auth">Basic Auth</Label>
                  <p className="text-sm text-muted-foreground max-w-prose">
                    Use HTTP basic authentication for Jackett/Prowlarr behind a reverse proxy.
                  </p>
                </div>
                <Switch
                  id="torznab-basic-auth"
                  checked={showBasicAuth}
                  onCheckedChange={(checked) => {
                    setShowBasicAuth(checked)
                    if (!checked) {
                      setBasicUsername("")
                      setBasicPassword("")
                    }
                  }}
                />
              </div>
              {showBasicAuth && (
                <div className="grid gap-4 rounded-lg border bg-muted/20 p-4">
                  <div className="grid gap-2">
                    <Label htmlFor="torznab-basic-username">Basic Username</Label>
                    <Input
                      id="torznab-basic-username"
                      value={basicUsername}
                      onChange={(e) => setBasicUsername(e.target.value)}
                      placeholder="Username"
                      autoComplete="off"
                      data-1p-ignore
                      required
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="torznab-basic-password">Basic Password</Label>
                    <Input
                      id="torznab-basic-password"
                      type="password"
                      value={basicPassword}
                      onChange={(e) => setBasicPassword(e.target.value)}
                      placeholder="Password"
                      autoComplete="off"
                      data-1p-ignore
                      required
                    />
                  </div>
                </div>
              )}
            </div>
            <DialogFooter className="flex-shrink-0">
              <Button type="button" variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={loading}>
                {loading ? "Discovering..." : "Discover"}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <div className="flex-1 flex flex-col min-h-0">
            {discoveredIndexers.length > 0 && (
              <div className="flex gap-2 pb-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleSelectAll}
                >
                  Select All
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleDeselectAll}
                >
                  Deselect All
                </Button>
                <span className="text-sm text-muted-foreground ml-auto self-center">
                  {selectedIndexers.size} of {discoveredIndexers.length} selected
                </span>
              </div>
            )}
            <ScrollArea className="h-[400px] pr-4">
              <div className="space-y-2">
                {discoveredIndexers.length === 0 ? (
                  <p className="text-center text-muted-foreground py-8">
                    No indexers found
                  </p>
                ) : (
                  discoveredIndexers.map((indexer) => (
                    <div
                      key={indexer.id}
                      className="flex items-start space-x-3 rounded-lg border p-3 hover:bg-accent"
                    >
                      <Checkbox
                        id={indexer.id}
                        checked={selectedIndexers.has(indexer.id)}
                        onCheckedChange={() => toggleIndexer(indexer.id)}
                      />
                      <div className="flex-1">
                        <div className="flex items-center gap-2">
                          <label
                            htmlFor={indexer.id}
                            className="text-sm font-medium leading-none cursor-pointer"
                          >
                            {indexer.name}
                          </label>
                          {existingIndexersMap.has(indexer.name) && (
                            <span className="text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 px-2 py-0.5 rounded">
                              Will Update
                            </span>
                          )}
                        </div>
                        {indexer.description && (
                          <p className="text-sm text-muted-foreground mt-1">
                            {indexer.description}
                          </p>
                        )}
                        <p className="text-xs text-muted-foreground mt-1">
                          Type: {indexer.type}
                          {indexer.backend && ` • Backend: ${indexer.backend}`}
                          {!indexer.configured && " (Not configured)"}
                        </p>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </ScrollArea>
            <DialogFooter className="flex-shrink-0">
              <Button
                type="button"
                variant="outline"
                onClick={() => setStep("input")}
              >
                Back
              </Button>
              <Button
                onClick={handleImport}
                disabled={loading || selectedIndexers.size === 0}
              >
                {loading? "Importing...": `Import ${selectedIndexers.size} indexer${selectedIndexers.size !== 1 ? "s" : ""}`}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
