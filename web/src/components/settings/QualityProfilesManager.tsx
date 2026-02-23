/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import {
  useCreateQualityProfile,
  useDeleteQualityProfile,
  useQualityProfiles,
  useUpdateQualityProfile,
} from "@/hooks/useQualityProfiles"
import type { QualityProfile, QualityProfileInput, RankingTier } from "@/types"
import {
  QUALITY_DEFAULT_VALUE_ORDERS,
  QUALITY_GROUP_FIELDS,
  QUALITY_RANK_FIELDS,
} from "@/types"
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronUp,
  Edit,
  GripVertical,
  Plus,
  Trash2,
  X,
} from "lucide-react"
import { useCallback, useEffect, useState } from "react"

// ─── helpers ────────────────────────────────────────────────────────────────

function fieldLabel(value: string): string {
  // Look up from known fields first, fall back to capitalization
  const allFields = [...QUALITY_GROUP_FIELDS, ...QUALITY_RANK_FIELDS]
  const found = allFields.find(f => f.value === value)
  return (found as { value: string; label: string } | undefined)?.label ?? (value.charAt(0).toUpperCase() + value.slice(1))
}

// ─── Tier editor ────────────────────────────────────────────────────────────

interface TierEditorProps {
  tier: RankingTier
  index: number
  total: number
  onChange: (t: RankingTier) => void
  onRemove: () => void
  onMoveUp: () => void
  onMoveDown: () => void
}

function TierEditor({ tier, index, total, onChange, onRemove, onMoveUp, onMoveDown }: TierEditorProps) {
  const [newValue, setNewValue] = useState("")
  const [expanded, setExpanded] = useState(true)

  const handleFieldChange = (field: string) => {
    const defaults = QUALITY_DEFAULT_VALUE_ORDERS[field as keyof typeof QUALITY_DEFAULT_VALUE_ORDERS] ?? []
    onChange({ field, valueOrder: [...defaults] })
  }

  const addValue = () => {
    const v = newValue.trim()
    if (!v || tier.valueOrder.includes(v)) return
    onChange({ ...tier, valueOrder: [...tier.valueOrder, v] })
    setNewValue("")
  }

  const removeValue = (idx: number) => {
    onChange({ ...tier, valueOrder: tier.valueOrder.filter((_, i) => i !== idx) })
  }

  const moveValue = (idx: number, dir: -1 | 1) => {
    const arr = [...tier.valueOrder]
    const target = idx + dir
    if (target < 0 || target >= arr.length) return
    ;[arr[idx], arr[target]] = [arr[target], arr[idx]]
    onChange({ ...tier, valueOrder: arr })
  }

  return (
    <div className="border rounded-lg bg-background">
      {/* tier header */}
      <div className="flex items-center gap-2 px-3 py-2">
        <GripVertical className="h-4 w-4 text-muted-foreground/40 shrink-0" />
        <button
          type="button"
          onClick={() => setExpanded(e => !e)}
          className="flex-1 flex items-center gap-2 text-left min-w-0"
        >
          <span className="text-xs text-muted-foreground font-mono shrink-0">#{index + 1}</span>
          <span className="text-sm font-medium truncate">
            {tier.field ? fieldLabel(tier.field) : <span className="text-muted-foreground italic">Choose field…</span>}
          </span>
          {tier.valueOrder.length > 0 && (
            <Badge variant="secondary" className="text-xs shrink-0">
              {tier.valueOrder.length} value{tier.valueOrder.length !== 1 ? "s" : ""}
            </Badge>
          )}
          {expanded ? <ChevronUp className="h-3.5 w-3.5 ml-auto shrink-0" /> : <ChevronDown className="h-3.5 w-3.5 ml-auto shrink-0" />}
        </button>
        <div className="flex items-center gap-1 shrink-0">
          <Button type="button" variant="ghost" size="icon" className="h-7 w-7" onClick={onMoveUp} disabled={index === 0}>
            <ArrowUp className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" variant="ghost" size="icon" className="h-7 w-7" onClick={onMoveDown} disabled={index === total - 1}>
            <ArrowDown className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" variant="ghost" size="icon" className="h-7 w-7 text-destructive hover:text-destructive" onClick={onRemove}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {expanded && (
        <div className="px-3 pb-3 space-y-3 border-t pt-3">
          {/* field select */}
          <div className="space-y-1.5">
            <Label className="text-xs">Quality field</Label>
            <Select value={tier.field} onValueChange={handleFieldChange}>
              <SelectTrigger className="h-8 text-sm">
                <SelectValue placeholder="Select a field…" />
              </SelectTrigger>
              <SelectContent>
                {QUALITY_RANK_FIELDS.map(f => (
                  <SelectItem key={f.value} value={f.value}>
                    {f.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {/* value ordering */}
          {tier.field && (
            <div className="space-y-1.5">
              <Label className="text-xs">Value order <span className="text-muted-foreground">(best → worst, top = highest quality)</span></Label>

              {tier.valueOrder.length > 0 ? (
                <div className="space-y-1">
                  {tier.valueOrder.map((val, vi) => (
                    <div key={vi} className="flex items-center gap-1 group">
                      <span className="text-xs text-muted-foreground font-mono w-5 text-right shrink-0">{vi + 1}.</span>
                      <span className="flex-1 text-sm px-2 py-0.5 bg-muted/50 rounded font-mono">{val}</span>
                      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                        <Button type="button" variant="ghost" size="icon" className="h-6 w-6" onClick={() => moveValue(vi, -1)} disabled={vi === 0}>
                          <ArrowUp className="h-3 w-3" />
                        </Button>
                        <Button type="button" variant="ghost" size="icon" className="h-6 w-6" onClick={() => moveValue(vi, 1)} disabled={vi === tier.valueOrder.length - 1}>
                          <ArrowDown className="h-3 w-3" />
                        </Button>
                        <Button type="button" variant="ghost" size="icon" className="h-6 w-6 text-destructive hover:text-destructive" onClick={() => removeValue(vi)}>
                          <X className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground italic py-1">No values defined — all items rank equally for this tier.</p>
              )}

              <div className="flex gap-2 mt-1">
                <Input
                  className="h-7 text-sm flex-1 font-mono"
                  placeholder="Add value…"
                  value={newValue}
                  onChange={e => setNewValue(e.target.value)}
                  onKeyDown={e => { if (e.key === "Enter") { e.preventDefault(); addValue() } }}
                />
                <Button type="button" variant="outline" size="sm" className="h-7 px-2" onClick={addValue}>
                  <Plus className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Profile dialog ──────────────────────────────────────────────────────────

interface ProfileDialogProps {
  open: boolean
  onClose: () => void
  profile?: QualityProfile | null
}

function ProfileDialog({ open, onClose, profile }: ProfileDialogProps) {
  const createMutation = useCreateQualityProfile()
  const updateMutation = useUpdateQualityProfile()
  const isEditing = !!profile

  // form state
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [groupFields, setGroupFields] = useState<string[]>(["title", "year"])
  const [rankingTiers, setRankingTiers] = useState<RankingTier[]>([])

  // hydrate when editing
  useEffect(() => {
    if (open && profile) {
      setName(profile.name)
      setDescription(profile.description ?? "")
      setGroupFields(profile.groupFields ?? [])
      setRankingTiers(profile.rankingTiers ? profile.rankingTiers.map(t => ({ ...t, valueOrder: [...t.valueOrder] })) : [])
    } else if (open && !profile) {
      setName("")
      setDescription("")
      setGroupFields(["title", "year"])
      setRankingTiers([])
    }
  }, [open, profile])

  const toggleGroupField = useCallback((val: string) => {
    setGroupFields(prev =>
      prev.includes(val) ? prev.filter(f => f !== val) : [...prev, val]
    )
  }, [])

  const addTier = () => {
    setRankingTiers(prev => [...prev, { field: "", valueOrder: [] }])
  }

  const updateTier = (idx: number, tier: RankingTier) => {
    setRankingTiers(prev => prev.map((t, i) => i === idx ? tier : t))
  }

  const removeTier = (idx: number) => {
    setRankingTiers(prev => prev.filter((_, i) => i !== idx))
  }

  const moveTier = (idx: number, dir: -1 | 1) => {
    setRankingTiers(prev => {
      const arr = [...prev]
      const target = idx + dir
      if (target < 0 || target >= arr.length) return arr
      ;[arr[idx], arr[target]] = [arr[target], arr[idx]]
      return arr
    })
  }

  const valid = name.trim().length > 0 && groupFields.length > 0

  const handleSubmit = async () => {
    if (!valid) return
    const payload: QualityProfileInput = {
      name: name.trim(),
      description: description.trim() || undefined,
      groupFields,
      rankingTiers: rankingTiers.filter(t => t.field),
    }
    if (isEditing && profile) {
      await updateMutation.mutateAsync({ id: profile.id, data: payload })
    } else {
      await createMutation.mutateAsync(payload)
    }
    onClose()
  }

  const isPending = createMutation.isPending || updateMutation.isPending

  return (
    <Dialog open={open} onOpenChange={v => { if (!v) onClose() }}>
      <DialogContent className="max-w-2xl max-h-[90vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{isEditing ? "Edit" : "New"} Quality Profile</DialogTitle>
          <DialogDescription>
            Define which fields identify the same content and how to rank technical quality.
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="flex-1 overflow-y-auto pr-1">
          <div className="space-y-6 py-1 px-1">
            {/* Basic info */}
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="qp-name">Name <span className="text-destructive">*</span></Label>
                <Input
                  id="qp-name"
                  placeholder="e.g. Movies, Music, TV Shows"
                  value={name}
                  onChange={e => setName(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="qp-desc">Description</Label>
                <Textarea
                  id="qp-desc"
                  placeholder="Optional description…"
                  value={description}
                  onChange={e => setDescription(e.target.value)}
                  rows={2}
                  className="resize-none"
                />
              </div>
            </div>

            <Separator />

            {/* Group fields */}
            <div className="space-y-3">
              <div>
                <h3 className="text-sm font-semibold">Content Identity Fields</h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Torrents that share <em>all</em> selected field values are considered the same content.
                  Select whichever fields identify the same release (e.g. Title + Year for movies).
                </p>
              </div>
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                {QUALITY_GROUP_FIELDS.map(f => (
                  <label
                    key={f.value}
                    className="flex items-center gap-2 rounded-md border px-3 py-2 cursor-pointer hover:bg-accent/50 transition-colors"
                  >
                    <Checkbox
                      checked={groupFields.includes(f.value)}
                      onCheckedChange={() => toggleGroupField(f.value)}
                    />
                    <span className="text-sm">{f.label}</span>
                  </label>
                ))}
              </div>
              {groupFields.length === 0 && (
                <p className="text-xs text-destructive">Select at least one grouping field.</p>
              )}
            </div>

            <Separator />

            {/* Ranking tiers */}
            <div className="space-y-3">
              <div>
                <h3 className="text-sm font-semibold">Quality Ranking Tiers</h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Add tiers in priority order (top tier evaluated first). Within each tier, list values from best to worst.
                  Torrents with higher-ranked values will be kept; inferior duplicates in the same group will be removed.
                </p>
              </div>

              {rankingTiers.length === 0 ? (
                <Card>
                  <CardContent className="py-6 text-center text-sm text-muted-foreground">
                    No ranking tiers defined. Add a tier to start ranking by quality.
                  </CardContent>
                </Card>
              ) : (
                <div className="space-y-2">
                  {rankingTiers.map((tier, idx) => (
                    <TierEditor
                      key={idx}
                      tier={tier}
                      index={idx}
                      total={rankingTiers.length}
                      onChange={t => updateTier(idx, t)}
                      onRemove={() => removeTier(idx)}
                      onMoveUp={() => moveTier(idx, -1)}
                      onMoveDown={() => moveTier(idx, 1)}
                    />
                  ))}
                </div>
              )}

              <Button type="button" variant="outline" size="sm" onClick={addTier} className="w-full">
                <Plus className="h-4 w-4 mr-1.5" />
                Add ranking tier
              </Button>
            </div>
          </div>
        </ScrollArea>

        <DialogFooter className="pt-4 border-t">
          <Button variant="outline" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!valid || isPending}>
            {isPending ? "Saving…" : isEditing ? "Save changes" : "Create profile"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ─── Main manager ────────────────────────────────────────────────────────────

export function QualityProfilesManager() {
  const { data: profiles, isLoading, error } = useQualityProfiles()
  const deleteMutation = useDeleteQualityProfile()

  const [showCreate, setShowCreate] = useState(false)
  const [editProfile, setEditProfile] = useState<QualityProfile | null>(null)
  const [deleteProfile, setDeleteProfile] = useState<QualityProfile | null>(null)

  if (isLoading) {
    return <div className="text-sm text-muted-foreground py-4">Loading quality profiles…</div>
  }

  if (error) {
    return <div className="text-sm text-destructive py-4">Failed to load quality profiles.</div>
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          {profiles?.length
            ? `${profiles.length} profile${profiles.length !== 1 ? "s" : ""} configured`
            : "No quality profiles yet"}
        </p>
        <Button size="sm" onClick={() => setShowCreate(true)}>
          <Plus className="h-4 w-4 mr-1.5" />
          New profile
        </Button>
      </div>

      {profiles && profiles.length > 0 ? (
        <div className="space-y-2">
          {profiles.map(profile => (
            <div
              key={profile.id}
              className="flex items-start gap-3 rounded-lg border px-4 py-3"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-sm">{profile.name}</span>
                </div>
                {profile.description && (
                  <p className="text-xs text-muted-foreground mt-0.5">{profile.description}</p>
                )}
                <div className="flex flex-wrap gap-1.5 mt-2">
                  <span className="text-xs text-muted-foreground">Groups by:</span>
                  {(profile.groupFields ?? []).map(f => (
                    <Badge key={f} variant="secondary" className="text-xs">
                      {fieldLabel(f)}
                    </Badge>
                  ))}
                  {(profile.rankingTiers ?? []).length > 0 && (
                    <>
                      <span className="text-xs text-muted-foreground ml-1">Ranks by:</span>
                      {profile.rankingTiers.map((t, i) => (
                        <Badge key={i} variant="outline" className="text-xs">
                          {fieldLabel(t.field)}
                        </Badge>
                      ))}
                    </>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1 shrink-0">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={() => setEditProfile(profile)}
                >
                  <Edit className="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 text-destructive hover:text-destructive"
                  onClick={() => setDeleteProfile(profile)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <Card>
          <CardContent className="py-8 text-center text-sm text-muted-foreground">
            <p>No quality profiles defined.</p>
            <p className="mt-1">Create a profile to enable quality-based torrent management in automations.</p>
          </CardContent>
        </Card>
      )}

      {/* Create / Edit dialog */}
      <ProfileDialog
        open={showCreate || editProfile !== null}
        onClose={() => { setShowCreate(false); setEditProfile(null) }}
        profile={editProfile}
      />

      {/* Delete confirmation */}
      <AlertDialog open={deleteProfile !== null} onOpenChange={v => { if (!v) setDeleteProfile(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete quality profile</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &quot;{deleteProfile?.name}&quot;?
              Any automations referencing this profile will need to be updated.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => {
                if (deleteProfile) {
                  deleteMutation.mutate(deleteProfile.id)
                  setDeleteProfile(null)
                }
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
