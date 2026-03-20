---
sidebar_position: 7
title: External Programs
description: Launch scripts or applications from the torrent context menu.
---

# External Programs

Launch scripts or desktop applications directly from the torrent context menu. Each program definition stores the executable path, optional arguments, and path-mapping rules so qui can pass torrent metadata to your tools.

## Security: Allow List

To keep this power feature safe, define an allow list in `config.toml` so only trusted paths can be executed:

```toml
externalProgramAllowList = [
  "/usr/local/bin/sonarr",
  "/home/user/bin"  # Directories allow any executable inside them
]
```

Leave the list empty to keep the previous behaviour (any path accepted). The allow list lives exclusively in `config.toml`, which the web UI cannot edit, so you retain control over what binaries are exposed.

## Where Programs Run

External programs always run on the same machine (or container) that is hosting the qui backend, not on the browser client. Make sure any executable paths, mounts, or environment variables are available to that host process. When you deploy qui inside Docker, the program runs inside the container unless you mount the executable in.

## Creating and Editing a Program

1. Open qui and go to **Settings → External Programs**
2. Click **Create External Program**
3. Fill in the form fields, then press **Create**. Toggle **Enable this program** to make it available in torrent menus
4. Use the edit and delete actions in the list to maintain existing programs

### Field Reference

| Field | Description |
|-------|-------------|
| **Name** | Display label shown in the torrent context menu and settings list. Must be unique. |
| **Program Path** | Absolute path to the executable or script. Use the host path seen by the qui backend (e.g. `/usr/local/bin/my-script.sh`, `C:\Scripts\postprocess.bat`, `C:\python312\python.exe`). |
| **Arguments Template** | Optional string of command-line arguments. qui substitutes torrent metadata placeholders before spawning the process. |
| **Path Mappings** | Optional array of `from → to` prefixes that rewrite remote qBittorrent paths into local mount points. Helpful when qui runs locally but qBittorrent stores data elsewhere. |
| **Launch in terminal window** | Opens the program in an interactive terminal window. See [Supported Terminal Emulators](#supported-terminal-emulators) for the list of detected terminals. Disable for GUI apps or background daemons. |
| **Enable this program** | Determines whether the program shows up in the torrent context menu. |

## Torrent Placeholders

Arguments are parsed with shell-style quoting and each placeholder is replaced with the corresponding torrent value before execution.

| Placeholder | Value |
|-------------|-------|
| `{hash}` | Torrent hash (always lowercase) |
| `{name}` | Torrent name |
| `{save_path}` | Torrent save path after path mappings are applied |
| `{content_path}` | Full content path (file or folder) after path mappings are applied |
| `{category}` | Torrent category |
| `{tags}` | Comma-separated list of tags |
| `{state}` | qBittorrent torrent state string |
| `{size}` | Size in bytes |
| `{progress}` | Progress value between 0 and 1 rounded to two decimal places |
| `{comment}` | Torrent comment |

**Example arguments:**

```text
"{hash}" "{name}" --save "{save_path}" --category "{category}" --tags "{tags}"
```

```text
D:\Upload Assistant\upload.py {save_path}\{name}
```

qui splits the template into arguments before substitutions are run, so you do not need to wrap values in extra quotes unless the called application expects them.

## Path Mappings

Use path mappings when the filesystem paths reported by qBittorrent do not match the paths visible to qui. Each mapping replaces the longest matching prefix.

| Remote path (from qBittorrent) | Local path seen by qui | Mapping |
|--------------------------------|------------------------|---------|
| `/data/torrents` | `/mnt/qbt` | `from=/data/torrents`, `to=/mnt/qbt` |
| `Z:\downloads` | `/srv/downloads` | `from=Z:\downloads`, `to=/srv/downloads` |

Given the template above, `{save_path}` becomes `/mnt/qbt/Movies` instead of `/data/torrents/Movies`. Be sure to use the same path separator style (`/` vs `\`) as the remote qBittorrent instance. If no mapping matches, the original path is used.

## Launch Modes

- **Enable terminal window** for scripts that need interaction or visible output.
- **Disable terminal window** for GUI applications or background tasks.

Programs run asynchronously - qui does not wait for completion.

### Supported Terminal Emulators

When "Launch in terminal window" is enabled, qui automatically detects and uses an available terminal emulator. Detection priority:

1. **TERM_PROGRAM environment variable** - If qui is running inside a terminal, that terminal is preferred
2. **Cross-platform terminals** (checked on all platforms):
   - WezTerm
   - Hyper
   - Kitty
   - Alacritty
3. **Linux terminals**:
   - GNOME Terminal
   - Konsole
   - Xfce4 Terminal
   - MATE Terminal
   - xterm
   - Terminator
4. **macOS native terminals**:
   - iTerm2
   - Terminal.app
5. **Fallback**: If no terminal is found, the command runs in the background via `sh -c`

On Windows, `cmd.exe` is always used.

:::tip
Terminal windows stay open after the command finishes, allowing you to inspect output. Close the window manually when done.
:::

## Executing Programs

1. Select one or more torrents
2. Right-click to open the context menu
3. Hover **External Programs**, then click the program name
4. qui queues one execution per selected torrent. Results are reported via toast notifications (success, partial success, or failure)

Execution requests include the torrents from the currently selected instance only. Disabled programs are hidden from the submenu. Command failures emitted by the host OS are logged at `info`/`debug` level through zerolog; enable debug logging to see the full command line and any non-zero exit codes.

## REST API

Automation workflows can manage external programs through the backend API (all endpoints require authentication):

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/external-programs` | List programs |
| `POST` | `/api/external-programs` | Create a program |
| `PUT` | `/api/external-programs/{id}` | Update a program |
| `DELETE` | `/api/external-programs/{id}` | Remove a program |
| `POST` | `/api/external-programs/execute` | Execute a program |

**Example request:**

```http
POST /api/external-programs/execute
Content-Type: application/json

{
  "program_id": 2,
  "instance_id": 1,
  "hashes": ["c0ffee...", "deadbeef..."]
}
```

The response contains a `results` array with per-hash `success` flags and optional error messages. Treat the endpoint as fire-and-forget; it returns once the processes have been spawned.

## Automation Integration

External programs can be triggered automatically via automation rules, allowing you to run scripts when torrents match specific conditions.

### Setting Up Automation Triggers

1. Create and enable an external program in **Settings → External Programs**
2. Go to **Automations** and create or edit a rule
3. Add an **External Program** action and select your program
4. Optionally add a condition override specific to this action

### Behavior

| Aspect | Description |
|--------|-------------|
| **Execution** | Programs run asynchronously (fire-and-forget) to avoid blocking automation processing |
| **Configuration** | Uses the same program settings (path, arguments, path mappings) as manual execution |
| **Availability** | Only enabled programs appear in the automation dropdown |
| **Combinable** | Can be combined with other actions (speed limits, share limits, pause, tag, category) |

### Activity Logging

Automation-triggered executions are logged in the activity feed with:
- Rule name and rule ID that triggered the execution
- Torrent name and hash
- Success or failure status
- Error details if the program failed to start

:::note
Success is logged after the program actually starts, not when queued. If the program fails to start (e.g., executable not found, permission denied), the error is captured and logged.
:::

### Example Use Cases

**Post-processing completed downloads:**
- Condition: `State is completed`
- Action: External Program that runs a media processing script

**Webhook notifications:**
- Condition: `Is Unregistered is true`
- Action: External Program that sends a notification via curl/webhook

**Media library scans:**
- Condition: Category changed to "movies" (use category action + external program)
- Action: External Program that triggers Plex/Jellyfin scan

## Troubleshooting

- **Docker**: The executable must be inside the container or bind-mounted.
- **Paths are wrong**: Add or adjust path mappings so `{save_path}` and `{content_path}` resolve to local mount points.
- **Multiple torrents**: The program runs once per torrent. Ensure your script handles concurrent executions or uses a locking mechanism.
- **Automation not triggering**: Ensure the program is enabled in Settings → External Programs. Disabled programs do not appear in automation dropdowns.
