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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import type { TorrentCreationParams, TorrentFormat } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, Info, Loader2 } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { pieceSizeOptions, TorrentPieceSize } from "./piece-size"

interface TorrentCreatorDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TorrentCreatorDialog({ instanceId, open, onOpenChange }: TorrentCreatorDialogProps) {
  const { t } = useTranslation()
  const [error, setError] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const queryClient = useQueryClient()

  // Fetch active trackers for the select dropdown
  const { data: activeTrackers } = useQuery({
    queryKey: ["active-trackers", instanceId],
    queryFn: () => api.getActiveTrackers(instanceId),
    enabled: open, // Only fetch when dialog is open
  })

  const mutation = useMutation({
    mutationFn: async (data: TorrentCreationParams) => {
      return api.createTorrent(instanceId, data)
    },
    onSuccess: () => {
      setError(null)
      onOpenChange(false)
      form.reset()
      // Invalidate tasks and badge count so polling views update immediately
      queryClient.invalidateQueries({ queryKey: ["torrent-creation-tasks", instanceId] })
      queryClient.invalidateQueries({ queryKey: ["active-task-count", instanceId] })
      toast.success(t("torrent_creator_dialog.toasts.queued"))
    },
    onError: (err: Error) => {
      setError(err.message)
      toast.error(err.message || t("torrent_creator_dialog.toasts.failed"))
    },
  })

  const form = useForm({
    defaultValues: {
      sourcePath: "",
      private: true,
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

  // Reset form and error state when dialog closes
  useEffect(() => {
    if (!open) {
      form.reset()
      setError(null)
      setAdvancedOpen(false)
    }
  }, [open, form])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("torrent_creator_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("torrent_creator_dialog.description")}
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
                  {t("torrent_creator_dialog.source_path.label")} <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="sourcePath"
                  placeholder={t("torrent_creator_dialog.source_path.placeholder")}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  required
                />
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <span>{t("torrent_creator_dialog.source_path.description")}</span>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-4 w-4 cursor-help shrink-0" />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{t("torrent_creator_dialog.source_path.tooltip")}</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
            )}
          </form.Field>

          {/* Private */}
          <form.Field name="private">
            {(field) => (
              <div className="flex items-center justify-between">
                <div className="space-y-2">
                  <Label htmlFor="private">{t("torrent_creator_dialog.private.label")}</Label>
                  <p className="text-sm text-muted-foreground">
                    {t("torrent_creator_dialog.private.description")}
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
                <Label htmlFor="trackers">{t("torrent_creator_dialog.trackers.label")}</Label>
                {activeTrackers && Object.keys(activeTrackers).length > 0 && (
                  <div className="space-y-2">
                    <p className="text-sm text-muted-foreground">
                      {t("torrent_creator_dialog.trackers.description")}
                    </p>
                    <Select
                      value=""
                      onValueChange={(trackerUrl) => {
                        const currentTrackers = field.state.value
                        const newTrackers = currentTrackers? `${currentTrackers}\n${trackerUrl}`: trackerUrl
                        field.handleChange(newTrackers)
                      }}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder={t("torrent_creator_dialog.trackers.placeholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        {Object.entries(activeTrackers)
                          .sort(([domainA], [domainB]) => domainA.localeCompare(domainB))
                          .map(([domain, url]) => (
                            <SelectItem key={domain} value={url}>
                              {domain}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
                <p className="text-sm text-muted-foreground">
                  {t("torrent_creator_dialog.trackers.line_by_line")}
                </p>
                <Textarea
                  id="trackers"
                  placeholder={t("torrent_creator_dialog.trackers.textarea_placeholder")}
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
                <Label htmlFor="comment">{t("torrent_creator_dialog.comment.label")}</Label>
                <Input
                  id="comment"
                  placeholder={t("torrent_creator_dialog.comment.placeholder")}
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
                <Label htmlFor="source">{t("torrent_creator_dialog.source.label")}</Label>
                <Input
                  id="source"
                  placeholder={t("torrent_creator_dialog.source.placeholder")}
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
                  <Label htmlFor="startSeeding">{t("torrent_creator_dialog.start_seeding.label")}</Label>
                  <p className="text-sm text-muted-foreground">
                    {t("torrent_creator_dialog.start_seeding.description")}
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
                <span className="text-sm font-medium">{t("torrent_creator_dialog.advanced_options.title")}</span>
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
                    <Label htmlFor="format">{t("torrent_creator_dialog.advanced_options.format.label")}</Label>
                    <Select
                      value={field.state.value}
                      onValueChange={(value) => field.handleChange(value as TorrentFormat | "")}
                    >
                      <SelectTrigger id="format">
                        <SelectValue placeholder={t("torrent_creator_dialog.advanced_options.format.placeholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="v1">{t("torrent_creator_dialog.advanced_options.format.v1")}</SelectItem>
                        <SelectItem value="v2">{t("torrent_creator_dialog.advanced_options.format.v2")}</SelectItem>
                        <SelectItem value="hybrid">{t("torrent_creator_dialog.advanced_options.format.hybrid")}</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-sm text-muted-foreground">
                      {t("torrent_creator_dialog.advanced_options.format.description")}
                    </p>
                  </div>
                )}
              </form.Field>

              {/* Piece Size
                  https://github.com/qbittorrent/qBittorrent/blob/master/src/gui/torrentcreatordialog.cpp#L86-L92

                  m_ui->comboPieceSize->addItem(tr("Auto"), 0);
                  for (int i = 4; i <= 17; ++i)
                  {
                      const int size = 1024 << i;
                      const QString displaySize = Utils::Misc::friendlyUnit(size, false, 0);
                      m_ui->comboPieceSize->addItem(displaySize, size);
                  }
              */}
              <form.Field name="pieceSize">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="pieceSize">{t("torrent_creator_dialog.advanced_options.piece_size.label")}</Label>
                    <Select
                      value={field.state.value || TorrentPieceSize.Auto}
                      onValueChange={field.handleChange}
                    >
                      <SelectTrigger id="pieceSize">
                        <SelectValue placeholder={t("torrent_creator_dialog.advanced_options.piece_size.placeholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        {pieceSizeOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.value === TorrentPieceSize.Auto
                              ? t("torrent_creator_dialog.advanced_options.piece_size.options.auto")
                              : option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-sm text-muted-foreground">
                      {t("torrent_creator_dialog.advanced_options.piece_size.description")}
                    </p>
                  </div>
                )}
              </form.Field>

              {/* Torrent File Path */}
              <form.Field name="torrentFilePath">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="torrentFilePath">{t("torrent_creator_dialog.advanced_options.save_path.label")}</Label>
                    <Input
                      id="torrentFilePath"
                      placeholder={t("torrent_creator_dialog.advanced_options.save_path.placeholder")}
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <span>{t("torrent_creator_dialog.advanced_options.save_path.description")}</span>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Info className="h-4 w-4 cursor-help shrink-0" />
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">
                          <p>{t("torrent_creator_dialog.advanced_options.save_path.tooltip")}</p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                  </div>
                )}
              </form.Field>

              {/* URL Seeds */}
              <form.Field name="urlSeeds">
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor="urlSeeds">{t("torrent_creator_dialog.advanced_options.web_seeds.label")}</Label>
                    <Textarea
                      id="urlSeeds"
                      placeholder={t("torrent_creator_dialog.advanced_options.web_seeds.placeholder")}
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      rows={3}
                    />
                    <p className="text-sm text-muted-foreground">
                      {t("torrent_creator_dialog.advanced_options.web_seeds.description")}
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
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("torrent_creator_dialog.actions.submit")}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
