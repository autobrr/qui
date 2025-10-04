/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useWebhooks } from "@/hooks/useWebhooks"
import { WebhookCard } from "@/components/instances/WebhookCard"
import { Link } from "@tanstack/react-router"
import { Button } from "@/components/ui/button"
import { Plus, AlertCircle, RefreshCw } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

export function Webhooks() {
  const { webhooks, isLoading, error } = useWebhooks()

  if (isLoading) {
    return (
      <div className="container mx-auto p-6">
        <div className="flex items-center justify-center gap-2 text-muted-foreground">
          <RefreshCw className="h-5 w-5 animate-spin" />
          <span>Loading webhooks...</span>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="container mx-auto p-6 max-w-7xl">
        <div className="mb-6">
          <h1 className="text-3xl font-bold">Webhooks</h1>
          <p className="text-muted-foreground mt-2">
            Configure webhook settings for your qBittorrent instances
          </p>
        </div>
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error loading webhooks</AlertTitle>
          <AlertDescription>
            {error instanceof Error ? error.message : "Failed to load webhook settings"}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="container mx-auto p-6 max-w-7xl">
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Webhooks</h1>
          <p className="text-muted-foreground mt-2">
            Configure webhook settings for your qBittorrent instances
          </p>
        </div>
        <Link to="/instances" search={{ modal: "add-instance" }}>
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Add Instance
          </Button>
        </Link>
      </div>

      {webhooks && webhooks.length > 0 ? (
        <div className="grid gap-6 md:grid-cols-2">
          {webhooks.map((webhook) => (
            <WebhookCard
              key={webhook.instance_id}
              webhook={webhook}
            />
          ))}
        </div>
      ) : (
        <div className="rounded-lg border border-dashed p-12 text-center">
          <p className="text-muted-foreground mb-2">No instances configured</p>
          <p className="text-sm text-muted-foreground mb-4">
            Add an instance first to configure webhooks
          </p>
          <Link to="/instances" search={{ modal: "add-instance" }}>
            <Button variant="outline">
              <Plus className="mr-2 h-4 w-4" />
              Add your first instance
            </Button>
          </Link>
        </div>
      )}
    </div>
  )
}