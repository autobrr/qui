/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState } from 'react'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Plus, Upload, Link } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'

interface AddTorrentDialogProps {
  instanceId: number
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

type TabValue = 'file' | 'url'

interface FormData {
  torrentFiles: File[] | null
  urls: string
  category: string
  tags: string
  startPaused: boolean
  savePath: string
}

export function AddTorrentDialog({ instanceId, open: controlledOpen, onOpenChange }: AddTorrentDialogProps) {
  const [internalOpen, setInternalOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<TabValue>('file')
  const queryClient = useQueryClient()
  
  // Use controlled state if provided, otherwise use internal state
  const open = controlledOpen !== undefined ? controlledOpen : internalOpen
  const setOpen = onOpenChange || setInternalOpen

  // Fetch categories for the dropdown
  const { data: categories } = useQuery({
    queryKey: ['categories', instanceId],
    queryFn: () => api.getCategories(instanceId),
    enabled: open,
  })

  const mutation = useMutation({
    retry: false, // Don't retry - could cause duplicate torrent additions
    mutationFn: async (data: FormData) => {
      const submitData: Parameters<typeof api.addTorrent>[1] = {
        startPaused: data.startPaused,
        savePath: data.savePath || undefined,
        category: data.category === '__none__' ? undefined : data.category || undefined,
        tags: data.tags ? data.tags.split(',').map(t => t.trim()).filter(Boolean) : undefined,
      }

      if (activeTab === 'file' && data.torrentFiles && data.torrentFiles.length > 0) {
        submitData.torrentFiles = data.torrentFiles
      } else if (activeTab === 'url' && data.urls) {
        submitData.urls = data.urls.split('\n').map(u => u.trim()).filter(Boolean)
      }

      return api.addTorrent(instanceId, submitData)
    },
    onSuccess: () => {
      // Add small delay to allow qBittorrent to process the new torrent
      setTimeout(() => {
        // Use refetch instead of invalidate to avoid loading state
        queryClient.refetchQueries({ 
          queryKey: ['torrents-list', instanceId],
          exact: false,
          type: 'active'
        })
        // Also refetch the metadata (categories, tags, counts)
        queryClient.refetchQueries({ 
          queryKey: ['instance-metadata', instanceId],
          exact: false,
          type: 'active'
        })
      }, 500) // Give qBittorrent time to process
      setOpen(false)
      form.reset()
    },
  })

  const form = useForm({
    defaultValues: {
      torrentFiles: null as File[] | null,
      urls: '',
      category: '',
      tags: '',
      startPaused: false,
      savePath: '',
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync(value)
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {controlledOpen === undefined && (
        <DialogTrigger asChild>
          <Button>
            <Plus className="mr-2 h-4 w-4 transition-transform duration-200" />
            Add Torrent
          </Button>
        </DialogTrigger>
      )}
      <DialogContent className="sm:max-w-[525px]">
        <DialogHeader>
          <DialogTitle>Add New Torrent</DialogTitle>
          <DialogDescription>
            Add a torrent file or magnet link to start downloading
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            form.handleSubmit()
          }}
          className="space-y-4"
        >
          {/* Tab selection */}
          <div className="flex rounded-md bg-muted p-1">
            <button
              type="button"
              onClick={() => setActiveTab('file')}
              className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
                activeTab === 'file'
                  ? 'bg-accent text-accent-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'
              }`}
            >
              <Upload className="mr-2 h-4 w-4 inline" />
              File
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('url')}
              className={`flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors ${
                activeTab === 'url'
                  ? 'bg-accent text-accent-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'
              }`}
            >
              <Link className="mr-2 h-4 w-4 inline" />
              URL
            </button>
          </div>

          {/* File upload or URL input */}
          {activeTab === 'file' ? (
            <form.Field
              name="torrentFiles"
              validators={{
                onChange: ({ value }) => {
                  if ((!value || value.length === 0) && activeTab === 'file') {
                    return 'Please select at least one torrent file'
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="torrentFiles">Torrent Files</Label>
                  <Input
                    id="torrentFiles"
                    type="file"
                    accept=".torrent"
                    multiple
                    onChange={(e) => {
                      const files = e.target.files ? Array.from(e.target.files) : null
                      field.handleChange(files)
                    }}
                  />
                  {field.state.value && field.state.value.length > 0 && (
                    <p className="text-sm text-muted-foreground">
                      {field.state.value.length} file{field.state.value.length > 1 ? 's' : ''} selected
                    </p>
                  )}
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>
          ) : (
            <form.Field
              name="urls"
              validators={{
                onChange: ({ value }) => {
                  if (!value && activeTab === 'url') {
                    return 'Please enter at least one URL or magnet link'
                  }
                  return undefined
                },
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="urls">URLs / Magnet Links</Label>
                  <Textarea
                    id="urls"
                    placeholder="Enter URLs or magnet links (one per line)"
                    rows={4}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>
          )}

          {/* Category */}
          <form.Field name="category">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="category">Category</Label>
                <Select
                  value={field.state.value}
                  onValueChange={field.handleChange}
                >
                  <SelectTrigger id="category">
                    <SelectValue placeholder="Select a category" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__none__">No category</SelectItem>
                    {categories && Object.entries(categories).map(([key, cat]) => (
                      <SelectItem key={key} value={cat.name}>
                        {cat.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
          </form.Field>

          {/* Tags */}
          <form.Field name="tags">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="tags">Tags</Label>
                <Input
                  id="tags"
                  placeholder="Enter tags separated by commas"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                />
              </div>
            )}
          </form.Field>

          {/* Save Path */}
          <form.Field name="savePath">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="savePath">Save Path</Label>
                <Input
                  id="savePath"
                  placeholder="Leave empty for default"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                />
              </div>
            )}
          </form.Field>

          {/* Start Paused */}
          <form.Field name="startPaused">
            {(field) => (
              <div className="flex items-center space-x-2">
                <Switch
                  id="startPaused"
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
                <Label htmlFor="startPaused">Start paused</Label>
              </div>
            )}
          </form.Field>

          {/* Submit buttons */}
          <div className="flex gap-2 pt-4">
            <form.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
            >
              {([canSubmit, isSubmitting]) => (
                <Button
                  type="submit"
                  disabled={!canSubmit || isSubmitting || mutation.isPending}
                  className="flex-1"
                >
                  {isSubmitting || mutation.isPending ? 'Adding...' : 'Add Torrent'}
                </Button>
              )}
            </form.Subscribe>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}