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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
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
  const defaultForm: TorznabIndexerFormData = {
    name: '',
    base_url: '',
    indexer_id: '',
    api_key: '',
    backend: 'jackett',
    enabled: true,
    priority: 0,
    timeout_seconds: 30,
  }
  const [formData, setFormData] = useState<TorznabIndexerFormData>(defaultForm)
  const backend = formData.backend ?? 'jackett'
  const baseUrlPlaceholder = backend === 'prowlarr' ? 'http://localhost:9696' : 'http://localhost:9117'
  const requiresIndexerId = backend === 'prowlarr'

  useEffect(() => {
    if (mode === 'edit' && indexer) {
      setFormData({
        name: indexer.name,
        base_url: indexer.base_url,
        indexer_id: indexer.indexer_id,
        api_key: '', // API key not returned from backend for security
        backend: indexer.backend,
        enabled: indexer.enabled,
        priority: indexer.priority,
        timeout_seconds: indexer.timeout_seconds,
      })
    } else {
      setFormData({ ...defaultForm })
    }
  }, [mode, indexer, open])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)

    try {
      const backendValue = formData.backend ?? 'jackett'
      const trimmedIndexerId = formData.indexer_id !== undefined ? formData.indexer_id.trim() : undefined

      if (mode === 'create') {
        const createPayload: TorznabIndexerFormData = {
          name: formData.name,
          base_url: formData.base_url,
          api_key: formData.api_key.trim(),
          backend: backendValue,
          enabled: formData.enabled,
          priority: formData.priority,
          timeout_seconds: formData.timeout_seconds,
        }
        if (trimmedIndexerId) {
          createPayload.indexer_id = trimmedIndexerId
        }

        await api.createTorznabIndexer(createPayload)
        toast.success('Indexer created successfully')
      } else if (mode === 'edit' && indexer) {
        const updatePayload: Partial<TorznabIndexerFormData> = {
          name: formData.name,
          base_url: formData.base_url,
          backend: backendValue,
          enabled: formData.enabled,
          priority: formData.priority,
          timeout_seconds: formData.timeout_seconds,
        }

        if (formData.indexer_id !== undefined) {
          updatePayload.indexer_id = trimmedIndexerId ?? ''
        }

        const trimmedApiKey = formData.api_key.trim()
        if (trimmedApiKey) {
          updatePayload.api_key = trimmedApiKey
        }

        await api.updateTorznabIndexer(indexer.id, updatePayload)
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
            <Label htmlFor="backend">Backend</Label>
            <Select
              value={backend}
              onValueChange={(value) =>
                setFormData(prev => ({
                  ...prev,
                  backend: value as TorznabIndexerFormData["backend"],
                  indexer_id: value === 'native' ? '' : prev.indexer_id ?? '',
                }))
              }
            >
              <SelectTrigger id="backend">
                <SelectValue placeholder="Select backend" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="jackett">Jackett</SelectItem>
                <SelectItem value="prowlarr">Prowlarr</SelectItem>
                <SelectItem value="native">Native Torznab</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="baseUrl">Base URL</Label>
            <Input
              id="baseUrl"
              type="url"
              value={formData.base_url}
              onChange={(e) =>
                  setFormData({ ...formData, base_url: e.target.value })
              }
              placeholder={baseUrlPlaceholder}
              required
            />
          </div>
          {backend !== 'native' && (
            <div className="grid gap-2">
              <Label htmlFor="indexerId">
                Indexer ID {requiresIndexerId && <span className="text-destructive">*</span>}
              </Label>
              <Input
                id="indexerId"
                value={formData.indexer_id ?? ''}
                onChange={(e) =>
                  setFormData({ ...formData, indexer_id: e.target.value })
                }
                placeholder={backend === 'prowlarr' ? 'Prowlarr indexer ID (e.g., 1)' : 'Optional Jackett indexer ID (e.g., aither)'}
                required={requiresIndexerId}
              />
              <p className="text-xs text-muted-foreground">
                {backend === 'prowlarr'
                  ? 'Enter the numeric ID from the indexer details page in Prowlarr.'
                  : 'Optional for Jackett. Leave blank to let qui derive it automatically.'}
              </p>
            </div>
          )}
            <div className="grid gap-2">
              <Label htmlFor="apiKey">API Key</Label>
              <Input
                id="apiKey"
                type="password"
                value={formData.api_key}
                onChange={(e) =>
                  setFormData({ ...formData, api_key: e.target.value })
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
                  value={formData.timeout_seconds}
                  onChange={(e) =>
                    setFormData({ ...formData, timeout_seconds: parseInt(e.target.value) })
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
