---
status: accepted
date: 2026-11-21
---

# qui gives each remote command to sh on standard input

sshd gives the command of a session to the login shell of the user as text. qui quotes each path for a POSIX shell. fish, tcsh, nushell, and other shells read that quoting differently, so a quoted path could end its quote early and run as a command. qui used to refuse commands on those hosts, and the user had to change the login shell.

Now the command of each session is the bare word `sh`, which every shell reads the same way. qui writes the real command to the standard input of that `sh`. Only a POSIX shell reads the quoting of qui.

Rejected: `/bin/sh`, because restricted bash refuses a command name with a slash. Rejected: the old way first and `sh` only when the quoting probe fails, because it keeps two ways to run a command, and one of them runs only on some hosts.

## Consequences

- A forced command in `authorized_keys` sees `sh` in `SSH_ORIGINAL_COMMAND`, not `find` or `stat`. A wrapper script that allows commands by name must allow `sh`, or the key gets the tier SFTP. A wrapper that allows `sh` lets the key run any command that it sends on standard input, so the name check no longer limits what runs. Where that access is not acceptable, use a key that is limited to SFTP.
- No remote command of qui can read standard input, because the script is on it.
- The quoting probe still runs, through `sh`. It fails when the login shell prints text at start, when the host has no `sh`, and when `sh` is not a POSIX shell. Such a host gets the tier SFTP.
