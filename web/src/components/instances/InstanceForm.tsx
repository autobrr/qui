/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from 'react'
import { useForm } from '@tanstack/react-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import type { Instance } from '@/types'
import { useInstances } from '@/hooks/useInstances'
import { formatErrorMessage } from '@/lib/utils'

// URL validation schema
const urlSchema = z
  .string()
  .min(1, 'URL is required')
  .transform((value) => {
    return value.includes('://') ? value : `http://${value}`
  })
  .refine((url) => {
    try {
      new URL(url)
      return true
    } catch {
      return false
    }
  }, 'Please enter a valid URL')
  .refine((url) => {
    const parsed = new URL(url)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  }, 'Only HTTP and HTTPS protocols are supported')
  .refine((url) => {
    const parsed = new URL(url)
    const hostname = parsed.hostname

    const isIPv4 = /^(\d{1,3}\.){3}\d{1,3}$/.test(hostname)
    const isIPv6 = hostname.startsWith('[') && hostname.endsWith(']')

    if ((isIPv4 || isIPv6) && !parsed.port) {
      return false
    }
    
    return true
  }, 'Port is required when using an IP address (e.g., :8080)')

interface InstanceFormData {
  name: string
  host: string
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
  const { createInstance, updateInstance, isCreating, isUpdating } = useInstances()
  const [showBasicAuth, setShowBasicAuth] = useState(!!instance?.basicUsername)
  
  const handleSubmit = (data: InstanceFormData) => {
    const submitData = showBasicAuth ? data : {
      ...data,
      basicUsername: undefined,
      basicPassword: undefined,
    }
    
    if (instance) {
      updateInstance({ id: instance.id, data: submitData }, {
        onSuccess: (data) => {
          if (data.connected) {
            toast.success('Instance Updated', {
              description: 'Instance updated and connected successfully'
            })
          } else {
            toast.warning('Instance Updated with Connection Issue', {
              description: data.connectionError ? formatErrorMessage(data.connectionError) : 'Instance updated but could not connect'
            })
          }
          onSuccess()
        },
        onError: (error) => {
          toast.error('Update Failed', {
            description: error instanceof Error ? formatErrorMessage(error.message) : 'Failed to update instance'
          })
        },
      })
    } else {
      createInstance(submitData, {
        onSuccess: (data) => {
          if (data.connected) {
            toast.success('Instance Created', {
              description: 'Instance created and connected successfully'
            })
          } else {
            toast.warning('Instance Created with Connection Issue', {
              description: data.connectionError ? formatErrorMessage(data.connectionError) : 'Instance created but could not connect'
            })
          }
          onSuccess()
        },
        onError: (error) => {
          toast.error('Create Failed', {
            description: error instanceof Error ? formatErrorMessage(error.message) : 'Failed to create instance'
          })
        },
      })
    }
  }

  const form = useForm({
    defaultValues: {
      name: instance?.name ?? '',
      host: instance?.host ?? 'http://localhost:8080',
      username: instance?.username ?? 'admin',
      password: '',
      basicUsername: instance?.basicUsername ?? '',
      basicPassword: '',
    },
    onSubmit: ({ value }) => {
      handleSubmit(value)
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
            const result = urlSchema.safeParse(value)
            return result.success ? undefined : result.error.issues[0]?.message
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor={field.name}>URL</Label>
            <Input
              id={field.name}
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="http://localhost:8080 or 192.168.1.100:8080"
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


      <div className="flex gap-2">
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, isSubmitting]) => (
            <Button 
              type="submit" 
              disabled={!canSubmit || isSubmitting || isCreating || isUpdating}
            >
              {(isCreating || isUpdating) ? 'Saving...' : instance ? 'Update Instance' : 'Add Instance'}
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