---
sidebar_position: 8
title: External Programs
description: Launch scripts or applications from the torrent context menu.
---

# External Programs

Launch scripts or desktop applications from the torrent context menu. Each program definition stores the executable path, optional arguments, and path-mapping rules. qui uses them to pass torrent metadata to your tools.

## Security: allow list

Define an allow list in `config.toml` so qui executes only trusted paths:

```toml
externalProgramAllowList = [
  "/usr/local/bin/sonarr",
  "/home/user/bin"  # Directories allow any executable inside them
]
```

If you leave the list empty, qui accepts any path. The allow list lives only in `config.toml`. The web UI cannot edit this file, so you retain full control over the binaries that qui runs.

## Where programs run

External programs run on the machine or container that hosts the qui backend, not on the browser client. Make sure that the host process has access to all required executable paths, mounts, and environment variables. If you deploy qui inside Docker, qui executes programs inside the container. If an executable resides on the host, mount that executable into the container.

## Creating and editing a program

1. Open qui and go to **Settings → External Programs**
2. Click **Create External Program**
3. Fill in the form fields, then click **Create**. Toggle **Enable this program** to display the program in torrent context menus
4. Use the edit and delete actions in the list to manage existing programs

### Field reference

| Field | Description |
|-------|-------------|
| **Name** | Display label in the torrent context menu and settings list. Must be unique. |
| **Program Path** | Absolute path to the executable or script. Use the host path that the qui backend sees (for example `/usr/local/bin/my-script.sh`, `C:\Scripts\postprocess.bat`, `C:\python312\python.exe`). |
| **Arguments Template** | Optional string of command-line arguments. qui substitutes torrent metadata placeholders before it spawns the process. |
| **Path Mappings** | Optional array of `from → to` prefixes that rewrite remote qBittorrent paths into local mount points. If qui runs locally and qBittorrent stores data elsewhere, configure path mappings here. |
| **Launch in terminal window** | Opens the program in an interactive terminal window. See [Supported terminal emulators](#supported-terminal-emulators) for the list of detected terminals. If you run GUI apps or background daemons, disable this option. |
| **Enable this program** | Controls whether the program appears in the torrent context menu. |

## Torrent placeholders

qui parses arguments with shell-style quoting and replaces each placeholder with the torrent value before execution.

| Placeholder | Value |
|-------------|-------|
| `{hash}` | Torrent hash (always lowercase) |
| `{name}` | Torrent name |
| `{save_path}` | Torrent save path after qui applies path mappings |
| `{content_path}` | Full content path (file or folder) after qui applies path mappings |
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
"D:\Upload Assistant\upload.py" "{save_path}\{name}"
```

qui splits the template into arguments before it runs substitutions. Quotes keep a path with spaces as one argument. qui removes the quotes, so the program receives the value without them. If the program requires literal quotes, put a different quote type inside the outer pair, for example `'"{name}"'`.

## Path mappings

If the filesystem paths from qBittorrent do not match the paths visible to qui, use path mappings. Each mapping replaces the longest matching prefix.

| Remote path (from qBittorrent) | Local path seen by qui | Mapping |
|--------------------------------|------------------------|---------|
| `/data/torrents` | `/mnt/qbt` | `from=/data/torrents`, `to=/mnt/qbt` |
| `Z:\downloads` | `/srv/downloads` | `from=Z:\downloads`, `to=/srv/downloads` |

With the first mapping, `{save_path}` becomes `/mnt/qbt/Movies` instead of `/data/torrents/Movies`. Use the same path separator style (`/` or `\`) as the remote qBittorrent instance. If no mapping matches, qui uses the original path.

## Launch modes

- **Enable terminal window**: Use this option for scripts that require interaction or visible output.
- **Disable terminal window**: Use this option for GUI applications or background tasks.

Programs run asynchronously. qui does not wait for processes to complete.

### Supported terminal emulators

If you enable "Launch in terminal window", qui detects and uses an available terminal emulator. Detection priority:

1. **TERM_PROGRAM environment variable**: qui accepts `WezTerm`, `Hyper`, `kitty`, `alacritty`, `iTerm.app`, and `Apple_Terminal`. If qui finds the matching terminal, it uses it
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
5. **Fallback**: If qui finds no terminal, qui runs the command in the background with `sh -c`

On Windows, qui always uses `cmd.exe`.

:::tip
Terminal windows stay open after the command finishes so you can inspect output. Close the window when you finish.
:::

## Executing programs

1. Select one or more torrents
2. Right-click to open the context menu
3. Hover **External Programs**, then click the program name
4. qui queues one execution per selected torrent and reports results through toast notifications (success, partial success, or failure)

Execution requests include only torrents from the currently selected instance. The submenu hides disabled programs. qui logs start failures at `error` level and non-zero exit codes at `warn` level with the full command line. If you enable debug logging, qui also logs each command before execution.

## REST API

Manage external programs through the backend API. All endpoints require authentication:

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

The response contains a `results` array with per-hash `success` flags and optional error messages. Treat the endpoint as fire-and-forget. It returns after qui spawns the processes.

## Automation integration

When torrents match configured conditions, automation rules trigger external programs.

### Setting up automation triggers

1. Create and enable an external program in **Settings → External Programs**
2. Go to **Automations** and create or edit a rule
3. Add an **External Program** action and select your program
4. If you need a custom condition for this action, add a condition override

### Behavior

| Aspect | Description |
|--------|-------------|
| **Execution** | Programs run asynchronously (fire-and-forget) and do not block automation processing |
| **Configuration** | Automation uses the same program configuration (path, arguments, path mappings) as manual execution |
| **Availability** | The dropdown lists enabled programs. A disabled program stays in the list, marked "(disabled)", only when the rule already uses it |
| **Combinable** | You can combine this action with other actions (speed limits, share limits, pause, tag, category) |

### Activity logging

qui logs automation executions in the activity feed with:
- Rule name and rule ID that triggered the execution
- Torrent name and hash
- Success or failure status
- Error details if the program failed to start

:::note
qui logs success after the program starts, not when qui queues the task. If the program fails to start (for example, executable not found or permission denied), qui captures and logs the error.
:::

### Example use cases

**Post-processing completed downloads:**
- Condition: `State is completed`
- Action: External Program that runs a media processing script

**Webhook notifications:**
- Condition: `Is Unregistered is true`
- Action: External Program that sends a notification via curl/webhook

**Media library scans:**
- Condition: `Category is movies`
- Action: External Program that triggers a Plex or Jellyfin scan

## Troubleshooting

- **Docker**: If qui runs in Docker, place the executable inside the container or bind-mount it from the host.
- **Paths are wrong**: Add or adjust path mappings so `{save_path}` and `{content_path}` resolve to local mount points.
- **Multiple torrents**: The program runs once per torrent. Make sure that your script handles concurrent executions or uses a lock.
- **Automation not triggering**: Make sure that you enabled the program in **Settings → External Programs**. Disabled programs do not appear in the dropdown for new rules.
