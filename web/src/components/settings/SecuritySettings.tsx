/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation } from "@tanstack/react-query"
import { useRef, useState, type ChangeEvent } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Field } from "@/components/kit/Field"
import { Section } from "@/components/kit/Section"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/hooks/useAuth"
import { api } from "@/lib/api"
import { canRegisterProtocolHandler, getMagnetHandlerRegistrationGuidance, registerMagnetHandler } from "@/lib/protocol-handler"

export function SecuritySettings() {
  return (
    <>
      <ChangePasswordSection />
      {canRegisterProtocolHandler() && <BrowserIntegrationSection />}
    </>
  )
}

const EMPTY = { currentPassword: "", newPassword: "", confirmPassword: "" }
type PasswordFields = typeof EMPTY

function ChangePasswordSection() {
  const { t } = useTranslation("settings")
  const { user, setIsAuthenticated } = useAuth()
  const [values, setValues] = useState(EMPTY)
  const [errors, setErrors] = useState<Partial<PasswordFields>>({})
  const inputs = useRef<Partial<Record<keyof PasswordFields, HTMLInputElement | null>>>({})

  const mutation = useMutation({
    mutationFn: () => api.changePassword(values.currentPassword, values.newPassword),
    onSuccess: () => {
      toast.success(t("changePassword.toasts.success"))
      setIsAuthenticated(false)
    },
    onError: () => toast.error(t("changePassword.toasts.error")),
  })

  const validate = (): Partial<PasswordFields> => ({
    currentPassword: values.currentPassword ? undefined : t("changePassword.validation.currentRequired"),
    newPassword: !values.newPassword
      ? t("changePassword.validation.newRequired")
      : values.newPassword.length < 8 ? t("changePassword.validation.minLength") : undefined,
    confirmPassword: !values.confirmPassword
      ? t("changePassword.validation.confirmRequired")
      : values.confirmPassword !== values.newPassword ? t("changePassword.validation.mismatch") : undefined,
  })

  const submit = () => {
    const next = validate()
    setErrors(next)
    const firstInvalid = (Object.keys(EMPTY) as (keyof PasswordFields)[]).find((name) => next[name])
    if (firstInvalid) {
      inputs.current[firstInvalid]?.focus()
      return
    }
    mutation.mutate()
  }

  const field = (name: keyof PasswordFields, autoComplete: "current-password" | "new-password") => ({
    name,
    ref: (input: HTMLInputElement | null) => {
      inputs.current[name] = input
    },
    type: "password",
    autoComplete,
    value: values[name],
    onChange: (event: ChangeEvent<HTMLInputElement>) => {
      setValues((prev) => ({ ...prev, [name]: event.target.value }))
      setErrors((prev) => ({ ...prev, [name]: undefined }))
    },
  })

  return (
    <Section
      title={t("security.changePassword.title")}
      description={t("security.changePassword.description")}
      submit={{ label: t("changePassword.submit"), onSubmit: submit, pending: mutation.isPending }}
    >
      <input type="text" name="username" autoComplete="username" value={user?.username ?? ""} readOnly hidden />
      <div className="grid gap-6 sm:grid-cols-2">
        <Field label={t("changePassword.currentPassword")} error={errors.currentPassword}>
          <Input {...field("currentPassword", "current-password")} />
        </Field>
        <div aria-hidden="true" className="max-sm:hidden" />
        <Field label={t("changePassword.newPassword")} description={t("changePassword.newPasswordHint")} error={errors.newPassword}>
          <Input {...field("newPassword", "new-password")} />
        </Field>
        <Field label={t("changePassword.confirmPassword")} error={errors.confirmPassword}>
          <Input {...field("confirmPassword", "new-password")} />
        </Field>
      </div>
    </Section>
  )
}

function BrowserIntegrationSection() {
  const { t } = useTranslation("settings")

  const register = () => {
    if (registerMagnetHandler()) {
      toast.success(t("security.browserIntegration.toasts.requested"), { description: getMagnetHandlerRegistrationGuidance() })
    } else {
      toast.error(t("security.browserIntegration.toasts.failed"))
    }
  }

  return (
    <Section
      title={t("security.browserIntegration.title")}
      description={t("security.browserIntegration.registerDescription")}
      actions={<Button variant="outline" size="sm" onClick={register}>{t("security.browserIntegration.registerButton")}</Button>}
    />
  )
}
