/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { JackettIndexer, TorznabIndexer, TorznabIndexerFormData, TorznabIndexerUpdate } from '@/types'
import { api } from '@/lib/api'

interface AutodiscoveryDialogProps {
  open: boolean
  onClose: () => void
}

export function AutodiscoveryDialog({ open, onClose }: AutodiscoveryDialogProps) {
  const [step, setStep] = useState<'input' | 'select'>('input')
  const [loading, setLoading] = useState(false)
  const [baseUrl, setBaseUrl] = useState('http://localhost:9117')
  const [apiKey, setApiKey] = useState('')
  const [discoveredIndexers, setDiscoveredIndexers] = useState<JackettIndexer[]>([])
  const [selectedIndexers, setSelectedIndexers] = useState<Set<string>>(new Set())
  const [existingIndexersMap, setExistingIndexersMap] = useState<Map<string, TorznabIndexer>>(new Map())

  const handleDiscover = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)

    try {
      const [response, existing] = await Promise.all([
        api.discoverJackettIndexers(baseUrl, apiKey),
        api.listTorznabIndexers()
      ])
      
      setDiscoveredIndexers(response)
      
      // Build map of existing indexers by name with full indexer data
      const existingMap = new Map<string, TorznabIndexer>()
      for (const idx of existing) {
        existingMap.set(idx.name, idx)
      }
      setExistingIndexersMap(existingMap)
      
      setStep('select')
      const existingCount = response.filter(idx => existingMap.has(idx.name)).length
      if (existingCount > 0) {
        toast.success(`Found ${response.length} indexers (${existingCount} already exist)`)
      } else {
        toast.success(`Found ${response.length} indexers`)
      }
    } catch (error) {
      toast.error('Failed to discover indexers. Check your URL and API key.')
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
    setLoading(true)
    let createdCount = 0
    let updatedCount = 0
    let errorCount = 0
    const errors: string[] = []

    for (const indexer of discoveredIndexers) {
      if (!selectedIndexers.has(indexer.id)) continue

      const torznabUrl = `${baseUrl}/api/v2.0/indexers/${indexer.id}/results/torznab/api`

      try {
        const existing = existingIndexersMap.get(indexer.name)
        if (existing) {
          // Update existing indexer - only update base_url and api_key
          const updateData: TorznabIndexerUpdate = {
            base_url: torznabUrl,
            api_key: apiKey,
          }
          await api.updateTorznabIndexer(existing.id, updateData)
          updatedCount++
        } else {
          // Create new indexer - backend applies defaults
          const createData: TorznabIndexerFormData = {
            name: indexer.name,
            base_url: torznabUrl,
            api_key: apiKey,
          }
          await api.createTorznabIndexer(createData)
          createdCount++
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
      toast.success(`Success: ${messages.join(', ')}`)
    } else {
      const messages = []
      if (createdCount > 0) messages.push(`${createdCount} created`)
      if (updatedCount > 0) messages.push(`${updatedCount} updated`)
      if (errorCount > 0) messages.push(`${errorCount} failed`)
      toast.error(messages.join(', '))
      // Show first error detail
      if (errors.length > 0) {
        toast.error(errors[0])
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
    setStep('input')
    setBaseUrl('http://localhost:9117')
    setApiKey('')
    setDiscoveredIndexers([])
    setSelectedIndexers(new Set())
    onClose()
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-[525px]">
        <DialogHeader>
          <DialogTitle>Discover Jackett Indexers</DialogTitle>
          <DialogDescription>
            {step === 'input'
              ? 'Connect to Jackett to discover configured indexers'
              : 'Select indexers to import'}
          </DialogDescription>
        </DialogHeader>

        {step === 'input' ? (
          <form onSubmit={handleDiscover}>
            <div className="grid gap-4 py-4">
              <div className="grid gap-2">
                <Label htmlFor="jackettUrl">Jackett URL</Label>
                <Input
                  id="jackettUrl"
                  type="url"
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  placeholder="http://localhost:9117"
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="jackettApiKey">API Key</Label>
                <Input
                  id="jackettApiKey"
                  type="password"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  placeholder="Your Jackett API key"
                  required
                />
              </div>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button type="submit" disabled={loading}>
                {loading ? 'Discovering...' : 'Discover'}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <>
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
                          {!indexer.configured && ' (Not configured)'}
                        </p>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </ScrollArea>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setStep('input')}
              >
                Back
              </Button>
              <Button
                onClick={handleImport}
                disabled={loading || selectedIndexers.size === 0}
              >
                {loading
                  ? 'Importing...'
                  : `Import ${selectedIndexers.size} indexer${selectedIndexers.size !== 1 ? 's' : ''}`}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
