# External Programs

The external programs feature lets you launch scripts or desktop applications directly from the torrent context menu in qui. Each program definition stores the executable path, optional arguments, and path-mapping rules so that qui can pass torrent metadata in a predictable way. This is ideal for glue scripts, post-processing pipelines, media managers, or anything else that needs rich torrent context without requiring another service to talk to qBittorrent.

## Where the programs run

External programs always run on the same machine (or container) that is hosting the qui backend, not on the browser client. Make sure any executable paths, mounts, or environment variables are available to that host process. When you deploy qui inside Docker, the program runs inside the container unless you mount the executable in.

## Creating and editing a program

1. Open qui and go to **Settings → External Programs**.
2. Click **Create External Program**.
3. Fill in the form fields, then press **Create**. Toggle **Enable this program** to make it available in torrent menus.
4. Use the edit and delete actions in the list to maintain existing programs.

### Field reference

| Field | Description |
| --- | --- |
| **Name** | Display label shown in the torrent context menu and settings list. Must be unique. |
| **Program Path** | Absolute path to the executable or script. Use the host path seen by the qui backend (e.g. `/usr/local/bin/my-script.sh`, `C:\Scripts\postprocess.bat`, `C:\python312\python.exe`). |
| **Arguments Template** | Optional string of command-line arguments. qui substitutes torrent metadata placeholders before spawning the process. |
| **Path Mappings** | Optional array of `from → to` prefixes that rewrite remote qBittorrent paths into local mount points. Helpful when qui runs locally but qBittorrent stores data elsewhere. |
| **Launch in terminal window** | Opens the program in an interactive terminal (`cmd.exe` on Windows, first available emulator on Linux/macOS). Disable for GUI apps or background daemons. |
| **Enable this program** | Determines whether the program shows up in the torrent context menu. |

> [!TIP]
> The form trims leading/trailing whitespace and drops blank path mappings automatically.

## Torrent placeholders

Arguments are parsed with shell-style quoting and each placeholder is replaced with the corresponding torrent value before execution.

| Placeholder | Value |
| --- | --- |
| `{hash}` | Torrent hash (always lowercase). |
| `{name}` | Torrent name. |
| `{save_path}` | Torrent save path after path mappings are applied. |
| `{content_path}` | Full content path (file or folder) after path mappings are applied. |
| `{category}` | Torrent category. |
| `{tags}` | Comma-separated list of tags. |
| `{state}` | qBittorrent torrent state string. |
| `{size}` | Size in bytes. |
| `{progress}` | Progress value between 0 and 1 rounded to two decimal places. |

Example arguments:

```text
"{hash}" "{name}" --save "{save_path}" --category "{category}" --tags "{tags}"
```

```text
D:\Upload Assistant\upload.py {save_path}\{name}
```

qui splits the template into arguments before substitutions are run, so you do not need to wrap values in extra quotes unless the called application expects them.

## Path mappings

Use path mappings when the filesystem paths reported by qBittorrent do not match the paths visible to qui. Each mapping replaces the longest matching prefix.

Example:

| Remote path (from qBittorrent) | Local path seen by qui | Mapping |
| --- | --- | --- |
| `/data/torrents` | `/mnt/qbt` | `from=/data/torrents`, `to=/mnt/qbt` |
| `Z:\downloads` | `/srv/downloads` | `from=Z:\downloads`, `to=/srv/downloads` |

Given the template above, `{save_path}` becomes `/mnt/qbt/Movies` instead of `/data/torrents/Movies`. Be sure to use the same path separator style (`/` vs `\`) as the remote qBittorrent instance. If no mapping matches, the original path is used.

## Launch modes

- **Launch in terminal window** is best for scripts that need interaction or to keep stdout/stderr visible. On Windows, qui runs `cmd.exe /c start "" cmd /k` so the window stays open. On Linux/macOS, qui tries common terminal emulators in this order: `gnome-terminal`, `konsole`, `xfce4-terminal`, `mate-terminal`, `xterm`, `kitty`, `alacritty`, `terminator`. If none are found the command falls back to `sh -c` in the background.
- **Disable the terminal option** for GUI applications or one-shot background tasks. On Windows, qui still uses `cmd.exe /c start` so GUI apps detach cleanly; on Unix-like systems it executes the binary directly.

Regardless of mode, the process is spawned asynchronously in a goroutine—qui never blocks waiting for completion, and the torrent context menu stays responsive.

## Executing programs

1. Select one or more torrents.
2. Right-click to open the context menu.
3. Hover **External Programs**, then click the program name.
4. qui queues one execution per selected torrent. Results are reported via toast notifications (success, partial success, or failure).

Execution requests include the torrents from the currently selected instance only. Disabled programs are hidden from the submenu. Command failures emitted by the host OS are logged at `info`/`debug` level through zerolog; enable debug logging to see the full command line and any non-zero exit codes.

## REST API

Automation workflows can manage external programs through the backend API (all endpoints require authentication):

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/external-programs` | List programs. |
| `POST` | `/api/external-programs` | Create a program. Body matches `ExternalProgramCreate`. |
| `PUT` | `/api/external-programs/{id}` | Update a program. |
| `DELETE` | `/api/external-programs/{id}` | Remove a program. |
| `POST` | `/api/external-programs/execute` | Execute a program for the provided `program_id`, `instance_id`, and torrent `hashes`. |

Example request:

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

## Troubleshooting checklist

- **Nothing happens**: verify the program path is correct for the qui host and that the binary has execute permissions. Inside Docker, the executable must be inside the container or bind-mounted.
- **Paths are wrong**: add or adjust path mappings so `{save_path}` and `{content_path}` resolve to local mount points.
- **Terminal window closes immediately**: add a shell wrapper that waits (e.g., `...; read -p "Press enter"`) or enable logging to a file.
- **Multiple torrents**: the program runs once per torrent hash. Ensure your script can handle concurrent executions or wrap it in a locking mechanism.
- **Windows quoting issues**: avoid additional wrapping quotes inside the arguments template; qui already handles command splitting and quoting for `cmd.exe`.

With the external programs feature you can integrate qui with virtually any local tool without sharing qBittorrent credentials or writing additional API glue.
