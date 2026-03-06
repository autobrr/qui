/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"

import { useForm } from "@tanstack/react-form"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { AlertCircle, ChevronDown, Info, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
  DialogFooter,
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
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceTrackers } from "@/hooks/useInstanceTrackers"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { TorrentCreationParams, TorrentFormat } from "@/types"

import { pieceSizeOptions, TorrentPieceSize } from "./piece-size"

/** Parse newline-separated input into array of non-empty trimmed strings */
function parseLines(input: string): string[] {
  return input.split("\n").map((line) => line.trim()).filter(Boolean)
}

interface PathSuggestionsProps {
  suggestions: string[]
  highlightedIndex: number
  onSelect: (entry: string) => void
}

function PathSuggestions({ suggestions, highlightedIndex, onSelect }: PathSuggestionsProps): React.ReactNode {
  if (suggestions.length === 0) return null

  return (
    <div className="relative">
      <div className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
        <div className="max-h-55 overflow-y-auto py-1">
          {suggestions.map((entry, idx) => (
            <button
              key={entry}
              type="button"
              title={entry}
              className={cn(
                "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                highlightedIndex === idx? "bg-accent text-accent-foreground": "hover:bg-accent/70"
              )}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onSelect(entry)}
            >
              <span className="block truncate text-left">{entry}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}

const FORM_ID = "torrent-creator-form"

interface TorrentCreatorDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TorrentCreatorDialog({ instanceId, open, onOpenChange }: TorrentCreatorDialogProps) {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => t(key as any, options as any)
  const [error, setError] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const queryClient = useQueryClient()

  const { versionInfo } = useQBittorrentAppInfo(instanceId)
  const supportsFormatSelection = versionInfo.isLibtorrent2 !== false
  const libtorrentVersionLabel = versionInfo.libtorrentMajorVersion
    ? tr("searchPage.torrentCreatorDialog.libtorrentVersion", { major: versionInfo.libtorrentMajorVersion })
    : tr("searchPage.torrentCreatorDialog.libtorrentVersionFallback")

  // Fetch active trackers for the select dropdown
  const { data: activeTrackers } = useInstanceTrackers(instanceId, { enabled: open })

  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false

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
      toast.success(tr("searchPage.torrentCreatorDialog.toasts.taskQueued"))
    },
    onError: (err: Error) => {
      setError(err.message)
      toast.error(err.message || tr("searchPage.torrentCreatorDialog.toasts.failedTask"))
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
      format: "v1" as TorrentFormat,
      pieceSize: "",
      torrentFilePath: "",
      urlSeeds: "",
    },
    onSubmit: async ({ value }) => {
      setError(null)

      const trackers = parseLines(value.trackers)
      const urlSeeds = parseLines(value.urlSeeds)

      const params: TorrentCreationParams = {
        sourcePath: value.sourcePath,
        private: value.private,
        trackers: trackers.length > 0 ? trackers : undefined,
        comment: value.comment || undefined,
        source: value.source || undefined,
        startSeeding: value.startSeeding,
        format: supportsFormatSelection ? value.format : "v1",
        pieceSize: value.pieceSize ? parseInt(value.pieceSize) : undefined,
        torrentFilePath: value.torrentFilePath || undefined,
        urlSeeds: urlSeeds.length > 0 ? urlSeeds : undefined,
      }

      mutation.mutate(params)
    },
  })

  const setSourcePath = useCallback((path: string) => {
    form.setFieldValue("sourcePath", path)
  }, [form])

  const setTorrentFilePath = useCallback((path: string) => {
    form.setFieldValue("torrentFilePath", path)
  }, [form])

  const {
    suggestions: sourcePathSuggestions,
    handleInputChange: handleSourcePathInputChange,
    handleSelect: handleSourcePathSelect,
    handleKeyDown: handleSourcePathKeyDown,
    highlightedIndex: sourcePathHighlightedIndex,
    showSuggestions: showSourcePathSuggestions,
    inputRef: sourcePathInputRef,
  } = usePathAutocomplete(setSourcePath, instanceId)

  const {
    suggestions: torrentFilePathSuggestions,
    handleInputChange: handleTorrentFilePathInputChange,
    handleSelect: handleTorrentFilePathSelect,
    handleKeyDown: handleTorrentFilePathKeyDown,
    highlightedIndex: torrentFilePathHighlightedIndex,
    showSuggestions: showTorrentFilePathSuggestions,
    inputRef: torrentFilePathInputRef,
  } = usePathAutocomplete(setTorrentFilePath, instanceId)

  useEffect(() => {
    if (!supportsFormatSelection) {
      form.setFieldValue("format", "v1")
    }
  }, [supportsFormatSelection, form])

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
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{tr("searchPage.torrentCreatorDialog.title")}</DialogTitle>
          <DialogDescription>
            {tr("searchPage.torrentCreatorDialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-0">
          <form
            id={FORM_ID}
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
                    {tr("searchPage.torrentCreatorDialog.form.sourcePath")} <span className="text-destructive">*</span>
                  </Label>
                  <Input
                    id="sourcePath"
                    ref={supportsPathAutocomplete ? sourcePathInputRef : undefined}
                    placeholder={tr("searchPage.torrentCreatorDialog.form.sourcePathPlaceholder")}
                    autoComplete="off"
                    spellCheck={false}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onKeyDown={supportsPathAutocomplete ? handleSourcePathKeyDown : undefined}
                    onChange={(e) => {
                      field.handleChange(e.target.value)
                      if (supportsPathAutocomplete) {
                        handleSourcePathInputChange(e.target.value)
                      }
                    }}
                    required
                  />

                  {supportsPathAutocomplete && showSourcePathSuggestions && (
                    <PathSuggestions
                      suggestions={sourcePathSuggestions}
                      highlightedIndex={sourcePathHighlightedIndex}
                      onSelect={handleSourcePathSelect}
                    />
                  )}

                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <span>{tr("searchPage.torrentCreatorDialog.form.sourcePathHint")}</span>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <Info className="h-4 w-4 cursor-help shrink-0" />
                      </TooltipTrigger>
                      <TooltipContent>
                        <p>{tr("searchPage.torrentCreatorDialog.form.windowsPathHint")}</p>
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
                    <Label htmlFor="private">{tr("searchPage.torrentCreatorDialog.form.privateTorrent")}</Label>
                    <p className="text-sm text-muted-foreground">
                      {tr("searchPage.torrentCreatorDialog.form.privateTorrentHint")}
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
                  <Label htmlFor="trackers">{tr("searchPage.torrentCreatorDialog.form.trackers")}</Label>
                  {activeTrackers && Object.keys(activeTrackers).length > 0 && (
                    <div className="space-y-2">
                      <p className="text-sm text-muted-foreground">
                        {tr("searchPage.torrentCreatorDialog.form.trackersHint")}
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
                          <SelectValue placeholder={tr("searchPage.torrentCreatorDialog.form.trackersSelectPlaceholder")} />
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
                    {tr("searchPage.torrentCreatorDialog.form.onePerLine")}
                  </p>
                  <Textarea
                    id="trackers"
                    placeholder={tr("searchPage.torrentCreatorDialog.form.trackersPlaceholder")}
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
                  <Label htmlFor="comment">{tr("searchPage.torrentCreatorDialog.form.comment")}</Label>
                  <Input
                    id="comment"
                    placeholder={tr("searchPage.torrentCreatorDialog.form.commentPlaceholder")}
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
                  <Label htmlFor="source">{tr("searchPage.torrentCreatorDialog.form.source")}</Label>
                  <Input
                    id="source"
                    placeholder={tr("searchPage.torrentCreatorDialog.form.sourcePlaceholder")}
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
                    <Label htmlFor="startSeeding">{tr("searchPage.torrentCreatorDialog.form.startSeeding")}</Label>
                    <p className="text-sm text-muted-foreground">
                      {tr("searchPage.torrentCreatorDialog.form.startSeedingHint")}
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
                  <span className="text-sm font-medium">{tr("searchPage.torrentCreatorDialog.form.advancedOptions")}</span>
                  <ChevronDown
                    className={`h-4 w-4 transition-transform ${advancedOpen ? "rotate-180" : ""}`}
                  />
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent className="space-y-4 pt-4">
                {/* Torrent Format */}
                {supportsFormatSelection ? (
                  <form.Field name="format">
                    {(field) => (
                      <div className="space-y-2">
                        <Label htmlFor="format">{tr("searchPage.torrentCreatorDialog.form.torrentFormat")}</Label>
                        <Select
                          value={field.state.value}
                          onValueChange={(value) => field.handleChange(value as TorrentFormat)}
                        >
                          <SelectTrigger id="format">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="v1">{tr("searchPage.torrentCreatorDialog.form.formatV1")}</SelectItem>
                            <SelectItem value="v2">{tr("searchPage.torrentCreatorDialog.form.formatV2")}</SelectItem>
                            <SelectItem value="hybrid">{tr("searchPage.torrentCreatorDialog.form.formatHybrid")}</SelectItem>
                          </SelectContent>
                        </Select>
                        <p className="text-sm text-muted-foreground">
                          {tr("searchPage.torrentCreatorDialog.form.torrentFormatHint")}
                        </p>
                      </div>
                    )}
                  </form.Field>
                ) : (
                  <Alert className="bg-muted/40 text-muted-foreground">
                    <Info className="h-4 w-4" />
                    <AlertTitle>{tr("searchPage.torrentCreatorDialog.form.formatUnavailableTitle")}</AlertTitle>
                    <AlertDescription>
                      {tr("searchPage.torrentCreatorDialog.form.formatUnavailableDescription", { version: libtorrentVersionLabel })}
                    </AlertDescription>
                  </Alert>
                )}

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
                      <Label htmlFor="pieceSize">{tr("searchPage.torrentCreatorDialog.form.pieceSize")}</Label>
                      <Select
                        value={field.state.value || TorrentPieceSize.Auto}
                        onValueChange={field.handleChange}
                      >
                        <SelectTrigger id="pieceSize">
                          <SelectValue placeholder={tr("searchPage.torrentCreatorDialog.form.pieceSizeAuto")} />
                        </SelectTrigger>
                        <SelectContent>
                          {pieceSizeOptions.map((option) => (
                            <SelectItem key={option.value} value={option.value}>
                              {option.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-sm text-muted-foreground">
                        {tr("searchPage.torrentCreatorDialog.form.pieceSizeHint")}
                      </p>
                    </div>
                  )}
                </form.Field>

                {/* Torrent File Path */}
                <form.Field name="torrentFilePath">
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="torrentFilePath">{tr("searchPage.torrentCreatorDialog.form.torrentFilePath")}</Label>
                      <Input
                        id="torrentFilePath"
                        ref={supportsPathAutocomplete ? torrentFilePathInputRef : undefined}
                        placeholder={tr("searchPage.torrentCreatorDialog.form.torrentFilePathPlaceholder")}
                        autoComplete="off"
                        spellCheck={false}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onKeyDown={supportsPathAutocomplete ? handleTorrentFilePathKeyDown : undefined}
                        onChange={(e) => {
                          field.handleChange(e.target.value)
                          if (supportsPathAutocomplete) {
                            handleTorrentFilePathInputChange(e.target.value)
                          }
                        }}
                      />

                      {supportsPathAutocomplete && showTorrentFilePathSuggestions && (
                        <PathSuggestions
                          suggestions={torrentFilePathSuggestions}
                          highlightedIndex={torrentFilePathHighlightedIndex}
                          onSelect={handleTorrentFilePathSelect}
                        />
                      )}

                      <div className="flex items-center gap-2 text-sm text-muted-foreground">
                        <span>{tr("searchPage.torrentCreatorDialog.form.torrentFilePathHint")}</span>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Info className="h-4 w-4 cursor-help shrink-0" />
                          </TooltipTrigger>
                          <TooltipContent className="max-w-xs">
                            <p>{tr("searchPage.torrentCreatorDialog.form.torrentFilePathTooltip")}</p>
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
                      <Label htmlFor="urlSeeds">{tr("searchPage.torrentCreatorDialog.form.webSeeds")}</Label>
                      <Textarea
                        id="urlSeeds"
                        placeholder={tr("searchPage.torrentCreatorDialog.form.webSeedsPlaceholder")}
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        rows={3}
                      />
                      <p className="text-sm text-muted-foreground">
                        {tr("searchPage.torrentCreatorDialog.form.webSeedsHint")}
                      </p>
                    </div>
                  )}
                </form.Field>
              </CollapsibleContent>
            </Collapsible>
          </form>
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            {tr("searchPage.torrentCreatorDialog.actions.cancel")}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={mutation.isPending}>
            {mutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {tr("searchPage.torrentCreatorDialog.actions.createTorrent")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
