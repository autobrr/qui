/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { useState } from 'react'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { Instance } from '@/types'
import { api } from '@/lib/api'

interface InstanceFormData {
  name: string
  host: string
  port: number
  username: string
  password: string
  basicUsername?: string
  basicPassword?: string
}

interface InstanceFormProps {
  instance?: Instance
  onSuccess: () => void
  onCancel: () => void
}

export function InstanceForm({ instance, onSuccess, onCancel }: InstanceFormProps) {
  const queryClient = useQueryClient()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  
  const mutation = useMutation({
    mutationFn: (data: InstanceFormData) => 
      instance ? api.updateInstance(instance.id, data) : api.createInstance(data),
    onSuccess: () => {
      // Invalidate instances query to refresh the list
      queryClient.invalidateQueries({ queryKey: ['instances'] })
      onSuccess()
    },
  })

  const form = useForm({
    defaultValues: {
      name: instance?.name ?? '',
      host: instance?.host ?? 'http://localhost',
      port: instance?.port ?? 8080,
      username: instance?.username ?? '',
      password: '',
      basicUsername: instance?.basicUsername ?? '',
      basicPassword: '',
    },
    onSubmit: async ({ value }) => {
      // Clear basic auth fields if toggle is off
      const submitData = showBasicAuth ? value : {
        ...value,
        basicUsername: undefined,
        basicPassword: undefined,
      }
      await mutation.mutateAsync(submitData)
    },
  })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-4"
    >
      <form.Field
        name="name"
        validators={{
          onChange: ({ value }) => 
            !value ? 'Instance name is required' : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Instance Name</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="My qBittorrent"
              data-1p-ignore
              autoComplete='off'
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="host"
        validators={{
          onChange: ({ value }) => {
            if (!value) return 'Host is required'
            if (!value.match(/^https?:\/\//)) return 'Host must start with http:// or https://'
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Host URL</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="http://localhost"
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="port"
        validators={{
          onChange: ({ value }) => {
            if (!value || value < 1 || value > 65535) {
              return 'Port must be between 1 and 65535'
            }
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Port</Label>
            <Input
              id={field.name}
              type="number"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field name="username">
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Username</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="admin"
              data-1p-ignore
              autoComplete='off'
            />
          </div>
        )}
      </form.Field>

      <form.Field
        name="password"
        validators={{
          onChange: ({ value }) => 
            !instance && !value ? 'Password is required for new instances' : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>Password</Label>
            <Input
              id={field.name}
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder={instance ? 'Leave empty to keep current password' : 'Enter password'}
              data-1p-ignore
              autoComplete='off'
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="space-y-0.5">
            <Label htmlFor="basic-auth-toggle">HTTP Basic Authentication</Label>
            <p className="text-sm text-muted-foreground">
              Enable if your qBittorrent is behind a reverse proxy with Basic Auth
            </p>
          </div>
          <Switch
            id="basic-auth-toggle"
            checked={showBasicAuth}
            onCheckedChange={setShowBasicAuth}
          />
        </div>

        {showBasicAuth && (
          <div className="space-y-4 pl-6 border-l-2 border-muted">
            <form.Field name="basicUsername">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor={field.name}>Basic Auth Username</Label>
                  <Input
                    id={field.name}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="Basic auth username"
                    data-1p-ignore
                    autoComplete='off'
                  />
                </div>
              )}
            </form.Field>

            <form.Field name="basicPassword">
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor={field.name}>Basic Auth Password</Label>
                  <Input
                    id={field.name}
                    type="password"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={instance?.basicUsername ? 'Leave empty to keep current password' : 'Enter basic auth password'}
                    data-1p-ignore
                    autoComplete='off'
                  />
                </div>
              )}
            </form.Field>
          </div>
        )}
      </div>

      {mutation.error && (
        <p className="text-sm text-destructive">
          {mutation.error.message || 'Failed to save instance'}
        </p>
      )}

      <div className="flex gap-2">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button 
              type="submit" 
              disabled={!canSubmit || isSubmitting || mutation.isPending}
            >
              {mutation.isPending ? 'Saving...' : instance ? 'Update Instance' : 'Add Instance'}
            </Button>
          )}
        </form.Subscribe>
        
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
    </form>
  )
}