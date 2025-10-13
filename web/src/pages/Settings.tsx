/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ClientApiKeysManager } from "@/components/settings/ClientApiKeysManager"
import { DateTimePreferencesForm } from "@/components/settings/DateTimePreferencesForm"
import { LicenseManager } from "@/components/themes/LicenseManager.tsx"
import { ThemeSelector } from "@/components/themes/ThemeSelector"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
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
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api } from "@/lib/api"
import { withBasePath } from "@/lib/base-url"
import { copyTextToClipboard } from "@/lib/utils"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useSearch } from "@tanstack/react-router"
import { Clock, Copy, ExternalLink, Key, Palette, Plus, Server, Shield, Trash2 } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge";

const settingsTabs = ["security", "themes", "api", "datetime", "client-api"] as const
type SettingsTab = (typeof settingsTabs)[number]

const isSettingsTab = (value: unknown): value is SettingsTab => {
  return typeof value === "string" && settingsTabs.some((tab) => tab === value)
}

function ChangePasswordForm() {
  const { t } = useTranslation()
  const mutation = useMutation({
    mutationFn: async (data: { currentPassword: string; newPassword: string }) => {
      return api.changePassword(data.currentPassword, data.newPassword)
    },
    onSuccess: () => {
      toast.success(t("settings.security.notifications.success"))
      form.reset()
    },
    onError: () => {
      toast.error(t("settings.security.notifications.error"))
    },
  })

  const form = useForm({
    defaultValues: {
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        currentPassword: value.currentPassword,
        newPassword: value.newPassword,
      })
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
        name="currentPassword"
        validators={{
          onChange: ({ value }) => !value ? t("settings.security.currentPassword.required") : undefined,
        }}
      >
        {(field) => (
          <div className="space-y-2">
                                  <Label htmlFor="currentPassword">{t("settings.security.currentPassword.label")}</Label>            <Input
              id="currentPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="newPassword"
        validators={{
          onChange: ({ value }) => {
            if (!value) return t("settings.security.newPassword.required")
            if (value.length < 8) return t("settings.security.newPassword.minLength")
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="newPassword">{t("settings.security.newPassword.label")}</Label>
            <Input
              id="newPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Field
        name="confirmPassword"
        validators={{
          onChange: ({ value, fieldApi }) => {
            const newPassword = fieldApi.form.getFieldValue("newPassword")
            if (!value) return t("settings.security.confirmNewPassword.required")
            if (value !== newPassword) return t("settings.security.confirmNewPassword.noMatch")
            return undefined
          },
        }}
      >
        {(field) => (
          <div className="space-y-2">
            <Label htmlFor="confirmPassword">{t("settings.security.confirmNewPassword.label")}</Label>
            <Input
              id="confirmPassword"
              type="password"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
            />
            {field.state.meta.isTouched && field.state.meta.errors[0] && (
              <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
            )}
          </div>
        )}
      </form.Field>

      <form.Subscribe
        selector={(state) => [state.canSubmit, state.isSubmitting]}
      >
        {([canSubmit, isSubmitting]) => (
          <Button
            type="submit"
            disabled={!canSubmit || isSubmitting || mutation.isPending}
          >
            {isSubmitting || mutation.isPending ? t("settings.security.button.changing") : t("settings.security.button.change")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}

function ApiKeysManager() {
  const { t } = useTranslation()
  const [showCreateDialog, setShowCreateDialog] = useState(false)
  const [deleteKeyId, setDeleteKeyId] = useState<number | null>(null)
  const [newKey, setNewKey] = useState<{ name: string; key: string } | null>(null)
  const queryClient = useQueryClient()
  const { formatDate } = useDateTimeFormatters()

  // Fetch API keys from backend
  const { data: apiKeys, isLoading } = useQuery({
    queryKey: ["apiKeys"],
    queryFn: () => api.getApiKeys(),
    staleTime: 30 * 1000, // 30 seconds
  })

  // Ensure apiKeys is always an array
  const keys = apiKeys || []

  const createMutation = useMutation({
    mutationFn: async (name: string) => {
      return api.createApiKey(name)
    },
    onSuccess: (data) => {
      setNewKey(data)
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      toast.success(t("settings.apiKeys.notifications.createSuccess"))
    },
    onError: () => {
      toast.error(t("settings.apiKeys.notifications.createError"))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      return api.deleteApiKey(id)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apiKeys"] })
      setDeleteKeyId(null)
      toast.success(t("settings.apiKeys.notifications.deleteSuccess"))
    },
    onError: () => {
      toast.error(t("settings.apiKeys.notifications.deleteError"))
    },
  })

  const form = useForm({
    defaultValues: {
      name: "",
    },
    onSubmit: async ({ value }) => {
      await createMutation.mutateAsync(value.name)
      form.reset()
    },
  })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">
          {t("settings.apiKeys.mainDescription")}
        </p>
        <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 h-4 w-4" />
              {t("settings.apiKeys.create")}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("settings.apiKeys.createDialog.title")}</DialogTitle>
              <DialogDescription>
                {t("settings.apiKeys.createDialog.description")}
              </DialogDescription>
            </DialogHeader>

            {newKey ? (
              <div className="space-y-4">
                <div>
                  <Label>{t("settings.apiKeys.createDialog.newKeyLabel")}</Label>
                  <div className="mt-2 flex items-center gap-2">
                    <code className="flex-1 rounded bg-muted px-2 py-1 text-sm font-mono break-all">
                      {newKey.key}
                    </code>
                    <Button
                      size="icon"
                      variant="outline"
                      onClick={async () => {
                        try {
                          await copyTextToClipboard(newKey.key)
                          toast.success(t("settings.apiKeys.notifications.copySuccess"))
                        } catch {
                          toast.error(t("settings.apiKeys.notifications.copyError"))
                        }
                      }}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                  <p className="mt-2 text-sm text-destructive">
                    {t("settings.apiKeys.createDialog.saveWarning")}
                  </p>
                </div>
                <Button
                  onClick={() => {
                    setNewKey(null)
                    setShowCreateDialog(false)
                  }}
                  className="w-full"
                >
                                        {t("common.buttons.done")}                </Button>
              </div>
            ) : (
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
                    onChange: ({ value }) => !value ? t("settings.apiKeys.createDialog.nameRequired") : undefined,
                  }}
                >
                  {(field) => (
                    <div className="space-y-2">
                      <Label htmlFor="name">{t("common.name")}</Label>
                      <Input
                        id="name"
                        placeholder={t("settings.apiKeys.createDialog.namePlaceholder")}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        data-1p-ignore
                        autoComplete='off'
                      />
                      {field.state.meta.isTouched && field.state.meta.errors[0] && (
                        <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                      )}
                    </div>
                  )}
                </form.Field>

                <form.Subscribe
                  selector={(state) => [state.canSubmit, state.isSubmitting]}
                >
                  {([canSubmit, isSubmitting]) => (
                    <Button
                      type="submit"
                      disabled={!canSubmit || isSubmitting || createMutation.isPending}
                      className="w-full"
                    >
                      {isSubmitting || createMutation.isPending ? t("settings.apiKeys.createDialog.creating") : t("settings.apiKeys.create")}
                    </Button>
                  )}
                </form.Subscribe>
              </form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      <div className="space-y-2">
        {isLoading ? (
          <p className="text-center text-sm text-muted-foreground py-8">
            {t("settings.apiKeys.loading")}
          </p>
        ) : (
          <>
            {keys.map((key) => (
              <div
                key={key.id}
                className="flex items-center justify-between rounded-lg border p-4"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{key.name}</span>
                    <Badge variant="outline" className="text-xs">
                      {t("settings.apiKeys.id", { id: key.id })}
                    </Badge>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    {t("settings.apiKeys.created", { date: formatDate(new Date(key.createdAt)) })}
                    {key.lastUsedAt && (
                      <> • {t("settings.apiKeys.lastUsed", { date: formatDate(new Date(key.lastUsedAt)) })}</>
                    )}
                  </p>
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  onClick={() => setDeleteKeyId(key.id)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}

            {keys.length === 0 && (
              <p className="text-center text-sm text-muted-foreground py-8">
                {t("settings.apiKeys.empty")}
              </p>
            )}
          </>
        )}
      </div>

      <AlertDialog open={!!deleteKeyId} onOpenChange={() => setDeleteKeyId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("settings.apiKeys.deleteDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("settings.apiKeys.deleteDialog.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
                          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>            <AlertDialogAction
              onClick={() => deleteKeyId && deleteMutation.mutate(deleteKeyId)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("common.buttons.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

export function Settings() {
  const { t } = useTranslation()
  const tabFromSearch = useSearch({
    from: "/_authenticated/settings",
    select: (search: Record<string, unknown>): SettingsTab | undefined => {
      const value = search.tab
      return isSettingsTab(value) ? value : undefined
    },
  })
  const defaultTab: SettingsTab = tabFromSearch ?? "security"
  const [activeTab, setActiveTab] = useState<SettingsTab>(defaultTab)

  return (
    <div className="container mx-auto p-4 md:p-6">
      <div className="mb-4 md:mb-6">
        <h1 className="text-2xl md:text-3xl font-bold">{t("common.titles.settings")}</h1>
        <p className="text-muted-foreground mt-1 md:mt-2 text-sm md:text-base">
          {t("settings.description")}
        </p>
      </div>

      {/* Mobile Dropdown Navigation */}
      <div className="md:hidden mb-4">
        <Select
          value={activeTab}
          onValueChange={(value) => {
            if (isSettingsTab(value)) {
              setActiveTab(value)
            }
          }}
        >
          <SelectTrigger className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="security">
              <div className="flex items-center">
                <Shield className="w-4 h-4 mr-2" />
                {t("settings.tabs.security")}
              </div>
            </SelectItem>
            <SelectItem value="themes">
              <div className="flex items-center">
                <Palette className="w-4 h-4 mr-2" />
                {t("settings.tabs.themes")}
              </div>
            </SelectItem>
            <SelectItem value="api">
              <div className="flex items-center">
                <Key className="w-4 h-4 mr-2" />
                {t("settings.tabs.api")}
              </div>
            </SelectItem>
            <SelectItem value="datetime">
              <div className="flex items-center">
                <Clock className="w-4 h-4 mr-2" />
                {t("settings.tabs.datetime")}
              </div>
            </SelectItem>
            <SelectItem value="client-api">
              <div className="flex items-center">
                <Server className="w-4 h-4 mr-2" />
                {t("settings.tabs.clientApi")}
              </div>
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="flex gap-6">
        {/* Desktop Sidebar Navigation */}
        <div className="hidden md:block w-64 shrink-0">
          <nav className="space-y-1">
            <button
              onClick={() => setActiveTab("security")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "security"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Shield className="w-4 h-4 mr-2" />
              {t("settings.tabs.security")}
            </button>
            <button
              onClick={() => setActiveTab("themes")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "themes"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Palette className="w-4 h-4 mr-2" />
              {t("settings.tabs.themes")}
            </button>
            <button
              onClick={() => setActiveTab("api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "api"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Key className="w-4 h-4 mr-2" />
              {t("settings.tabs.api")}
            </button>
            <button
              onClick={() => setActiveTab("datetime")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "datetime"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Clock className="w-4 h-4 mr-2" />
              {t("settings.tabs.datetime")}
            </button>
            <button
              onClick={() => setActiveTab("client-api")}
              className={`w-full flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                activeTab === "client-api"? "bg-accent text-accent-foreground": "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
              }`}
            >
              <Server className="w-4 h-4 mr-2" />
              {t("settings.tabs.clientApi")}
            </button>
          </nav>
        </div>

        {/* Main Content Area */}
        <div className="flex-1 min-w-0">

          {activeTab === "security" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>{t("settings.security.title")}</CardTitle>
                  <CardDescription>
                    {t("settings.security.description")}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ChangePasswordForm />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "datetime" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>{t("settings.dateTime.title")}</CardTitle>
                  <CardDescription>
                    {t("settings.dateTime.description")}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <DateTimePreferencesForm />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "themes" && (
            <div className="space-y-4">
              <LicenseManager />
              <ThemeSelector />
            </div>
          )}

          {activeTab === "api" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <div className="flex items-start justify-between">
                    <div className="space-y-1.5">
                      <CardTitle>{t("settings.apiKeys.title")}</CardTitle>
                      <CardDescription>
                        {t("settings.apiKeys.description")}
                      </CardDescription>
                    </div>
                    <a
                      href={withBasePath("api/docs")}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
                      title={t("settings.apiKeys.docsTitle")}
                    >
                      <span className="hidden sm:inline">{t("settings.apiKeys.docs")}</span>
                      <ExternalLink className="h-3.5 w-3.5" />
                    </a>
                  </div>
                </CardHeader>
                <CardContent>
                  <ApiKeysManager />
                </CardContent>
              </Card>
            </div>
          )}

          {activeTab === "client-api" && (
            <div className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>{t("settings.clientApiKeys.title")}</CardTitle>
                  <CardDescription>
                    {t("settings.clientApiKeys.description")}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <ClientApiKeysManager />
                </CardContent>
              </Card>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}