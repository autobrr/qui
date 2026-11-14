# Memory rig

The memory rig measures how much memory qui uses on a large library when many requests read the torrents at the same time. It runs qui against a stub qBittorrent with synthetic torrents. CI does not run it. ADR 0051 and issue #4029 give the reason for the rig.

## What it does

- `stub.py` is a fake qBittorrent WebUI. It holds the number of torrents that you give it, on 40 tracker hosts under `.test`. Each content has 1 to 4 copies. The first sync gets a full update. Each later sync gets a change to 0.5% of the torrents.
- `run.sh` starts the stub and qui, adds the stub as an instance, waits for the first sync, and runs one scenario. After each step, it writes the resident memory, the peak resident memory, and the Go heap of qui to `samples.txt`. It resets the peak between steps. A run stops with exit status 1 if qui or the stub stops, the library does not load, a list request fails, a stream ends early or gets a sync error, or the rig counts no sync.
- `launch.sh` starts `run.sh` in a network namespace that has only the loopback interface, with a memory limit of 5 GiB and no swap. If a run uses more memory, the kernel stops it, and the machine stays usable. The ports of the rig do not collide with processes on the host.
- `all.sh` runs the five scenarios of a comparison, one after the other. For one build, it takes about 15 minutes. It exits with status 1 if a scenario did not run to the end.

| Scenario | What it sends |
| --- | --- |
| `scen-webhook.sh` | Cross-seed webhook checks, 1, 8, and 32 at the same time, 6 rounds each. The `KS` variable changes the counts. |
| `scen-concurrency.sh` | Torrent list requests, 1, 4, 16, and 32 at the same time, each with its own sort. |
| `scen-sse.sh` | Live-update streams, 1, 8, and 24 at the same time, each a different view. |
| `scen-synccpu.sh` | One live-update stream for 60 seconds. It reports the CPU time and the allocated bytes for each sync. |
| `scen-baseline.sh` | Idle, then list pages, then one stream. |

## Requirements

The rig runs on Linux. It needs `systemd-run --user`, `unshare`, `python3`, and `curl`.

## Run it

Build qui, then run one scenario:

```bash
make build
scripts/memrig/launch.sh "$PWD/qui" 20000 scripts/memrig/scen-webhook.sh ~/.cache/qui-memrig/out/hook
```

To run the full comparison for one build, run `all.sh`. It writes the output to `$MEMRIG_OUT`, which is `~/.cache/qui-memrig/out` by default:

```bash
scripts/memrig/all.sh ./qui new
```

To compare two builds, run the new build, then the old build, then the new build again. The two runs of the new build must agree before a difference counts. Two runs of one build differ by about 15%.
