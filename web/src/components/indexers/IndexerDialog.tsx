/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from 'react'
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
import { Switch } from '@/components/ui/switch'
import type { TorznabIndexer, TorznabIndexerFormData } from '@/types'
import { api } from '@/lib/api'

interface IndexerDialogProps {
  open: boolean
  onClose: () => void
  mode: 'create' | 'edit'
  indexer?: TorznabIndexer | null
}

export function IndexerDialog({ open, onClose, mode, indexer }: IndexerDialogProps) {
  const [loading, setLoading] = useState(false)
  const [formData, setFormData] = useState<TorznabIndexerFormData>({
    name: '',
    baseUrl: '',
    apiKey: '',
    enabled: true,
    priority: 0,
    timeoutSeconds: 30,
  })

  useEffect(() => {
    if (mode === 'edit' && indexer) {
      setFormData({
        name: indexer.name,
        baseUrl: indexer.baseUrl,
        apiKey: '', // API key not returned from backend for security
        enabled: indexer.enabled,
        priority: indexer.priority,
        timeoutSeconds: indexer.timeoutSeconds,
      })
    } else {
      setFormData({
        name: '',
        baseUrl: '',
        apiKey: '',
        enabled: true,
        priority: 0,
        timeoutSeconds: 30,
      })
    }
  }, [mode, indexer, open])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)

    try {
      if (mode === 'create') {
        await api.createTorznabIndexer(formData)
        toast.success('Indexer created successfully')
      } else if (mode === 'edit' && indexer) {
        await api.updateTorznabIndexer(indexer.id, formData)
        toast.success('Indexer updated successfully')
      }
      onClose()
    } catch (error) {
      toast.error(`Failed to ${mode} indexer`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[525px]">
        <DialogHeader>
          <DialogTitle>
            {mode === 'create' ? 'Add Indexer' : 'Edit Indexer'}
          </DialogTitle>
          <DialogDescription>
            {mode === 'create'
              ? 'Add a new Torznab indexer for cross-seed discovery'
              : 'Update indexer settings'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="name">Name</Label>
              <Input
                id="name"
                value={formData.name}
                onChange={(e) =>
                  setFormData({ ...formData, name: e.target.value })
                }
                placeholder="My Indexer"
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="baseUrl">Base URL</Label>
              <Input
                id="baseUrl"
                type="url"
                value={formData.baseUrl}
                onChange={(e) =>
                  setFormData({ ...formData, baseUrl: e.target.value })
                }
                placeholder="http://localhost:9117"
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="apiKey">API Key</Label>
              <Input
                id="apiKey"
                type="password"
                value={formData.apiKey}
                onChange={(e) =>
                  setFormData({ ...formData, apiKey: e.target.value })
                }
                placeholder={mode === 'edit' ? 'Leave blank to keep existing' : 'Your API key'}
                required={mode === 'create'}
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="priority">Priority</Label>
                <Input
                  id="priority"
                  type="number"
                  value={formData.priority}
                  onChange={(e) =>
                    setFormData({ ...formData, priority: parseInt(e.target.value) })
                  }
                  min="0"
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="timeout">Timeout (seconds)</Label>
                <Input
                  id="timeout"
                  type="number"
                  value={formData.timeoutSeconds}
                  onChange={(e) =>
                    setFormData({ ...formData, timeoutSeconds: parseInt(e.target.value) })
                  }
                  min="5"
                  max="120"
                  required
                />
              </div>
            </div>
            <div className="flex items-center justify-between">
              <Label htmlFor="enabled">Enabled</Label>
              <Switch
                id="enabled"
                checked={formData.enabled}
                onCheckedChange={(checked) =>
                  setFormData({ ...formData, enabled: checked })
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? 'Saving...' : mode === 'create' ? 'Add' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
