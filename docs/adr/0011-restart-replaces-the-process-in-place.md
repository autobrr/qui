---
status: accepted
date: 2026-09-28
---

# A Restart replaces the process in place

A Restart runs the graceful shutdown of `serve`, the same steps as SIGTERM. Then qui starts again as the same process. qui never restarts by exiting and relying on a supervisor to start it again. Issue #2853.

On Unix (linux, darwin, freebsd), qui calls `syscall.Exec` with the resolved binary path, `os.Args` unchanged, and the environment unchanged. The process ID does not change. qui resolves the binary path with `os.Executable` and `filepath.EvalSymlinks` at startup, before a Self-update can replace the file. qui does not exec `/proc/self/exe`, because that changes the process name to `exe`. qui does not change argv.

Before the shutdown, qui checks that the binary exists, is a regular file, and is executable. If the check fails, qui refuses the Restart and keeps running.

When the graceful shutdown hits the 30-second timeout, a Restart logs a warning and continues to the exec. SIGTERM exits with 1 after the timeout. A Restart that did the same would leave qui down on installs without a supervisor.

When the exec returns an error, qui logs the error and exits with a non-zero code. Installs with `Restart=on-failure` or `Restart=always` then start qui again. A process that serves nothing is worse than an exit.

## Windows

The Windows Restart comes in #2858: the first process becomes a supervisor with a kill-on-close job object. That issue adds its points here.

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
- **Restart `serve` inside the process.** Rejected: global state and the metric registration make a second `serve` unsafe, and it cannot load a new binary after a Self-update.

## Consequences

- A Restart releases the port and the database because the exec closes every file descriptor. The deferred cleanup of `serve` does not run, the same as on SIGTERM.
- A change that makes a Restart exit, or that execs a different path or argv, reverses this decision and needs a new ADR.
