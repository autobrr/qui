/*
 * PROTOTYPE, throwaway. Variant C: a Settings tab, "Problem reports", built from the cards of the
 * Application tab. The sidebar button, the menus, and "Report this" open this tab.
 */

import { AlertTriangle, Loader2, Trash2 } from "lucide-react"
import { useState, type ReactNode } from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"
import { laneFull, logLineCount, logLines, signedInUser, statusText, systemRows } from "./mock"
import { PrivacyLine, SenderMenu, StatusDot } from "./parts"
import { useComposer, useSentReports, type Composer } from "./useComposer"

export function ReportsSettingsPanel() {
  const c = useComposer()
  return (
    <div className="space-y-4">
      <Card>
        <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1.5">
            <CardTitle>Report a problem</CardTitle>
            <CardDescription>Tell the maintainers what went wrong. qui adds the details below. Nothing leaves qui before you press Send.</CardDescription>
          </div>
          <SenderMenu c={c} variant="outline" className="h-8 shrink-0 px-3 text-sm" />
        </CardHeader>
        <CardContent className="space-y-4">
          {c.phase === "blocked" && (
            <Alert>
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{laneFull.title}</AlertTitle>
              <AlertDescription>
                <p>{laneFull.body}</p>
                <div className="mt-2 flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" disabled={!!c.signingIn} onClick={() => c.signIn("Discord")}>Sign in with Discord</Button>
                  <Button size="sm" variant="outline" disabled={!!c.signingIn} onClick={() => c.signIn("GitHub")}>Sign in with GitHub</Button>
                </div>
              </AlertDescription>
            </Alert>
          )}
          <Textarea
            value={c.text}
            onChange={(e) => c.setText(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && c.canSend) c.send() }}
            placeholder={c.hasError ? "What were you doing when it happened? (optional)" : "What happened, and what did you expect?"}
            className="min-h-32 resize-y text-sm"
          />
        </CardContent>
        <CardFooter className="flex flex-col-reverse items-stretch gap-3 border-t pt-6 sm:flex-row sm:items-center sm:justify-between">
          <PrivacyLine c={c} className="max-w-md" />
          <Button className="min-h-11 shrink-0 md:min-h-9" disabled={!c.canSend} onClick={c.send}>
            {c.phase === "sending" && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {c.phase === "sending" ? "Sending" : "Send report"}
          </Button>
        </CardFooter>
      </Card>

      <Attached c={c} />
      <SentReports />
      <Sharing c={c} />
    </div>
  )
}

interface Field {
  label: string
  value: ReactNode
  secondary?: string
  monospace?: boolean
  muted?: boolean
  action?: ReactNode
  extra?: ReactNode
}

/** The ApplicationSection of the Settings page: uppercase labels, one row each, lines between. */
function Section({ title, description, fields }: { title: string; description: string; fields: Field[] }) {
  return (
    <Card>
      <CardHeader>
        <div className="space-y-1.5">
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <dl className="divide-y">
          {fields.map((field) => (
            <div key={field.label} className="px-4 py-3 sm:px-6">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
                <dt className="text-xs uppercase text-muted-foreground sm:w-44 sm:shrink-0">{field.label}</dt>
                <dd className="min-w-0 flex-1">
                  <div className="flex items-start gap-2">
                    <div className={cn("min-w-0 flex-1", field.muted && "text-muted-foreground line-through")}>
                      <div className={cn(field.monospace ? "font-mono text-xs sm:text-sm" : "text-sm font-medium", "break-words")}>{field.value}</div>
                      {field.secondary && <p className="mt-1 text-xs text-muted-foreground">{field.secondary}</p>}
                    </div>
                    {field.action}
                  </div>
                  {field.extra}
                </dd>
              </div>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  )
}

function Attached({ c }: { c: Composer }) {
  const [logsOpen, setLogsOpen] = useState(false)
  const fields: Field[] = [
    ...(c.hasError ? [{ label: "Error", value: c.error.title, secondary: c.error.cause }] : []),
    ...systemRows.map((row) => ({ label: row.label, value: row.value, secondary: row.secondary, monospace: row.monospace })),
    {
      label: "Logs",
      value: `Last ${logLineCount} warnings and errors`,
      secondary: c.logsIncluded ? "Keys and passkeys removed." : "Not sent.",
      muted: !c.logsIncluded,
      action: (
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" className="h-7 text-xs" disabled={!c.logsIncluded} onClick={() => setLogsOpen(!logsOpen)}>{logsOpen ? "Hide" : "View"}</Button>
          <Switch aria-label="Send the logs" checked={c.logsIncluded} onCheckedChange={c.setLogsIncluded} />
        </div>
      ),
      extra: logsOpen && c.logsIncluded && (
        <pre className="mt-3 max-h-40 overflow-auto rounded-md border bg-muted/40 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-muted-foreground">{logLines.join("\n")}</pre>
      ),
    },
  ]
  return <Section title="Sent with your report" description="qui fills this in. You can leave out the logs." fields={fields} />
}

function SentReports() {
  const { rows, remove } = useSentReports()
  return (
    <Card>
      <CardHeader>
        <div className="space-y-1.5">
          <CardTitle>Sent reports</CardTitle>
          <CardDescription>The status updates each time you open this page.</CardDescription>
        </div>
      </CardHeader>
      <CardContent className="p-0">
        <ul className="divide-y border-t">
          {rows.map((r) => (
            <li key={r.id} className="group flex items-center gap-3 px-4 py-3 sm:px-6">
              <StatusDot status={r.status} />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{r.title}</p>
                <p className="text-xs text-muted-foreground">{r.when}</p>
              </div>
              <span className={cn("hidden text-sm sm:block", r.status === "issue" ? "text-foreground" : "text-muted-foreground")}>
                {r.status === "issue" ? <a href="#issue" className="underline-offset-2 hover:underline">{statusText(r)}</a> : statusText(r)}
              </span>
              {r.status === "not-sent" && <Button size="sm" variant="outline" className="h-7 text-xs">Send</Button>}
              <Button variant="ghost" size="icon" aria-label="Delete report" className="h-7 w-7 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100" onClick={() => remove(r.id)}>
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function Sharing({ c }: { c: Composer }) {
  return (
    <Section
      title="Sharing"
      description="How qui sends problem reports."
      fields={[
        { label: "Problem reports", value: "On", secondary: "Turn off with problemReports = false in config.toml, or QUI__PROBLEM_REPORTS=false." },
        c.anonymous
          ? { label: "Sender", value: "Anonymous", secondary: "brrbin keeps no IP address. Sign in so the maintainers can ask you for more.", action: <SignInButtons c={c} /> }
          : { label: "Sender", value: `${signedInUser.name} (${signedInUser.provider})`, secondary: "The maintainers can see your name and contact you.", action: <Button variant="outline" size="sm" onClick={c.signOut}>Sign out</Button> },
      ]}
    />
  )
}

function SignInButtons({ c }: { c: Composer }) {
  if (c.signingIn) {
    return <Button variant="outline" size="sm" disabled><Loader2 className="mr-2 h-4 w-4 animate-spin" />Waiting for {c.signingIn}</Button>
  }
  return (
    <div className="flex shrink-0 gap-2">
      <Button variant="outline" size="sm" onClick={() => c.signIn("Discord")}>Discord</Button>
      <Button variant="outline" size="sm" onClick={() => c.signIn("GitHub")}>GitHub</Button>
    </div>
  )
}
