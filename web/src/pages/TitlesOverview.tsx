/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Titles } from "@/pages/Titles"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useInstances } from "@/hooks/useInstances"
import { HardDrive } from "lucide-react"
import { useState } from "react"

export function TitlesOverview() {
  const { instances, isLoading } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = useState<number | null>(null)

  if (isLoading) {
    return <div className="p-6">Loading instances...</div>
  }

  if (!instances || instances.length === 0) {
    return (
      <div className="container mx-auto p-6">
        <Card>
          <CardHeader>
            <CardTitle>Titles Dashboard</CardTitle>
            <CardDescription>
              Parse and filter your torrent titles with comprehensive metadata
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground">
              No qBittorrent instances configured. Please add an instance in Settings to use the Titles dashboard.
            </p>
          </CardContent>
        </Card>
      </div>
    )
  }

  if (selectedInstanceId === null) {
    return (
      <div className="container mx-auto p-6">
        <div className="mb-6">
          <h1 className="text-2xl font-bold">Titles Dashboard</h1>
          <p className="text-muted-foreground mt-2">
            Select a qBittorrent instance to view parsed torrent titles
          </p>
        </div>

        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {instances.map((instance) => (
            <Card key={instance.id} className="cursor-pointer hover:shadow-md transition-shadow">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <HardDrive className="h-5 w-5" />
                  {instance.name}
                </CardTitle>
                <CardDescription>
                  {instance.host}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between">
                  <span
                    className={`inline-flex items-center gap-1 text-sm ${
                      instance.connected ? "text-green-600" : "text-red-600"
                    }`}
                  >
                    <div
                      className={`h-2 w-2 rounded-full ${
                        instance.connected ? "bg-green-500" : "bg-red-500"
                      }`}
                    />
                    {instance.connected ? "Connected" : "Disconnected"}
                  </span>
                  <Button
                    onClick={() => setSelectedInstanceId(instance.id)}
                    disabled={!instance.connected}
                  >
                    View Titles
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  return <Titles instanceId={selectedInstanceId} instanceName={instances.find(i => i.id === selectedInstanceId)?.name || "Unknown"} />
}