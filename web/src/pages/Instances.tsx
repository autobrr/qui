/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { useInstances } from "@/hooks/useInstances"
import { InstanceCard } from "@/components/instances/InstanceCard"
import { InstanceForm } from "@/components/instances/InstanceForm"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Plus } from "lucide-react"
import { useNavigate, useSearch } from "@tanstack/react-router"
import type { Instance } from "@/types"

export function Instances() {
  const { instances, isLoading } = useInstances()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const [editingInstance, setEditingInstance] = useState<Instance | undefined>()

  // Check if modal should be open based on URL params
  const isDialogOpen = search?.modal === "add-instance"

  const handleOpenDialog = (instance?: Instance) => {
    setEditingInstance(instance)
    navigate({
      to: "/instances",
      search: { modal: "add-instance" },
      replace: true,
    })
  }

  const handleCloseDialog = () => {
    setEditingInstance(undefined)
    navigate({
      to: "/instances",
      search: {},
      replace: true,
    })
  }

  // Open modal if URL has the parameter on mount
  useEffect(() => {
    if (search?.modal === "add-instance" && !editingInstance) {
      // Dialog is already open due to URL, no need to set additional state
    }
  }, [search?.modal, editingInstance])

  if (isLoading) {
    return <div className="p-6">Loading instances...</div>
  }

  return (
    <div className="container mx-auto p-6">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Instances</h1>
          <p className="text-muted-foreground mt-2">
            Manage your qBittorrent connection settings
          </p>
        </div>
        <Button onClick={() => handleOpenDialog()}>
          <Plus className="mr-2 h-4 w-4" />
          Add Instance
        </Button>
      </div>

      {/* Show banner if any instances have decryption errors */}
      <PasswordIssuesBanner instances={instances || []} />

      {instances && instances.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {instances.map((instance) => (
            <InstanceCard
              key={instance.id}
              instance={instance}
              onEdit={() => handleOpenDialog(instance)}
            />
          ))}
        </div>
      ) : (
        <div className="rounded-lg border border-dashed p-12 text-center">
          <p className="text-muted-foreground">No instances configured</p>
          <Button
            onClick={() => handleOpenDialog()}
            className="mt-4"
            variant="outline"
          >
            <Plus className="mr-2 h-4 w-4" />
            Add your first instance
          </Button>
        </div>
      )}

      <Dialog open={isDialogOpen} onOpenChange={(open) => open ? handleOpenDialog() : handleCloseDialog()}>
        <DialogContent className="sm:max-w-[425px]">
          <DialogHeader>
            <DialogTitle>
              {editingInstance ? "Edit Instance" : "Add Instance"}
            </DialogTitle>
            <DialogDescription>
              {editingInstance? "Update your qBittorrent instance configuration": "Add a new qBittorrent instance to manage"
              }
            </DialogDescription>
          </DialogHeader>
          <InstanceForm
            instance={editingInstance}
            onSuccess={handleCloseDialog}
            onCancel={handleCloseDialog}
          />
        </DialogContent>
      </Dialog>
    </div>
  )
}