---
status: accepted
date: 2026-09-29
---

# The MSI installs per user, and Self-update stays the update path

Windows users download a zip, extract it to a folder that they pick, and start `qui-tray.exe` by hand. They get no Start Menu entry and no uninstall entry. Every release now also ships `qui_<version>_windows_x86_64.msi` next to the zip.

The MSI installs `qui.exe` and `qui-tray.exe` for the current Windows account in `%LOCALAPPDATA%\Programs\qui`. It adds a Start Menu entry that starts the Tray, adds the folder to the per-user `PATH`, and adds an uninstall entry. It needs no administrator rights and shows no wizard. The user can write to that folder, so Self-update stays on.

GoReleaser Pro builds the MSI on the Linux release runner with wixl from msitools. wixl reads the WiX v3 schema and supports only part of it, so `distrib/windows/qui.wxs` uses only the elements that wixl supports.

## Consequences

- The zip stays. Self-update downloads the zip and takes both exes from it (ADR 0012), on an MSI install too. go-selfupdate ignores `.msi` assets, and a test holds that.
- After a Self-update, Installed apps still shows the version of the MSI. The files are newer than the label. A newer MSI still upgrades such an install, because wixl removes the old product before it copies the new files.
- The MSI never reads, moves, or deletes `%APPDATA%\qui`. `config.toml` holds the secret that decrypts fields in `qui.db`, so the two files must always move together. The MSI changes neither file.
- An MSI upgrade while qui runs does not stop qui. Windows offers to close it, but the old version keeps serving and holds the port, so the new version fails to start. The docs tell users to quit qui first.
- The MSI does not write the "Start with Windows" value. The Tray owns it (ADR 0012), and the uninstall cannot remove it. Users turn it off before they uninstall.
- wixl 0.106 has no `RemoveFile` element. A custom action runs `cmd /c del` on the Self-update backups (`*.bak.exe`) at uninstall, so the install folder can go away. wixl also ignores `Permanent="no"`, so the `PATH` row uses the name `-PATH`, which makes the uninstall remove the entry.
- The release build needs GoReleaser Pro. A tag release needs the `GORELEASER_KEY` secret. A snapshot build runs without a key.
- The release job needs wixl 0.105 or later for the `PATH` entry, so it runs on Ubuntu 26.04.
- The MSI has no Authenticode signature. SmartScreen shows "unknown publisher", the same as for the exes in the zip.
- The MSI is amd64 only, the same as `qui-tray.exe`.

## Considered options

- **A per-machine install in Program Files.** Rejected: a normal user cannot write there, so Self-update turns off, and every update needs a new MSI and a UAC prompt.
- **WiX v5 with a wizard, built on a Windows runner.** Rejected: WiX v4 and later run only on Windows, so the release moves off Linux. A folder picker also lets users pick Program Files, which turns Self-update off.
- **Updates through a new MSI.** Rejected: it adds a second update path only to keep the version label in Installed apps correct.
