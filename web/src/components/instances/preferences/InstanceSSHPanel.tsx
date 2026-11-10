/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { CopyButton } from "@/components/ui/copy-button"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { RemoteAccessIndicator } from "@/components/instances/RemoteAccessIndicator"
import { useSSHSetup } from "@/hooks/useSSHSetup"
import { formatErrorMessage } from "@/lib/format-error-message"
import { useIncognitoMode } from "@/lib/incognito"
import type { Instance, SSHProbe, SSHTestResponse } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useQueryClient } from "@tanstack/react-query"
import { Check, X } from "lucide-react"
import { useCallback, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PreferencesFormShell } from "./PreferencesFormShell"

interface InstanceSSHPanelProps {
  instance: Instance
}

function sshDefaults(instance: Instance) {
  return {
    host: instance.sshHost ?? "",
    port: instance.sshPort ?? 22,
    username: instance.sshUsername ?? "",
    privateKey: "",
  }
}

export function InstanceSSHPanel({ instance }: InstanceSSHPanelProps) {
  // The setup starts over from the row whenever the stored endpoint or user
  // changes: a save that the server normalized, a clear, or a change from
  // another client. A test result answers for the endpoint it ran against.
  return (
    <SSHCredentialsForm
      key={`${instance.id}:${instance.sshHost ?? ""}:${instance.sshPort ?? ""}:${instance.sshUsername ?? ""}`}
      instance={instance}
    />
  )
}

function SSHCredentialsForm({ instance }: InstanceSSHPanelProps) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  const queryClient = useQueryClient()
  const onChanged = useCallback(() => queryClient.invalidateQueries({ queryKey: ["instances"] }), [queryClient])
  const { state, test, save, clear, pin } = useSSHSetup({ instanceId: instance.id, onChanged })
  const [replaceOpen, setReplaceOpen] = useState(false)

  const stored = !!instance.sshUsername
  const busy = state.kind !== "idle"
  const result = state.result

  // A key is needed on the first save and when the endpoint or user changes;
  // with the three unchanged and no key pasted, there is nothing to send.
  const endpointChanged = (value: { host: string; port: number; username: string }) =>
    !stored || value.host.trim() !== instance.sshHost || value.port !== instance.sshPort || value.username.trim() !== instance.sshUsername
  // Compared against the row, not the form's dirty flag: an edit typed back
  // to the stored value is no unsaved change.
  const hasUnsavedEdits = (value: { host: string; port: number; username: string; privateKey: string }) =>
    value.privateKey !== "" || (stored && endpointChanged(value))

  const form = useForm({
    defaultValues: sshDefaults(instance),
    onSubmit: async ({ value }) => {
      const needsKey = endpointChanged(value)
      if (!needsKey && value.privateKey === "") {
        return
      }
      if (await save({ host: value.host.trim(), port: value.port, username: value.username.trim(), privateKey: value.privateKey })) {
        // A changed endpoint remounts the form from the row; a rotated key
        // alone leaves the row's endpoint as it was, so the field is cleared here.
        form.reset({ ...form.state.values, privateKey: "" })
        toast.success(t("preferences.ssh.toast.saved"))
      }
    },
  })

  const handleClear = async () => {
    if (await clear()) {
      toast.success(t("preferences.ssh.toast.cleared"))
    }
  }

  const blur = incognitoMode ? "blur-sm select-none" : ""

  return (
    <>
      <PreferencesFormShell
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          form.handleSubmit()
        }}
        footer={(
          <div className="flex w-full items-center justify-between gap-2">
            <div>
              {stored && (
                <Button type="button" variant="outline" disabled={busy} onClick={handleClear}>
                  {t("preferences.ssh.buttons.clear")}
                </Button>
              )}
            </div>
            <form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting]}>
              {([canSubmit, isSubmitting]) => (
                <Button type="submit" disabled={!canSubmit || isSubmitting || busy} className="min-w-32">
                  {state.kind === "saving" ? t("preferences.common.saving") : t("preferences.ssh.buttons.save")}
                </Button>
              )}
            </form.Subscribe>
          </div>
        )}
      >
        <fieldset disabled={busy} className="space-y-6">
          {instance.hasLocalFilesystemAccess && stored && (
            <p className="rounded-lg border bg-muted/40 p-4 text-sm text-muted-foreground" role="status">
              {t("preferences.ssh.localWins")}
            </p>
          )}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <form.Field
              name="host"
              validators={{ onChange: ({ value }) => value.trim() === "" ? t("preferences.ssh.validation.hostRequired") : undefined }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="ssh-host" className="flex items-center gap-2">
                    {t("preferences.ssh.labels.host")} <span className="text-destructive" aria-hidden="true">*</span>
                    <FieldHelp>{t("preferences.ssh.labels.hostDescription")}</FieldHelp>
                  </Label>
                  <Input
                    id="ssh-host"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("preferences.ssh.placeholders.host")}
                    className={blur}
                    autoComplete="off"
                    aria-required="true"
                    aria-invalid={field.state.meta.isTouched && !!field.state.meta.errors[0]}
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="port"
              validators={{ onChange: ({ value }) => Number.isInteger(value) && value >= 1 && value <= 65535 ? undefined : t("preferences.ssh.validation.portRange") }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="ssh-port">{t("preferences.ssh.labels.port")}</Label>
                  <Input
                    id="ssh-port"
                    type="number"
                    min={1}
                    max={65535}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(Number(e.target.value))}
                    aria-invalid={field.state.meta.isTouched && !!field.state.meta.errors[0]}
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>

            <form.Field
              name="username"
              validators={{ onChange: ({ value }) => value.trim() === "" ? t("preferences.ssh.validation.usernameRequired") : undefined }}
            >
              {(field) => (
                <div className="space-y-2">
                  <Label htmlFor="ssh-username">
                    {t("preferences.ssh.labels.username")} <span className="text-destructive" aria-hidden="true">*</span>
                  </Label>
                  <Input
                    id="ssh-username"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    className={blur}
                    data-1p-ignore
                    autoComplete="off"
                    aria-required="true"
                    aria-invalid={field.state.meta.isTouched && !!field.state.meta.errors[0]}
                  />
                  {field.state.meta.isTouched && field.state.meta.errors[0] && (
                    <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                  )}
                </div>
              )}
            </form.Field>
          </div>

          <form.Field
            name="privateKey"
            validators={{
              onChangeListenTo: ["host", "port", "username"],
              onChange: ({ value, fieldApi }) => {
                const { host, port, username } = fieldApi.form.state.values
                return value === "" && endpointChanged({ host, port, username }) ? t("preferences.ssh.validation.keyRequired") : undefined
              },
            }}
          >
            {(field) => (
              <div className="space-y-2">
                <Label htmlFor="ssh-private-key" className="flex items-center gap-2">
                  {t("preferences.ssh.labels.privateKey")}
                  <FieldHelp>{t("preferences.ssh.labels.privateKeyDescription")}</FieldHelp>
                </Label>
                <Textarea
                  id="ssh-private-key"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(e) => field.handleChange(e.target.value)}
                  placeholder={stored ? t("preferences.ssh.placeholders.privateKeyStored") : t("preferences.ssh.placeholders.privateKey")}
                  rows={5}
                  spellCheck={false}
                  className={`font-mono text-xs ${blur}`}
                  autoComplete="off"
                  aria-invalid={!!field.state.meta.errors[0]}
                />
                {field.state.meta.errors[0] && (
                  <p className="text-sm text-destructive" role="alert">{field.state.meta.errors[0]}</p>
                )}
              </div>
            )}
          </form.Field>

          {state.kind === "idle" && state.error && (
            <p className="text-sm text-destructive" role="alert">{formatErrorMessage(state.error)}</p>
          )}

          {stored && (
            <form.Subscribe selector={(s) => hasUnsavedEdits(s.values)}>
              {(isDirty) => (
                // A test runs against the stored credentials, so unsaved edits
                // hide its result: a fingerprint under a host the row does not hold yet would mislead.
                <div className="space-y-4 rounded-lg border bg-muted/40 p-4">
                  <div className="flex items-center justify-between gap-4">
                    <span className="text-sm font-medium">{t("preferences.ssh.test.title")}</span>
                    <Button type="button" variant="outline" size="sm" disabled={isDirty} onClick={() => test()}>
                      {state.kind === "testing" ? t("preferences.ssh.buttons.testing") : t("preferences.ssh.buttons.test")}
                    </Button>
                  </div>
                  {isDirty ? (
                    <p className="text-sm text-muted-foreground">{t("preferences.ssh.test.saveFirst")}</p>
                  ) : result ? (
                    <SSHTestResult
                      result={result}
                      instance={instance}
                      onConfirm={() => pin(false)}
                      onReplace={() => setReplaceOpen(true)}
                    />
                  ) : (
                    <>
                      <p className="text-sm text-muted-foreground">
                        {instance.sshHostKeyPinned ? t("preferences.ssh.test.pinnedAtRest") : t("preferences.ssh.test.unpinnedAtRest")}
                      </p>
                      {/* The limits come from the stored probe, so a pinned host shows them before any new test. */}
                      {instance.sshHostKeyPinned && <RemoteAccessIndicator instance={instance} variant="inline" />}
                    </>
                  )}
                </div>
              )}
            </form.Subscribe>
          )}
        </fieldset>
      </PreferencesFormShell>

      <AlertDialog open={replaceOpen} onOpenChange={setReplaceOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("preferences.ssh.replaceDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3">
                <p>{t("preferences.ssh.replaceDialog.description")}</p>
                {result && <Fingerprints result={result} />}
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("preferences.ssh.replaceDialog.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => pin(true)}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("preferences.ssh.replaceDialog.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

interface SSHTestResultProps {
  result: SSHTestResponse
  instance: Instance
  onConfirm: () => void
  onReplace: () => void
}

function SSHTestResult({ result, instance, onConfirm, onReplace }: SSHTestResultProps) {
  const { t } = useTranslation("instances")
  const tier = instance.sshTier

  switch (result.status) {
    case "unpinned":
      return (
        <div className="space-y-3" data-ssh-status="unpinned">
          <p className="text-sm font-medium">{t("preferences.ssh.status.unpinned.title")}</p>
          <p className="text-sm text-muted-foreground">{t("preferences.ssh.status.unpinned.text")}</p>
          <Fingerprints result={result} />
          <ProbeTable probe={result.capabilities} tier={tier} />
          <Button type="button" size="sm" disabled={!result.hostKey} onClick={onConfirm}>
            {t("preferences.ssh.buttons.confirm")}
          </Button>
        </div>
      )
    case "pinned":
      return (
        <div className="space-y-3" data-ssh-status="pinned">
          <p className="flex items-center gap-2 text-sm font-medium">
            <Check className="h-4 w-4 text-green-500" aria-hidden="true" />
            {t("preferences.ssh.status.pinned.title")}
          </p>
          <ProbeTable probe={result.capabilities} tier={tier} />
          {/* The hardlink flag comes from this test, not the row: a failed probe write would otherwise leave the limits contradicting the table above. */}
          <RemoteAccessIndicator instance={result.capabilities ? { ...instance, sshHardlink: result.capabilities.hardlink } : instance} variant="inline" />
        </div>
      )
    case "mismatch":
      return (
        <div className="space-y-3" data-ssh-status="mismatch">
          <p className="text-sm font-medium text-destructive">{t("preferences.ssh.status.mismatch.title")}</p>
          <p className="text-sm">{t("preferences.ssh.status.mismatch.text")}</p>
          <Fingerprints result={result} />
          <Button type="button" size="sm" variant="destructive" disabled={!result.hostKey} onClick={onReplace}>
            {t("preferences.ssh.buttons.replace")}
          </Button>
        </div>
      )
    case "pin_unreadable":
      return (
        <div className="space-y-3" data-ssh-status="pin_unreadable">
          <p className="text-sm font-medium text-destructive">{t("preferences.ssh.status.pinUnreadable.title")}</p>
          <p className="text-sm">{t("preferences.ssh.status.pinUnreadable.text")}</p>
          {result.hostKey ? (
            <Fingerprints result={result} />
          ) : (
            <p className="text-sm text-muted-foreground">{t("preferences.ssh.status.pinUnreadable.noKey")}</p>
          )}
          <Button type="button" size="sm" variant="destructive" disabled={!result.hostKey} onClick={onReplace}>
            {t("preferences.ssh.buttons.replace")}
          </Button>
        </div>
      )
    case "error":
      return (
        <div className="space-y-2" data-ssh-status="error">
          <p className="text-sm font-medium text-destructive">{t("preferences.ssh.status.error.title")}</p>
          <p className="text-sm break-all" role="alert">{formatErrorMessage(result.error)}</p>
        </div>
      )
  }
}

function Fingerprints({ result }: { result: SSHTestResponse }) {
  const { t } = useTranslation("instances")
  const [incognitoMode] = useIncognitoMode()
  const blur = incognitoMode ? "blur-sm select-none" : ""
  return (
    <div className="space-y-2">
      {result.pinnedFingerprint && (
        <FingerprintRow label={t("preferences.ssh.fingerprint.pinned")} keyType={result.pinnedKeyType} value={result.pinnedFingerprint} className={blur} />
      )}
      {result.fingerprint && (
        <FingerprintRow
          label={result.pinnedFingerprint ? t("preferences.ssh.fingerprint.presented") : t("preferences.ssh.fingerprint.label")}
          keyType={result.keyType}
          value={result.fingerprint}
          className={blur}
        />
      )}
    </div>
  )
}

function FingerprintRow({ label, keyType, value, className }: { label: string; keyType?: string; value: string; className: string }) {
  return (
    <div>
      <span className="text-xs uppercase text-muted-foreground">{label}{keyType ? ` · ${keyType}` : ""}</span>
      <div className="mt-1 flex items-center gap-2">
        <code className={`w-full flex-1 rounded bg-muted px-2 py-1.5 text-xs font-mono break-all ${className}`}>{value}</code>
        <CopyButton value={value} label={label} className="h-7 w-7" />
      </div>
    </div>
  )
}

function ProbeTable({ probe, tier }: { probe?: SSHProbe; tier: Instance["sshTier"] }) {
  const { t } = useTranslation("instances")
  if (!probe) {
    return null
  }
  const rows: { label: string; ok: boolean }[] = [
    { label: t("preferences.ssh.probes.sftp"), ok: probe.sftp },
    { label: t("preferences.ssh.probes.statvfs"), ok: probe.statvfs },
    { label: t("preferences.ssh.probes.hardlink"), ok: probe.hardlink },
    { label: t("preferences.ssh.probes.limits"), ok: probe.limits },
    { label: t("preferences.ssh.probes.exec"), ok: probe.exec },
    { label: t("preferences.ssh.probes.gnuUserland"), ok: probe.gnuUserland },
  ]
  return (
    <div className="space-y-2">
      <p className="flex items-center gap-2 text-sm">
        {t("preferences.ssh.probes.tier", { tier: tier === "exec" ? t("preferences.ssh.probes.tierExec") : t("preferences.ssh.probes.tierSftp") })}
        <FieldHelp>{t("preferences.ssh.probes.tierHint")}</FieldHelp>
      </p>
      <ul className="grid grid-cols-1 gap-1 text-sm sm:grid-cols-2">
        {rows.map((row) => (
          <li key={row.label} className="flex items-center gap-2">
            {row.ok
              ? <Check className="h-4 w-4 text-green-500" aria-label={t("preferences.ssh.probes.yes")} />
              : <X className="h-4 w-4 text-muted-foreground" aria-label={t("preferences.ssh.probes.no")} />}
            {row.label}
          </li>
        ))}
      </ul>
    </div>
  )
}
