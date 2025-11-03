/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from 'react'
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
import { useToast } from '@/hooks/use-toast'
import type { JackettIndexer, TorznabIndexerFormData } from '@/types'
import * as API from '@/lib/api'

interface AutodiscoveryDialogProps {
  open: boolean
  onClose: () => void
}

export function AutodiscoveryDialog({ open, onClose }: AutodiscoveryDialogProps) {
  const { toast } = useToast()
  const [step, setStep] = useState<'input' | 'select'>('input')
  const [loading, setLoading] = useState(false)
  const [baseUrl, setBaseUrl] = useState('http://localhost:9117')
  const [apiKey, setApiKey] = useState('')
  const [discoveredIndexers, setDiscoveredIndexers] = useState<JackettIndexer[]>([])
  const [selectedIndexers, setSelectedIndexers] = useState<Set<string>>(new Set())

  const handleDiscover = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)

    try {
      const response = await API.discoverJackettIndexers(baseUrl, apiKey)
      setDiscoveredIndexers(response.indexers)
      setStep('select')
      toast({
        title: 'Success',
        description: `Found ${response.indexers.length} indexers`,
      })
    } catch (error) {
      toast({
        title: 'Error',
        description: 'Failed to discover indexers. Check your URL and API key.',
        variant: 'destructive',
      })
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
    let successCount = 0
    let errorCount = 0

    for (const indexer of discoveredIndexers) {
      if (!selectedIndexers.has(indexer.id)) continue

      const formData: TorznabIndexerFormData = {
        name: indexer.name,
        baseUrl: `${baseUrl}/api/v2.0/indexers/${indexer.id}/results/torznab`,
        apiKey: apiKey,
        enabled: true,
        priority: 0,
        timeoutSeconds: 30,
      }

      try {
        await API.createTorznabIndexer(formData)
        successCount++
      } catch (error) {
        errorCount++
      }
    }

    setLoading(false)

    if (errorCount === 0) {
      toast({
        title: 'Success',
        description: `Imported ${successCount} indexers`,
      })
    } else {
      toast({
        title: 'Partial Success',
        description: `Imported ${successCount} indexers, ${errorCount} failed`,
        variant: 'destructive',
      })
    }

    handleClose()
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
                        <label
                          htmlFor={indexer.id}
                          className="text-sm font-medium leading-none cursor-pointer"
                        >
                          {indexer.name}
                        </label>
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
