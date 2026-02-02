/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import type { CrossSeedBlocklistEntry, Instance } from "@/types"

interface BlocklistTabProps {
  instances: Instance[]
}

const infoHashRegex = /^[a-fA-F0-9]{40}$|^[a-fA-F0-9]{64}$/

function normalizeInfoHash(value: string): string {
  return value.trim().toLowerCase()
}

function isValidInfoHash(value: string): boolean {
  return infoHashRegex.test(value)
}

export function BlocklistTab({ instances }: BlocklistTabProps) {
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  const [instanceId, setInstanceId] = useState<number | null>(null)
  const [infoHash, setInfoHash] = useState("")
  const [note, setNote] = useState("")

  useEffect(() => {
    if (instances.length === 0) {
      if (instanceId !== null) {
        setInstanceId(null)
      }
      return
    }

    const hasInstance = instanceId !== null && instances.some((instance) => instance.id === instanceId)
    if (!hasInstance) {
      setInstanceId(instances[0].id)
    }
  }, [instanceId, instances])

  const { data: blocklistData, isLoading } = useQuery({
    queryKey: ["cross-seed", "blocklist", instanceId],
    queryFn: () => instanceId ? api.listCrossSeedBlocklist(instanceId) : Promise.resolve([]),
    enabled: instanceId !== null,
  })
  const blocklist = blocklistData ?? []

  const addMutation = useMutation({
    mutationFn: (payload: { instanceId: number; infoHash: string; note?: string }) => api.addCrossSeedBlocklist(payload),
    onSuccess: () => {
      toast.success("Added to blocklist")
      setInfoHash("")
      setNote("")
      queryClient.invalidateQueries({ queryKey: ["cross-seed", "blocklist"] })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (entry: CrossSeedBlocklistEntry) => api.deleteCrossSeedBlocklist(entry.instanceId, entry.infoHash),
    onSuccess: () => {
      toast.success("Removed from blocklist")
      queryClient.invalidateQueries({ queryKey: ["cross-seed", "blocklist"] })
    },
    onError: (error: Error) => {
      toast.error(error.message)
    },
  })

  const handleAdd = useCallback(() => {
    if (!instanceId) {
      toast.error("Select an instance")
      return
    }

    const normalized = normalizeInfoHash(infoHash)
    if (!isValidInfoHash(normalized)) {
      toast.error("Infohash must be 40 or 64 hex characters")
      return
    }

    addMutation.mutate({
      instanceId,
      infoHash: normalized,
      note: note.trim() || undefined,
    })
  }, [addMutation, infoHash, instanceId, note])

  const formatDateValue = useCallback((value?: string) => {
    if (!value) return "—"
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return "—"
    return formatDate(parsed)
  }, [formatDate])

  if (instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Cross-Seed Blocklist</CardTitle>
          <CardDescription>
            Add instances to manage blocked cross-seed infohashes.
          </CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Cross-Seed Blocklist</CardTitle>
          <CardDescription>
            Prevent specific infohashes from being injected on a per-instance basis.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 md:grid-cols-[200px_1fr]">
            <div className="space-y-2">
              <Label htmlFor="blocklist-instance">Instance</Label>
              <Select
                value={instanceId ? instanceId.toString() : ""}
                onValueChange={(value) => setInstanceId(Number(value))}
              >
                <SelectTrigger id="blocklist-instance">
                  <SelectValue placeholder="Select instance" />
                </SelectTrigger>
                <SelectContent>
                  {instances.map((instance) => (
                    <SelectItem key={instance.id} value={instance.id.toString()}>
                      {instance.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="blocklist-infohash">Infohash</Label>
              <Input
                id="blocklist-infohash"
                placeholder="e.g. 63e07ff523710ca268567dad344ce1e0e6b7e8a3"
                value={infoHash}
                onChange={(event) => setInfoHash(event.target.value)}
              />
            </div>
          </div>
          <div className="grid gap-4 md:grid-cols-[1fr_auto]">
            <div className="space-y-2">
              <Label htmlFor="blocklist-note">Note (optional)</Label>
              <Input
                id="blocklist-note"
                placeholder="Why is this hash blocked?"
                value={note}
                onChange={(event) => setNote(event.target.value)}
              />
            </div>
            <div className="flex items-end">
              <Button
                onClick={handleAdd}
                disabled={addMutation.isPending || infoHash.trim() === ""}
              >
                {addMutation.isPending ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Plus className="mr-2 h-4 w-4" />
                )}
                Add
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Blocked Hashes</CardTitle>
          <CardDescription>
            Entries are applied only to the selected instance.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          ) : blocklist.length === 0 ? (
            <div className="text-sm text-muted-foreground">No blocked infohashes.</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Infohash</TableHead>
                  <TableHead>Note</TableHead>
                  <TableHead>Added</TableHead>
                  <TableHead className="w-[80px]"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {blocklist.map((entry) => {
                  const isDeleting = deleteMutation.isPending
                    && deleteMutation.variables?.instanceId === entry.instanceId
                    && deleteMutation.variables?.infoHash === entry.infoHash
                  return (
                    <TableRow key={`${entry.instanceId}-${entry.infoHash}`}>
                      <TableCell className="font-mono text-xs break-all">
                        {entry.infoHash}
                      </TableCell>
                      <TableCell>{entry.note || "—"}</TableCell>
                      <TableCell>{formatDateValue(entry.createdAt)}</TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => deleteMutation.mutate(entry)}
                          disabled={isDeleting}
                          aria-label={`Remove ${entry.infoHash} from blocklist`}
                        >
                          {isDeleting ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
