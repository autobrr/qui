---
sidebar_position: 7
---

# External Programs

The torrent context menu can launch local scripts or applications through configurable "external programs". To keep that power feature safe, define an allow list in `config.toml` so only trusted paths can be executed:

```toml
externalProgramAllowList = [
  "/usr/local/bin/sonarr",
  "/home/user/bin"  # Directories allow any executable inside them
]
```

Leave the list empty to keep the previous behaviour (any path accepted). The allow list lives exclusively in `config.toml`, which the web UI cannot edit, so you retain control over what binaries are exposed.
