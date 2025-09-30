/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type { TorrentCreationParams, TorrentFormat } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, Loader2 } from "lucide-react"
import { useState } from "react"

interface TorrentCreatorDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TorrentCreatorDialog({ instanceId, open, onOpenChange }: TorrentCreatorDialogProps) {
  const [error, setError] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const mutation = useMutation({
    mutationFn: async (data: TorrentCreationParams) => {
      return api.createTorrent(instanceId, data)
    },
    onSuccess: () => {
      setError(null)
      onOpenChange(false)
      form.reset()
    },
    onError: (err: Error) => {
      setError(err.message)
    },
  })

  const form = useForm({
    defaultValues: {
      sourcePath: "",
      private: false,
      trackers: "",
      comment: "",
      source: "",
      startSeeding: true,
      // Advanced options
      format: "" as TorrentFormat | "",
      pieceSize: "",
      torrentFilePath: "",
      urlSeeds: "",
    },
    onSubmit: async ({ value }) => {
      setError(null)

      // Parse trackers (one per line)
      const trackers = value.trackers
        ?.split("\n")
        .map((t) => t.trim())
        .filter(Boolean)

      // Parse URL seeds (one per line)
      const urlSeeds = value.urlSeeds
        ?.split("\n")
        .map((u) => u.trim())
        .filter(Boolean)

      const params: TorrentCreationParams = {
        sourcePath: value.sourcePath,
        private: value.private,
        trackers: trackers && trackers.length > 0 ? trackers : undefined,
        comment: value.comment || undefined,
        source: value.source || undefined,
        startSeeding: value.startSeeding, // Always send boolean value
        // Advanced options
        format: value.format || undefined,
        pieceSize: value.pieceSize ? parseInt(value.pieceSize) : undefined,
        torrentFilePath: value.torrentFilePath || undefined,
        urlSeeds: urlSeeds && urlSeeds.length > 0 ? urlSeeds : undefined,
      }

      mutation.mutate(params)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create Torrent</DialogTitle>
          <DialogDescription>
            Create a new .torrent file from a file or folder on the server
          </DialogDescription>
        </DialogHeader>

        <form
          onSubmit={(e) => {
            e.preventDefault()
            e.stopPropagation()
            form.handleSubmit()
          }}
          className="space-y-4"
        >
          {error && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {/* Source Path */}
          <form.Field name="sourcePath">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="sourcePath">
                  Source Path <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="sourcePath"
                  placeholder="/path/to/file/or/folder"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  required
                />
                <p className="text-sm text-muted-foreground">
                  Full path on the server where qBittorrent is running
                </p>
              </div>
            )}
          </form.Field>

          {/* Private */}
          <form.Field name="private">
            {(field) => (
              <div className="flex items-center justify-between">
                <div className="space-y-2">
                  <Label htmlFor="private">Private torrent</Label>
                  <p className="text-sm text-muted-foreground">
                    Disable DHT, PEX, and local peer discovery
                  </p>
                </div>
                <Switch
                  id="private"
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
              </div>
            )}
          </form.Field>

          {/* Trackers */}
          <form.Field name="trackers">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="trackers">Trackers</Label>
                <p className="text-sm text-muted-foreground">
                  One tracker URL per line
                </p>
                <Textarea
                  id="trackers"
                  placeholder="https://tracker.example.com:443/announce&#10;udp://tracker.example2.com:6969/announce"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  rows={4}
                />
              </div>
            )}
          </form.Field>

          {/* Comment */}
          <form.Field name="comment">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="comment">Comment</Label>
                <Input
                  id="comment"
                  placeholder="Optional comment"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                />
              </div>
            )}
          </form.Field>

          {/* Source */}
          <form.Field name="source">
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="source">Source</Label>
                <Input
                  id="source"
                  placeholder="Optional source tag"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                />
              </div>
            )}
          </form.Field>

          {/* Start Seeding */}
          <form.Field name="startSeeding">
            {(field) => (
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="startSeeding">Add to qBittorrent</Label>
                  <p className="text-sm text-muted-foreground">
                    Add the created torrent to qBittorrent and start seeding. If disabled, only creates the .torrent file for download.
                  </p>
                </div>
                <Switch
                  id="startSeeding"
                  checked={field.state.value}
                  onCheckedChange={field.handleChange}
                />
              </div>
            )}
          </form.Field>

          {/* Advanced Options */}
          <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
            <CollapsibleTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                className="w-full justify-between p-0 hover:bg-transparent"
              >
                <span className="text-sm font-medium">Advanced Options</span>
                <ChevronDown
                  className={`h-4 w-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`}
                />
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent className="space-y-4 pt-4">
              {/* Torrent Format */}
              <form.Field name="format">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="format">Torrent Format</Label>
                    <Select
                      value={field.state.value}
                      onValueChange={(value) => field.handleChange(value as TorrentFormat | "")}
                    >
                      <SelectTrigger id="format">
                        <SelectValue placeholder="Auto (v1)" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="v1">v1 (Compatible)</SelectItem>
                        <SelectItem value="v2">v2 (Modern)</SelectItem>
                        <SelectItem value="hybrid">Hybrid (v1 + v2)</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-sm text-muted-foreground">
                      v1 for maximum compatibility, v2 for modern clients, hybrid for both
                    </p>
                  </div>
                )}
              </form.Field>

              {/* Piece Size */}
              <form.Field name="pieceSize">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="pieceSize">Piece Size (bytes)</Label>
                    <Input
                      id="pieceSize"
                      type="number"
                      placeholder="Auto (leave empty)"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                    <p className="text-sm text-muted-foreground">
                      Leave empty for auto (recommended). Common: 16384, 32768, 65536, 131072, 262144
                    </p>
                  </div>
                )}
              </form.Field>

              {/* Torrent File Path */}
              <form.Field name="torrentFilePath">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="torrentFilePath">Save .torrent to (optional)</Label>
                    <Input
                      id="torrentFilePath"
                      placeholder="/path/to/save/file.torrent"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                    <p className="text-sm text-muted-foreground">
                      Where to save the .torrent file on the server
                    </p>
                  </div>
                )}
              </form.Field>

              {/* URL Seeds */}
              <form.Field name="urlSeeds">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="urlSeeds">Web Seeds (HTTP/HTTPS)</Label>
                    <Textarea
                      id="urlSeeds"
                      placeholder="https://mirror1.example.com/path&#10;https://mirror2.example.com/path"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      rows={3}
                    />
                    <p className="text-sm text-muted-foreground">
                      HTTP/HTTPS URLs where the content can be downloaded. One URL per line.
                    </p>
                  </div>
                )}
              </form.Field>
            </CollapsibleContent>
          </Collapsible>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Create Torrent
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}