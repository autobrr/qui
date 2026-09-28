---
status: accepted
date: 2026-09-28
---

# A Restart replaces the process in place

A Restart runs the graceful shutdown of `serve`, the same steps as SIGTERM. Then qui starts again as the same process. qui never restarts by exiting and relying on an external supervisor to start it again. On Windows, qui runs its own supervisor (see below). Issue #2853.

On Unix (linux, darwin, freebsd), qui calls `syscall.Exec` with the resolved binary path, `os.Args` unchanged, and the environment unchanged. The process ID does not change. qui resolves the binary path with `os.Executable` and `filepath.EvalSymlinks` at startup, before a Self-update can replace the file. qui does not exec `/proc/self/exe`, because that changes the process name to `exe`. qui does not change argv.

Before the shutdown, qui checks that the binary exists, is a regular file, and is executable. If the check fails, qui refuses the Restart and keeps running.

When the graceful shutdown hits the 30-second timeout, a Restart logs a warning and continues to the exec. SIGTERM exits with 1 after the timeout. A Restart that did the same would leave qui down on installs without a supervisor.

When the exec returns an error, qui logs the error and exits with a non-zero code. Installs with `Restart=on-failure` or `Restart=always` then start qui again. A process that serves nothing is worse than an exit.

## Windows

Windows has no exec that keeps the process. The first `serve` process becomes a supervisor before it loads the config or opens the database:

- It starts the resolved binary as a child, with the same argv and the environment variable `QUI_SUPERVISED=1`, and waits for it. The child serves qui.
- It puts itself in a job object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` before it starts a child, so each child starts inside the job. When the supervisor stops, Windows closes the job handle and stops the child. Task Scheduler "End" stops the supervisor, so it also stops the child.
- A child that gets a Restart request runs the graceful shutdown and exits with the reserved code 75. The supervisor then starts the binary again from the same path. After a Self-update, that path holds the new release.
- When the child exits with any other code, the supervisor exits with that code.
- The supervisor ignores Ctrl+C and the console close event. The child shares the console and handles them.

The supervisor starts with `serve`, not on the first Restart. The graceful shutdown stops only the HTTP server and the partial pool. Automations, reannounce, scans, backups, and the sync loops stop only when the process exits. A process that became the supervisor on its first Restart would keep them running next to its child. Two copies would then act on qBittorrent and write to the database. The cost is a second, idle `qui.exe` in Task Manager.

## Evidence

- The autobrr installer units for HostingByDesign and Ultra.cc, and the swizzin hosted-scripts unit, have no `Restart=`. An exit leaves qui down.
- Bytesized (autobrr installer) has no watchdog.
- The watchdog on the Whatbox wiki runs `pgrep -f "qui"`. The pattern matches the cron script itself, so the watchdog never starts qui.
- The Feral and Seedhost cron watchdogs match the full command line with `pgrep -f`. A changed argv makes them start a second copy.
- Provider stop scripts run `pkill qui` and `pkill -f qui`. A changed process name makes them miss qui.
- Task Scheduler lets a child process break away from the task job. A child that outlives its parent is out of reach of "End".

## Considered options

- **Exit with a non-zero code and let the supervisor start qui again.** Rejected: the installs above have no supervisor that restarts qui, so qui stays down.
- **On Windows, start a child and exit.** Rejected: the child breaks away from the task job. "End" cannot stop it, and "Run" starts a second copy.
- **On Windows, become the supervisor on the first Restart.** Rejected: the services of the first process keep running next to the child.
- **Restart `serve` inside the process.** Rejected: global state and the metric registration make a second `serve` unsafe, and it cannot load a new binary after a Self-update.

## Consequences

- On Unix, a Restart releases the port and the database because the exec closes every file descriptor. On Windows, the child exit releases them. The deferred cleanup of `serve` does not run, the same as on SIGTERM.
- On Windows the supervisor keeps running the file that it started from. After a Self-update that file is a backup, and a later update cannot delete it until the task stops.
- A change that makes a Restart exit to an external supervisor, or that execs a different path or argv, reverses this decision and needs a new ADR.
