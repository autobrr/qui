---
status: accepted
date: 2026-09-29
---

# The Tray lives in the served child of a second Windows binary

Windows users run qui in a console window that must stay open. The Tray replaces that window as the user's handle on a running qui. The Windows release ships two binaries from the same `cmd/qui` source:

- `qui.exe` is a console program. It does not change. The CLI, the headless Scheduled Task setup, and every subcommand keep working.
- `qui-tray.exe` is a GUI-subsystem build (`-H windowsgui`). Windows opens no console for it. A link-time variable marks the build, not the file name, so a renamed exe still works. It always runs `serve` with the Tray and accepts the `serve` flags. A subcommand shows an error dialog that points to `qui.exe`.

The Tray runs in the served child that the supervisor starts (ADR 0011), not in the supervisor and not in a separate app. The child already has the config, the Restart, and the shutdown path, so the Tray needs no API key and no IPC. The icon disappears for a moment during a Restart and comes back with the new child. When the Tray cannot start, for example in a task that runs without a logon session, qui logs a warning and keeps serving.

The Tray is Windows only. It uses `fyne.io/systray` behind `//go:build windows`, which is pure Go on Windows, so the release keeps `CGO_ENABLED=0`.

## Consequences

- Self-update replaces both binaries in one Install when both sit in the same folder. It downloads and verifies the release once. Each binary gets its own backup name. Replacing only the running binary would leave `qui.exe` one version behind, and a later `qui.exe` command would run against a database that the newer binary migrated.
- The lock that stops a Restart during a Self-update lives in the `update` package, so the Tray and the API share it.
- In `qui-tray.exe`, stderr is not a valid handle. The log writer drops stderr when it is not valid, so the log file and the web UI log stream keep working. When `logPath` is unset, `qui-tray.exe` writes to `log/qui.log` in the config folder.
- A fatal error in `qui-tray.exe`, such as a port already in use, shows a native error dialog before the process exits. There is no console to print it to. The supervisor shows the dialog: it gives the served child a pipe for stderr and keeps the end of it. So the dialog also covers a config error, which happens before the log file exists, and a panic. The zerolog exit hook cannot see the message of the event.
- "Start with Windows" writes a value under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` that points at `qui-tray.exe`. Users pick this or the Scheduled Task with `qui.exe`, never both, or two copies start.
- A change that moves the Tray into its own process, or that drops the console binary, reverses this decision and needs a new ADR.

## Considered options

- **One GUI-subsystem binary that calls `AttachConsole`.** Rejected: `cmd.exe` does not wait for a GUI program, so every CLI command returns to the prompt at once and its output mixes with the prompt.
- **One console binary that hides its own window at start (`serve --tray`), as Syncthing's `--no-console` does.** Rejected: the window flashes at start, and on Windows 11 the default console host is Windows Terminal, where hiding the console window does not reliably hide the terminal.
- **A separate tray app that starts qui hidden and talks to its API, as Jellyfin and SyncTrayzor do.** Rejected: it needs an API key or config parsing, its own lifecycle, and a second program to release.
- **The Tray in the supervisor.** Rejected: the supervisor loads no config, so it does not know the URL.
- **A Windows service.** Rejected: a service runs in session 0 and cannot show a tray icon. The Tray would still need a second process.
