/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Footer } from "@/components/Footer"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Logo } from "@/components/ui/Logo"
import { ShineBorder } from "@/components/magicui/shine-border"
import { BlurFade } from "@/components/magicui/blur-fade"
import { useAuth } from "@/hooks/useAuth"
import { api } from "@/lib/api"
import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { useEffect } from "react"
import { useTranslation } from "react-i18next"

export function Setup() {
  const { t } = useTranslation(["auth", "footer"])
  const navigate = useNavigate()
  const { setup, isSettingUp, setupError } = useAuth()

  const { data: oidcConfig } = useQuery({
    queryKey: ["oidc-config"],
    queryFn: () => api.getOIDCConfig(),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  })

  useEffect(() => {
    if (oidcConfig === undefined) {
      return
    }

    if (oidcConfig.enabled) {
      navigate({ to: "/login" })
      return
    }

    // Check if user already exists
    api.checkAuth().then(() => {
      navigate({ to: "/login" })
    }).catch(() => {
      // No user exists, stay on setup page
    })
  }, [navigate, oidcConfig])

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      confirmPassword: "",
    },
    onSubmit: async ({ value }) => {
      const { username, password } = value
      setup({ username, password })
    },
  })

  return (
    <div className="flex h-screen items-center justify-center bg-background px-4 sm:px-6">
      <BlurFade duration={0.5} delay={0.1} blur="10px" className="w-full max-w-md">
        <Card className="relative overflow-hidden w-full shadow-xl">
          <ShineBorder shineColor={["var(--chart-1)", "var(--chart-2)", "var(--chart-3)"]} />
          <CardHeader className="text-center">
            <div className="flex items-center justify-center mb-2">
              <Logo className="h-12 w-12" />
            </div>
            <CardTitle className="text-3xl font-bold pointer-events-none select-none">
              qui
            </CardTitle>
            <CardDescription className="pointer-events-none select-none">
              {t("auth:setup.subtitle")}
            </CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            <form
              onSubmit={(e) => {
                e.preventDefault()
                form.handleSubmit()
              }}
              className="space-y-4"
            >
              <form.Field
                name="username"
                validators={{
                  onChange: ({ value }) => {
                    if (!value) return t("auth:setup.errors.usernameRequired")
                    if (value.length < 3) return t("auth:setup.errors.usernameTooShort")
                    return undefined
                  },
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{t("auth:setup.usernameLabel")}</Label>
                    <Input
                      id={field.name}
                      type="text"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("auth:setup.usernamePlaceholder")}
                    />
                    {field.state.meta.isTouched && field.state.meta.errors[0] && (
                      <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                    )}
                  </div>
                )}
              </form.Field>

              <form.Field
                name="password"
                validators={{
                  onChange: ({ value }) => {
                    if (!value) return t("auth:setup.errors.passwordRequired")
                    if (value.length < 8) return t("auth:setup.errors.passwordTooShort")
                    return undefined
                  },
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{t("auth:setup.passwordLabel")}</Label>
                    <Input
                      id={field.name}
                      type="password"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("auth:setup.passwordPlaceholder")}
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
                    const password = fieldApi.form.getFieldValue("password")
                    if (!value) return t("auth:setup.errors.confirmPasswordRequired")
                    if (value !== password) return t("auth:setup.errors.passwordMismatch")
                    return undefined
                  },
                }}
              >
                {(field) => (
                  <div className="space-y-2">
                    <Label htmlFor={field.name}>{t("auth:setup.confirmPasswordLabel")}</Label>
                    <Input
                      id={field.name}
                      type="password"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder={t("auth:setup.confirmPasswordPlaceholder")}
                    />
                    {field.state.meta.isTouched && field.state.meta.errors[0] && (
                      <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                    )}
                  </div>
                )}
              </form.Field>

              {setupError && (
                  <div className="bg-destructive/10 border border-destructive/20 text-destructive px-4 py-3 rounded-md text-sm">
                  {setupError.message || t("auth:setup.errors.createUserFailed")}
                </div>
              )}

              <form.Subscribe
                selector={(state) => [state.canSubmit, state.isSubmitting]}
              >
                {([canSubmit, isSubmitting]) => (
                  <Button
                    type="submit"
                    className="w-full"
                    size="lg"
                    disabled={!canSubmit || isSubmitting || isSettingUp}
                  >
                    {isSettingUp ? t("auth:setup.creatingAccount") : t("auth:setup.createAccount")}
                  </Button>
                )}
              </form.Subscribe>
            </form>
            <Footer />
          </CardContent>
        </Card>
      </BlurFade>
    </div>
  )
}
