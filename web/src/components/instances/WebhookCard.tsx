/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Button } from "@/components/ui/button"
import { useWebhooks } from "@/hooks/useWebhooks"
import type { WebhookPreferences } from "@/types"
import { toast } from "sonner"
import { Save } from "lucide-react"
import { Badge } from "@/components/ui/badge"

interface WebhookCardProps {
  webhook: WebhookPreferences
}

export function WebhookCard({ webhook }: WebhookCardProps) {
  const { updateWebhook, isUpdating } = useWebhooks()
  
  const [formData, setFormData] = useState({
    enabled: webhook.enabled,
    autorun_enabled: webhook.autorun_enabled,
    autorun_on_torrent_added_enabled: webhook.autorun_on_torrent_added_enabled,
    qui_url: webhook.qui_url,
  })

  // Update form data when webhook prop changes
  useEffect(() => {
    setFormData({
      enabled: webhook.enabled,
      autorun_enabled: webhook.autorun_enabled,
      autorun_on_torrent_added_enabled: webhook.autorun_on_torrent_added_enabled,
      qui_url: webhook.qui_url,
    })
  }, [webhook])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    
    updateWebhook({
      instanceId: parseInt(webhook.instance_id),
      preferences: {
        enabled: formData.enabled,
        autorun_enabled: formData.autorun_enabled,
        autorun_on_torrent_added_enabled: formData.autorun_on_torrent_added_enabled,
        qui_url: formData.qui_url,
        instance_id: webhook.instance_id,
        api_key_id: webhook.api_key_id,
      },
    }, {
      onSuccess: () => {
        toast.success("Webhook settings updated", {
          description: `Successfully updated webhook for "${webhook.instance_name}"`,
        })
      },
      onError: (error) => {
        toast.error("Update failed", {
          description: error instanceof Error ? error.message : "Failed to update webhook settings",
        })
      },
    })
  }

  const handleToggleEnable = (checked: boolean) => {
    setFormData({ ...formData, enabled: checked })
  }

  const hasChanges = 
    formData.enabled !== webhook.enabled ||
    formData.autorun_enabled !== webhook.autorun_enabled ||
    formData.autorun_on_torrent_added_enabled !== webhook.autorun_on_torrent_added_enabled ||
    formData.qui_url !== webhook.qui_url

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <div className="flex-1">
            <CardTitle className="text-lg font-medium">{webhook.instance_name}</CardTitle>
          </div>
          <Badge variant={formData.enabled ? "default" : "secondary"}>
            {formData.enabled ? "Enabled" : "Disabled"}
          </Badge>
        </div>
      </CardHeader>
      
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Enable Webhook */}
          <div className="flex items-center justify-between space-x-2">
            <Label htmlFor={`enabled-${webhook.instance_id}`} className="flex-1">
              Enable
            </Label>
            <Switch
              id={`enabled-${webhook.instance_id}`}
              checked={formData.enabled}
              onCheckedChange={handleToggleEnable}
            />
          </div>

          {/* Autorun Settings */}
          <div className="space-y-3 pt-2">
            

            <div className="flex items-center space-x-2">
              <Label htmlFor={`autorun-${webhook.instance_id}`} className="font-normal">
                Enable Autorun
              </Label>
              <Switch
                id={`autorun-${webhook.instance_id}`}
                checked={formData.autorun_enabled}
                onCheckedChange={(checked) => setFormData({ ...formData, autorun_enabled: checked })}
                disabled={!formData.enabled}
              />
            </div>

            <div className="flex items-center space-x-2">
              <Label htmlFor={`autorun-added-${webhook.instance_id}`} className="font-normal">
                Autorun on Torrent Added
              </Label>
              <Switch
                id={`autorun-added-${webhook.instance_id}`}
                checked={formData.autorun_on_torrent_added_enabled}
                onCheckedChange={(checked) => setFormData({ ...formData, autorun_on_torrent_added_enabled: checked })}
                disabled={!formData.enabled}
              />
            </div>
          </div>

          {/* qui URL */}
          <div className="space-y-2">
            <Label htmlFor={`qui-url-${webhook.instance_id}`}>
              qui URL
              <span className="text-muted-foreground text-xs ml-2">(Should be reachable by your qBittorrent instance)</span>
            </Label>
            <Input
              id={`qui-url-${webhook.instance_id}`}
              type="url"
              value={formData.qui_url}
              onChange={(e) => setFormData({ ...formData, qui_url: e.target.value })}
              placeholder="https://qui.example.com"
              disabled={!formData.enabled}
            />
          </div>

          {/* Save Button */}
          <div className="flex justify-end pt-2">
            <Button
              type="submit"
              disabled={isUpdating || !hasChanges}
              className="w-full sm:w-auto"
            >
              <Save className="mr-2 h-4 w-4" />
              {isUpdating ? "Saving..." : "Save Changes"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

