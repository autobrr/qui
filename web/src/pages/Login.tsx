/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Footer } from "@/components/Footer"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Logo } from "@/components/ui/Logo"
import { useAuth } from "@/hooks/useAuth"
import { api } from "@/lib/api"
import { useForm } from "@tanstack/react-form"
import { useNavigate } from "@tanstack/react-router"
import { useEffect } from "react"

export function Login() {
  const navigate = useNavigate()
  const { login, isLoggingIn, loginError } = useAuth()

  useEffect(() => {
    // Check if setup is needed
    api.checkSetupRequired().then(setupRequired => {
      if (setupRequired) {
        navigate({ to: "/setup" })
      }
    })
  }, [navigate])

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
    },
    onSubmit: async ({ value }) => {
      login(value)
    },
  })

  return (
    <div className="flex h-screen items-center justify-center bg-background px-4 sm:px-6">
      <Card className="w-full max-w-md shadow-xl">
        <CardHeader className="text-center">
          <div className="flex items-center justify-center mb-2">
            <Logo className="h-12 w-12" />
          </div>
          <CardTitle className="text-3xl font-bold pointer-events-none select-none">
            qui
          </CardTitle>
          <CardDescription className="pointer-events-none select-none">
            qBittorrent management interface
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
                onChange: ({ value }) => (!value ? "Username is required" : undefined),
              }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor={field.name}>Username</Label>
                  <Input
                    id={field.name}
                    type="text"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="Enter your username"
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
                onChange: ({ value }) => (!value ? "Password is required" : undefined),
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
                    placeholder="Enter your password"
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            {loginError && (
              <div className="bg-destructive/10 border border-destructive/20 text-destructive px-4 py-3 rounded-md text-sm">
                {typeof loginError === "string"? loginError: loginError.message?.includes("Invalid credentials") || loginError.message?.includes("401")? "Invalid username or password": loginError.message || "Login failed. Please try again."}
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
                  disabled={!canSubmit || isSubmitting || isLoggingIn}
                >
                  {isLoggingIn ? "Logging in..." : "Sign in"}
                </Button>
              )}
            </form.Subscribe>
          </form>
          <Footer />
        </CardContent>
      </Card>
    </div>
  )
}