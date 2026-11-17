/*
 * PROTOTYPE, throwaway. Fake data for the "Report a problem" prototype. Not for production.
 */

export type Identity = "anonymous" | "signed-in"
export type Lane = "open" | "full"
export type View = "compose" | "sent"
export type Source = "menu" | "error" | "search"

export const signedInUser = { name: "mira", provider: "Discord" as const }

export const errorContext = {
  title: "Could not load torrents from Stub 2",
  cause: "GET /api/instances/2/torrents: 502 Bad Gateway",
  since: "09:41, 3 min ago",
}

export interface InfoRow {
  label: string
  value: string
  secondary?: string
  monospace?: boolean
}

export const qui = { version: "v1.36.0", instances: 2 }

export const systemRows: InfoRow[] = [
  { label: "Version", value: "v1.36.0", monospace: true },
  { label: "System", value: "linux/amd64, Docker", monospace: true },
  { label: "Database", value: "sqlite", monospace: true },
  { label: "Stub 1", value: "qBittorrent 5.1.2, libtorrent 2.0.11", secondary: "Connected" },
  { label: "Stub 2", value: "qBittorrent 4.6.7, libtorrent 1.2.19", secondary: "Connected" },
  { label: "Page", value: "Torrents, Stub 1" },
  { label: "Browser", value: "Firefox 147, 2560 × 1440" },
  { label: "System notices", value: "None" },
  { label: "Failed requests", value: "2 in the last 10 minutes" },
]

export const logLines = [
  "2026-11-17 09:41:02 WRN sync: Stub 2 answered after 4.1 s",
  "2026-11-17 09:41:07 ERR api: GET /api/instances/2/torrents 502",
  "2026-11-17 09:41:07 ERR qbittorrent: Stub 2 login failed, retry in 5 s",
  "2026-11-17 09:41:12 WRN reannounce: tracker.example announce=[removed] timed out",
  "2026-11-17 09:41:30 ERR api: GET /api/instances/2/torrents 502",
  "2026-11-17 09:42:01 WRN sync: Stub 2 answered after 3.8 s",
  "2026-11-17 09:44:16 ERR api: GET /api/instances/2/torrents 502",
  "2026-11-17 09:44:16 ERR qbittorrent: Stub 2 login failed, retry in 5 s",
  "2026-11-17 09:44:21 WRN crossseed: search for Example.Show.S02E04 skipped, Stub 2 is offline",
  "2026-11-17 09:46:40 WRN sync: Stub 2 answered after 5.2 s",
  "2026-11-17 09:46:52 ERR api: GET /api/instances/2/torrents 502",
  "2026-11-17 09:47:03 WRN reannounce: tracker.example announce=[removed] timed out",
  "2026-11-17 09:49:10 ERR qbittorrent: Stub 2 login failed, retry in 5 s",
  "2026-11-17 09:51:33 WRN sync: Stub 2 answered after 4.4 s",
  "2026-11-17 09:52:02 ERR api: GET /api/instances/2/torrents 502",
]
export const logLineCount = 200

/** What "Everything recent" adds: the last lines at the configured log level, DEBUG by default. */
export const allLogLines = [
  "2026-11-17 09:57:58 INF crossseed: manual search for Example.Show.S02E04.1080p.WEB.h264-GRPB",
  "2026-11-17 09:57:58 DBG crossseed: source size 2.31 GiB, tolerance 5%",
  "2026-11-17 09:57:59 DBG jackett: Indexer A answered 20 results in 0.8 s",
  "2026-11-17 09:58:00 DBG jackett: Indexer B answered 18 results in 1.4 s",
  "2026-11-17 09:58:00 WRN jackett: Indexer C rate limited, retry pass skipped",
  "2026-11-17 09:58:01 DBG crossseed: Example.Show.S02E04.1080p.WEB.h264-GRPC rejected: release group differs",
  "2026-11-17 09:58:01 DBG crossseed: Example.Show.S02E04.720p.WEB.h264-GRPB rejected: resolution differs",
  "2026-11-17 09:58:01 DBG crossseed: Example.Show.S02E04.1080p.WEB.h264-GRPB rejected: size differs by 6.2%",
  "2026-11-17 09:58:01 DBG crossseed: Example.Show.S02.1080p.WEB.h264-GRPB rejected: season pack",
  "2026-11-17 09:58:02 INF crossseed: search finished, 38 candidates, 0 matches",
  "2026-11-17 09:58:05 DBG sync: Stub 1 updated 60 torrents in 41 ms",
]
export const allLogLineCount = 1000

export const searchContext = {
  title: "Cross-seed search for Example.Show.S02E04.1080p.WEB.h264-GRPB",
  summary: "09:58, 38 candidates, 0 matches",
}

export const searchBreakdown = {
  rejections: [
    { reason: "Release mismatch", count: 23 },
    { reason: "Size mismatch", count: 11 },
    { reason: "Duplicates", count: 4 },
  ],
  indexers: [
    { name: "Indexer A", status: "searched", candidates: 20 },
    { name: "Indexer B", status: "searched", candidates: 18 },
    { name: "Indexer C", status: "incomplete", candidates: 0 },
  ],
}

export type ReportStatus = "received" | "issue" | "closed" | "not-sent"

export interface SentReport {
  id: string
  title: string
  when: string
  age: string
  status: ReportStatus
  detail?: string
}

export const sentReports: SentReport[] = [
  { id: "r2", title: "Cross-seed search stops after the first indexer", when: "Today, 13:10", age: "5h", status: "received" },
  { id: "r1", title: "The tracker table on Statistics does not sort", when: "Nov 15", age: "2d", status: "issue", detail: "#4300" },
  { id: "r3", title: "Pin an instance to the top of the sidebar", when: "Nov 11", age: "6d", status: "closed", detail: "Already in v1.36.0" },
]

export const draftReport: SentReport = {
  id: "draft",
  title: "Stub 2 keeps disconnecting every few minutes",
  when: "Today, 18:02",
  age: "now",
  status: "not-sent",
}

export function statusText(report: SentReport): string {
  switch (report.status) {
    case "received":
      return "Received"
    case "issue":
      return `Became issue ${report.detail}`
    case "closed":
      return `Closed: ${report.detail}`
    case "not-sent":
      return "Not sent"
  }
}

export const privacyText = {
  anonymous: "Anonymous: no name, no account. brrbin keeps no IP address.",
  signedIn: `The maintainers see your ${signedInUser.provider} name (${signedInUser.name}) and may contact you.`,
}

export const laneFull = {
  title: "Anonymous reports are full for today",
  body: "Sign in to send now, or try again tomorrow. Your text stays here as a draft.",
}

export const featureNotice = {
  title: "Report a problem from qui",
  description: "Tell the maintainers what went wrong, with no GitHub account. qui adds its version and recent errors. Nothing leaves qui before you press Send.",
  action: "Report a problem",
}
